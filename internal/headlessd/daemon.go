package headlessd

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/logging"
	"pkt.systems/lingon/internal/mvu"
	"pkt.systems/lingon/internal/protocol"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/relayclient"
	"pkt.systems/lingon/internal/session"
	"pkt.systems/lingon/internal/terminal"
	"pkt.systems/lingon/internal/trace"
	"pkt.systems/pslog"
)

// Options configures a headless daemon session.
type Options struct {
	ConfigDir               string
	Endpoint                string
	Token                   string
	AuthFile                string
	SessionID               string
	Cols                    int
	Rows                    int
	Shell                   string
	Term                    string
	Respawn                 bool
	Offline                 bool
	WallInactiveAfterLevels []time.Duration

	Publish         bool
	PublishControl  bool
	HostnameOnly    bool
	ScrollbackLines int
	TLSDir          string
	Insecure        bool

	Clock  clock.Clock
	Logger pslog.Logger
	Trace  *trace.Writer
}

// Daemon runs one local headless PTY session and serves local attach clients over UDS websocket.
type Daemon struct {
	opts       Options
	logger     pslog.Logger
	clock      clock.Clock
	store      *headless.Store
	runner     *session.Runner
	sessionID  string
	socketPath string
	startedAt  time.Time

	stdinMu sync.Mutex
	stdinW  *os.File

	clientsMu  sync.RWMutex
	clients    map[string]*wsClient
	holderID   string
	snapshot   *protocolpb.Snapshot
	scrollback *mvu.ProtoScrollbackBuffer
	offline    bool
	cols       int
	rows       int
	status     *protocolpb.Wall
	statusExp  time.Time

	wallMu           sync.Mutex
	wallLevels       []time.Duration
	wallEnabled      bool
	wallLevel        int
	wallAfter        time.Duration
	wallLastActivity time.Time
	wallArmed        bool

	seq uint64
}

type wsClient struct {
	id      string
	ws      *websocket.Conn
	writeMu sync.Mutex
}

type internalWallEvent struct {
	SourceSessionID string `json:"source_session_id"`
	Sender          string `json:"sender"`
	Message         string `json:"message"`
	TimeoutSeconds  uint32 `json:"timeout_seconds"`
}

// New constructs a daemon.
func New(opts Options) *Daemon {
	logger := opts.Logger
	if logger == nil {
		logger = logging.Default()
	}
	clk := opts.Clock
	if clk == nil {
		clk = clock.New()
	}
	cfgDir := strings.TrimSpace(opts.ConfigDir)
	if cfgDir == "" {
		cfgDir = config.DefaultConfigDir()
	}
	return &Daemon{
		opts:             opts,
		logger:           logger,
		clock:            clk,
		store:            headless.NewStore(cfgDir),
		clients:          map[string]*wsClient{},
		offline:          opts.Offline,
		cols:             opts.Cols,
		rows:             opts.Rows,
		wallLevels:       normalizeWallInactiveAfterLevels(opts.WallInactiveAfterLevels),
		wallLastActivity: clk.Now().UTC(),
	}
}

func normalizeWallInactiveAfterLevels(levels []time.Duration) []time.Duration {
	if len(levels) == 0 {
		return config.DefaultWallInactiveAfterLevels()
	}
	out := make([]time.Duration, 0, len(levels))
	seen := make(map[time.Duration]struct{}, len(levels))
	for _, level := range levels {
		if level <= 0 {
			continue
		}
		if _, ok := seen[level]; ok {
			continue
		}
		seen[level] = struct{}{}
		out = append(out, level)
	}
	if len(out) == 0 {
		return config.DefaultWallInactiveAfterLevels()
	}
	return out
}

func (d *Daemon) scrollbackLimit() int {
	if d.opts.ScrollbackLines > 0 {
		return d.opts.ScrollbackLines
	}
	return config.DefaultScrollbackLines
}

// Run starts the daemon and blocks until shutdown.
func (d *Daemon) Run(ctx context.Context) error {
	d.startedAt = d.clock.Now().UTC()
	sessionID, err := d.resolveSessionID()
	if err != nil {
		return err
	}
	d.sessionID = sessionID
	if d.cols <= 0 {
		d.cols = config.DefaultTerminalCols
	}
	if d.rows <= 0 {
		d.rows = config.DefaultTerminalRows
	}
	d.clientsMu.Lock()
	d.snapshot = blankSnapshot(d.cols, d.rows)
	d.clientsMu.Unlock()

	socketPath, err := headless.SocketPath(d.opts.ConfigDir, d.sessionID)
	if err != nil {
		return err
	}
	d.socketPath = socketPath
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return err
	}
	if err := d.ensureSessionAvailable(); err != nil {
		return err
	}
	_ = os.Remove(socketPath)

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = ln.Close()
		_ = os.Remove(socketPath)
	}()
	_ = os.Chmod(socketPath, 0o600)

	if err := d.writeState("running", ""); err != nil {
		return err
	}
	defer func() {
		_ = d.removeStateRecord()
	}()

	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return err
	}
	defer func() {
		_ = stdinR.Close()
		_ = stdinW.Close()
	}()
	d.stdinW = stdinW

	stdoutFile, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer func() {
		_ = stdoutFile.Close()
	}()

	runnerCtx, cancelRunner := context.WithCancel(ctx)
	defer cancelRunner()
	runner := session.New(session.Options{
		Endpoint:        d.opts.Endpoint,
		Token:           d.opts.Token,
		AuthFile:        d.opts.AuthFile,
		SessionID:       d.sessionID,
		Cols:            d.opts.Cols,
		Rows:            d.opts.Rows,
		Shell:           d.opts.Shell,
		Term:            d.opts.Term,
		Respawn:         d.opts.Respawn,
		Offline:         d.opts.Offline,
		Publish:         d.opts.Publish,
		PublishControl:  d.opts.PublishControl,
		HostnameOnly:    d.opts.HostnameOnly,
		ScrollbackLines: d.opts.ScrollbackLines,
		TLSDir:          d.opts.TLSDir,
		Insecure:        d.opts.Insecure,
		Stdin:           stdinR,
		Stdout:          stdoutFile,
		DisableRaw:      true,
		Logger:          d.logger,
		Trace:           d.opts.Trace,
		Clock:           d.clock,
		OnPublishFrame:  d.handlePublishedFrame,
		OnPublishStatus: d.handlePublishStatus,
		OnPublishWall:   d.handlePublishWall,
		OnStatus:        d.handleSessionStatus,
		ToggleWallInactivityFallback: func(ctx context.Context, sessionID string) (session.WallInactivityToggleResult, error) {
			return d.toggleWallInactivityFallback(sessionID), nil
		},
		OnSnapshot: func(snap terminal.Snapshot) {
			frame := &protocolpb.Frame{
				SessionId: d.sessionID,
				Payload: &protocolpb.Frame_Snapshot{
					Snapshot: protocol.SnapshotToProto(snap),
				},
			}
			d.handlePublishedFrame(frame)
		},
	})
	d.runner = runner

	runnerErrCh := make(chan error, 1)
	go func() {
		runnerErrCh <- runner.Run(runnerCtx)
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/client", d.handleWSClient)
	mux.HandleFunc("/internal/headless/wall", d.handleInternalWall)
	httpServer := &http.Server{Handler: mux}
	serveErrCh := make(chan error, 1)
	go func() {
		serveErrCh <- httpServer.Serve(ln)
	}()

	heartbeatCtx, stopHeartbeat := context.WithCancel(context.Background())
	defer stopHeartbeat()
	go d.heartbeat(heartbeatCtx)
	go d.monitorLocalWallInactivity(ctx)

	runnerDone := false
	var runnerErr error
	select {
	case <-ctx.Done():
		cancelRunner()
	case err := <-runnerErrCh:
		runnerDone = true
		runnerErr = err
		if runnerErr != nil && !errors.Is(runnerErr, context.Canceled) {
			_ = d.writeState("stopped", err.Error())
			return err
		}
	case err := <-serveErrCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			_ = d.writeState("stopped", err.Error())
			cancelRunner()
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	cancelRunner()
	if !runnerDone {
		select {
		case err := <-runnerErrCh:
			runnerDone = true
			runnerErr = err
		case <-d.clock.After(2 * time.Second):
		}
	}
	if runnerDone && runnerErr != nil && !errors.Is(runnerErr, context.Canceled) {
		_ = d.writeState("stopped", runnerErr.Error())
		return runnerErr
	}
	_ = d.writeState("stopped", "")
	return nil
}

func (d *Daemon) resolveSessionID() (string, error) {
	sid := strings.TrimSpace(d.opts.SessionID)
	if sid == "" {
		generated, _ := session.DefaultSessionIdentity()
		sid = generated
	}
	return headless.NormalizeSessionID(sid)
}

func (d *Daemon) handlePublishedFrame(frame *protocolpb.Frame) {
	if frame == nil {
		return
	}
	cloneAny := proto.Clone(frame)
	cloned, ok := cloneAny.(*protocolpb.Frame)
	if !ok {
		return
	}
	if cloned.SessionId == "" {
		cloned.SessionId = d.sessionID
	}
	if cloned.GetSnapshot() != nil || cloned.GetScrollback() != nil {
		d.noteLocalActivity()
	}
	d.clientsMu.Lock()
	if snap := cloned.GetSnapshot(); snap != nil {
		d.snapshot = snap
		d.cols = int(snap.GetCols())
		d.rows = int(snap.GetRows())
	}
	if scrollback := cloned.GetScrollback(); scrollback != nil {
		if d.snapshot == nil && d.cols <= 0 {
			d.cols = int(scrollback.GetCols())
		}
		if d.scrollback == nil {
			d.scrollback = mvu.NewProtoScrollbackBuffer(d.scrollbackLimit())
		}
		d.scrollback.SetLimit(d.scrollbackLimit())
		d.scrollback.Apply(scrollback)
	}
	d.clientsMu.Unlock()
	if cloned.Seq == 0 {
		cloned.Seq = atomic.AddUint64(&d.seq, 1)
	}
	d.broadcast(cloned)
}

func (d *Daemon) handleWSClient(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer func() {
		_ = ws.Close(websocket.StatusNormalClosure, "closing")
	}()
	ws.SetReadLimit(config.DefaultWSReadLimit)

	frame, err := readFrame(ctx, ws)
	if err != nil {
		return
	}
	hello := frame.GetHello()
	if hello == nil {
		_ = writeFrame(ctx, ws, frameError("missing hello"))
		return
	}
	if frame.SessionId != "" && frame.SessionId != d.sessionID {
		_ = writeFrame(ctx, ws, frameError("session mismatch"))
		return
	}
	clientID := strings.TrimSpace(hello.GetClientId())
	if clientID == "" {
		clientID = randomClientID()
	}
	client := &wsClient{id: clientID, ws: ws}
	d.addClient(client)
	defer d.removeClient(client.id)

	holderChanged := false
	var scrollRows []*protocolpb.ScrollbackRow
	d.clientsMu.Lock()
	if hello.GetWantsControl() || d.holderID == "" {
		if d.holderID != clientID {
			d.holderID = clientID
			holderChanged = true
		}
	}
	holder := d.holderID
	cols := d.cols
	rows := d.rows
	snap := d.snapshot
	if d.scrollback != nil {
		scrollRows = d.scrollback.Rows()
	}
	d.clientsMu.Unlock()

	if cols <= 0 {
		cols = config.DefaultTerminalCols
	}
	if rows <= 0 {
		rows = config.DefaultTerminalRows
	}
	welcome := &protocolpb.Frame{SessionId: d.sessionID, Payload: &protocolpb.Frame_Welcome{Welcome: &protocolpb.Welcome{
		GrantedControl: true,
		ServerCols:     uint32(cols),
		ServerRows:     uint32(rows),
		HolderClientId: holder,
	}}}
	if err := writeFrame(ctx, ws, welcome); err != nil {
		return
	}
	if holderChanged {
		d.broadcast(&protocolpb.Frame{SessionId: d.sessionID, Payload: &protocolpb.Frame_Control{Control: &protocolpb.Control{HolderClientId: holder}}})
	}
	if snap != nil || len(scrollRows) > 0 {
		d.handleClientResync(ctx, client, snap, cols, scrollRows)
	}
	if status := d.currentStatusWall(); status != nil {
		_ = d.writeClientFrame(ctx, client, &protocolpb.Frame{
			SessionId: d.sessionID,
			Seq:       atomic.AddUint64(&d.seq, 1),
			Payload:   &protocolpb.Frame_Wall{Wall: status},
		})
	}

	for {
		frame, err := readFrame(ctx, ws)
		if err != nil {
			return
		}
		if frame.GetHello() != nil {
			d.clientsMu.RLock()
			snap := d.snapshot
			cols := d.cols
			var scrollRows []*protocolpb.ScrollbackRow
			if d.scrollback != nil {
				scrollRows = d.scrollback.Rows()
			}
			d.clientsMu.RUnlock()
			if snap != nil || len(scrollRows) > 0 {
				d.handleClientResync(ctx, client, snap, cols, scrollRows)
			}
			continue
		}
		if in := frame.GetIn(); in != nil {
			data := in.GetData()
			if len(data) > 0 {
				d.setHolder(client.id)
				if d.runner != nil {
					d.runner.HandleSessionInput(d.sessionID, data)
				} else {
					_ = d.writeInput(data)
				}
				d.noteLocalActivity()
			}
			continue
		}
		if command := frame.GetCommand(); command != nil {
			d.setHolder(client.id)
			d.handleCommand(ctx, command.GetKind())
			continue
		}
		if frame.GetResize() != nil {
			d.setHolder(client.id)
			resize := frame.GetResize()
			d.applyResize(int(resize.GetCols()), int(resize.GetRows()))
			continue
		}
	}
}

func (d *Daemon) applyResize(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	d.clientsMu.Lock()
	d.cols = cols
	d.rows = rows
	d.clientsMu.Unlock()
	if d.runner != nil {
		d.runner.ResizeActive(cols, rows)
	}
}

func (d *Daemon) handleCommand(ctx context.Context, kind protocolpb.CommandKind) {
	if d.runner == nil {
		return
	}
	d.runner.HandleSessionCommand(ctx, d.sessionID, kind)
	switch kind {
	case protocolpb.CommandKind_COMMAND_KIND_TOGGLE_OFFLINE:
		offline, ok := d.runner.SessionOffline(d.sessionID)
		if !ok {
			return
		}
		d.clientsMu.Lock()
		d.offline = offline
		d.clientsMu.Unlock()
		d.disableLocalWallInactivity()
		d.disableRelayWallInactivityAsync()
		_ = d.writeState("running", "")
		d.logger.Info("headless.offline.toggled", "session", d.sessionID, "offline", offline)
	}
}

func (d *Daemon) disableLocalWallInactivity() {
	d.wallMu.Lock()
	d.wallEnabled = false
	d.wallLevel = 0
	d.wallAfter = 0
	d.wallArmed = false
	d.wallLastActivity = d.clock.Now().UTC()
	d.wallMu.Unlock()
}

func (d *Daemon) disableRelayWallInactivityAsync() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := d.disableRelayWallInactivity(ctx); err != nil && d.logger != nil {
			d.logger.Debug("headless.wall.inactivity.relay.disable.failed", "session", d.sessionID, "err", err)
		}
	}()
}

func (d *Daemon) disableRelayWallInactivity(ctx context.Context) error {
	endpoint := strings.TrimSpace(d.opts.Endpoint)
	if endpoint == "" || strings.TrimSpace(d.sessionID) == "" {
		return nil
	}
	token, err := d.resolveRelayAccessToken(ctx)
	if err != nil || strings.TrimSpace(token) == "" {
		return err
	}
	_, err = relayclient.SetWallInactivity(
		ctx,
		endpoint,
		token,
		d.sessionID,
		false,
		d.opts.TLSDir,
		d.opts.Insecure,
	)
	return err
}

func (d *Daemon) forwardWallToRelayAsync(wall *protocolpb.Wall) {
	if wall == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := d.forwardWallToRelay(ctx, wall); err != nil && d.logger != nil {
			d.logger.Debug("headless.wall.forward.relay.failed", "session", d.sessionID, "err", err)
		}
	}()
}

func (d *Daemon) forwardWallToRelay(ctx context.Context, wall *protocolpb.Wall) error {
	if wall == nil {
		return nil
	}
	message := strings.TrimSpace(wall.GetMessage())
	endpoint := strings.TrimSpace(d.opts.Endpoint)
	if message == "" || endpoint == "" {
		return nil
	}
	token, err := d.resolveRelayAccessToken(ctx)
	if err != nil || strings.TrimSpace(token) == "" {
		return err
	}
	_, err = relayclient.SendWall(
		ctx,
		endpoint,
		token,
		message,
		d.opts.TLSDir,
		d.opts.Insecure,
	)
	return err
}

func (d *Daemon) resolveRelayAccessToken(ctx context.Context) (string, error) {
	token := strings.TrimSpace(d.opts.Token)
	if token != "" {
		return token, nil
	}
	authPath := strings.TrimSpace(d.opts.AuthFile)
	endpoint := strings.TrimSpace(d.opts.Endpoint)
	if authPath == "" || endpoint == "" {
		return "", fmt.Errorf("relay auth unavailable")
	}
	refresher := relayclient.TokenRefresher(endpoint, authPath, d.opts.TLSDir, d.opts.Insecure, nil)
	token, err := refresher(ctx)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(token), nil
}

func (d *Daemon) handlePublishStatus(status session.PublishStatus) {
	sender := ""
	timeoutSeconds := uint32(0)
	switch status.Kind {
	case session.PublishStatusConnected:
		sender = headless.RoutedStatusSenderConnected
		timeoutSeconds = 3
	case session.PublishStatusConnectionLost:
		sender = headless.RoutedStatusSenderLost
	case session.PublishStatusConnectionBackoff:
		sender = headless.RoutedStatusSenderBackoff
		if status.Remaining > 0 {
			timeoutSeconds = uint32(status.Remaining.Round(time.Second) / time.Second)
			if timeoutSeconds == 0 {
				timeoutSeconds = 1
			}
		}
	default:
		return
	}
	message := strings.TrimSpace(status.Message)
	if message == "" {
		return
	}
	wall := &protocolpb.Wall{
		Sender:         sender,
		Message:        message,
		TimeoutSeconds: timeoutSeconds,
	}

	now := d.clock.Now().UTC()
	d.clientsMu.Lock()
	d.status = &protocolpb.Wall{
		Sender:         wall.Sender,
		Message:        wall.Message,
		TimeoutSeconds: wall.TimeoutSeconds,
	}
	if timeoutSeconds > 0 {
		d.statusExp = now.Add(time.Duration(timeoutSeconds) * time.Second)
	} else {
		d.statusExp = time.Time{}
	}
	d.clientsMu.Unlock()

	d.broadcast(&protocolpb.Frame{
		SessionId: d.sessionID,
		Seq:       atomic.AddUint64(&d.seq, 1),
		Payload:   &protocolpb.Frame_Wall{Wall: wall},
	})
}

func (d *Daemon) handleSessionStatus(status session.StatusUpdate) {
	message := strings.TrimSpace(status.Message)
	if message == "" {
		return
	}
	timeoutSeconds := uint32(0)
	if status.Duration > 0 {
		timeoutSeconds = uint32(status.Duration.Round(time.Second) / time.Second)
		if timeoutSeconds == 0 {
			timeoutSeconds = 1
		}
	}
	sender := headless.RoutedStatusSenderInfo
	if status.Kind == session.StatusKindError {
		sender = headless.RoutedStatusSenderError
	}
	wall := &protocolpb.Wall{
		Sender:         sender,
		Message:        message,
		TimeoutSeconds: timeoutSeconds,
	}
	d.routeWallEvent(wall, true)
}

func (d *Daemon) handlePublishWall(wall *protocolpb.Wall) {
	if wall == nil {
		return
	}
	d.routeWallEvent(wall, true)
}

func (d *Daemon) routeWallEvent(wall *protocolpb.Wall, fanout bool) {
	d.routeWallEventWithSource(wall, d.sessionID, fanout)
}

func (d *Daemon) routeWallEventWithSource(wall *protocolpb.Wall, sourceSessionID string, fanout bool) {
	if wall == nil {
		return
	}
	msg := strings.TrimSpace(wall.GetMessage())
	if msg == "" {
		return
	}
	sourceID := strings.TrimSpace(sourceSessionID)
	if sourceID == "" {
		sourceID = d.sessionID
	}
	out := &protocolpb.Wall{
		Sender:         strings.TrimSpace(wall.GetSender()),
		Message:        msg,
		TimeoutSeconds: wall.GetTimeoutSeconds(),
	}
	frame := &protocolpb.Frame{
		SessionId: sourceID,
		Seq:       atomic.AddUint64(&d.seq, 1),
		Payload:   &protocolpb.Frame_Wall{Wall: out},
	}
	d.broadcast(frame)
	if fanout {
		d.broadcastWallToPeers(out)
	}
}

func (d *Daemon) handleInternalWall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var evt internalWallEvent
	if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	wall := &protocolpb.Wall{
		Sender:         strings.TrimSpace(evt.Sender),
		Message:        strings.TrimSpace(evt.Message),
		TimeoutSeconds: evt.TimeoutSeconds,
	}
	if wall.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}
	d.routeWallEventWithSource(wall, evt.SourceSessionID, false)
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) currentStatusWall() *protocolpb.Wall {
	d.clientsMu.Lock()
	defer d.clientsMu.Unlock()
	if d.status == nil {
		return nil
	}
	if !d.statusExp.IsZero() && d.clock.Now().UTC().After(d.statusExp) {
		d.status = nil
		d.statusExp = time.Time{}
		return nil
	}
	return &protocolpb.Wall{
		Sender:         d.status.Sender,
		Message:        d.status.Message,
		TimeoutSeconds: d.status.TimeoutSeconds,
	}
}

func (d *Daemon) broadcastWallToPeers(wall *protocolpb.Wall) {
	if wall == nil {
		return
	}
	records, err := d.store.Reconcile()
	if err != nil {
		return
	}
	for _, rec := range records {
		if strings.TrimSpace(rec.SessionID) == d.sessionID {
			continue
		}
		socketPath := strings.TrimSpace(rec.SocketPath)
		if socketPath == "" || !headless.SocketExists(socketPath) {
			continue
		}
		evt := internalWallEvent{
			SourceSessionID: d.sessionID,
			Sender:          strings.TrimSpace(wall.GetSender()),
			Message:         strings.TrimSpace(wall.GetMessage()),
			TimeoutSeconds:  wall.GetTimeoutSeconds(),
		}
		_ = postInternalWallEvent(socketPath, evt)
	}
}

func postInternalWallEvent(socketPath string, evt internalWallEvent) error {
	payload, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   750 * time.Millisecond,
	}
	defer transport.CloseIdleConnections()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://unix/internal/headless/wall", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("internal wall event failed: %s", resp.Status)
	}
	return nil
}

const resyncScrollbackChunkSize = 100

func (d *Daemon) handleClientResync(ctx context.Context, client *wsClient, snap *protocolpb.Snapshot, cols int, rows []*protocolpb.ScrollbackRow) {
	if client == nil {
		return
	}
	if len(rows) > 0 {
		for _, frame := range buildProtoScrollbackFrames(d.sessionID, cols, rows, true) {
			frame.Seq = atomic.AddUint64(&d.seq, 1)
			_ = d.writeClientFrame(ctx, client, frame)
		}
	}
	if snap == nil {
		return
	}
	cloneAny := proto.Clone(snap)
	cloned, ok := cloneAny.(*protocolpb.Snapshot)
	if !ok {
		return
	}
	frame := &protocolpb.Frame{
		SessionId: d.sessionID,
		Seq:       atomic.AddUint64(&d.seq, 1),
		Payload:   &protocolpb.Frame_Snapshot{Snapshot: cloned},
	}
	_ = d.writeClientFrame(ctx, client, frame)
}

func buildProtoScrollbackFrames(sessionID string, cols int, rows []*protocolpb.ScrollbackRow, clear bool) []*protocolpb.Frame {
	if len(rows) == 0 && !clear {
		return nil
	}
	chunkSize := resyncScrollbackChunkSize
	if chunkSize <= 0 {
		chunkSize = 100
	}
	if cols <= 0 && len(rows) > 0 {
		cols = len(rows[0].GetRunes())
	}
	frames := make([]*protocolpb.Frame, 0, 1+len(rows)/chunkSize)
	if len(rows) == 0 {
		msg := &protocolpb.Scrollback{
			Cols:  uint32(cols),
			Clear: clear,
		}
		frames = append(frames, &protocolpb.Frame{
			SessionId: sessionID,
			Payload:   &protocolpb.Frame_Scrollback{Scrollback: msg},
		})
		return frames
	}
	for i := 0; i < len(rows); i += chunkSize {
		end := i + chunkSize
		if end > len(rows) {
			end = len(rows)
		}
		msg := &protocolpb.Scrollback{
			Cols:  uint32(cols),
			Clear: clear && i == 0,
		}
		for _, row := range rows[i:end] {
			msg.Rows = append(msg.Rows, cloneScrollbackRow(row))
		}
		frames = append(frames, &protocolpb.Frame{
			SessionId: sessionID,
			Payload:   &protocolpb.Frame_Scrollback{Scrollback: msg},
		})
	}
	return frames
}

func cloneScrollbackRow(row *protocolpb.ScrollbackRow) *protocolpb.ScrollbackRow {
	if row == nil {
		return &protocolpb.ScrollbackRow{}
	}
	out := &protocolpb.ScrollbackRow{}
	if len(row.Runes) > 0 {
		out.Runes = append(out.Runes, row.Runes...)
	}
	if len(row.Modes) > 0 {
		out.Modes = append(out.Modes, row.Modes...)
	}
	if len(row.Fg) > 0 {
		out.Fg = append(out.Fg, row.Fg...)
	}
	if len(row.Bg) > 0 {
		out.Bg = append(out.Bg, row.Bg...)
	}
	if len(row.Graphemes) > 0 {
		out.Graphemes = append(out.Graphemes, row.Graphemes...)
	}
	return out
}

func (d *Daemon) writeInput(data []byte) error {
	d.stdinMu.Lock()
	defer d.stdinMu.Unlock()
	if d.stdinW == nil {
		return io.ErrClosedPipe
	}
	remaining := data
	for len(remaining) > 0 {
		n, err := d.stdinW.Write(remaining)
		if err != nil {
			return err
		}
		remaining = remaining[n:]
	}
	d.noteLocalActivity()
	return nil
}

func (d *Daemon) addClient(c *wsClient) {
	d.clientsMu.Lock()
	d.clients[c.id] = c
	d.clientsMu.Unlock()
}

func (d *Daemon) removeClient(id string) {
	d.clientsMu.Lock()
	delete(d.clients, id)
	if d.holderID == id {
		d.holderID = ""
	}
	d.clientsMu.Unlock()
}

func (d *Daemon) setHolder(holderID string) {
	if holderID == "" {
		return
	}
	d.clientsMu.Lock()
	if d.holderID == holderID {
		d.clientsMu.Unlock()
		return
	}
	d.holderID = holderID
	d.clientsMu.Unlock()
	d.broadcast(&protocolpb.Frame{SessionId: d.sessionID, Payload: &protocolpb.Frame_Control{Control: &protocolpb.Control{HolderClientId: holderID}}})
}

func (d *Daemon) broadcast(frame *protocolpb.Frame) {
	d.clientsMu.RLock()
	clients := make([]*wsClient, 0, len(d.clients))
	for _, client := range d.clients {
		clients = append(clients, client)
	}
	d.clientsMu.RUnlock()
	for _, client := range clients {
		if err := d.writeClientFrame(context.Background(), client, frame); err != nil {
			d.removeClient(client.id)
			_ = client.ws.Close(websocket.StatusInternalError, "write failed")
		}
	}
}

func (d *Daemon) writeClientFrame(ctx context.Context, client *wsClient, frame *protocolpb.Frame) error {
	if client == nil {
		return os.ErrInvalid
	}
	client.writeMu.Lock()
	defer client.writeMu.Unlock()
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return writeFrame(writeCtx, client.ws, frame)
}

func (d *Daemon) heartbeat(ctx context.Context) {
	ticker := d.clock.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = d.writeState("running", "")
		}
	}
}

func (d *Daemon) writeState(status, lastErr string) error {
	now := d.clock.Now().UTC()
	d.clientsMu.RLock()
	offline := d.offline
	d.clientsMu.RUnlock()
	return d.store.WithLock(func(state *headless.State) error {
		if state.Sessions == nil {
			state.Sessions = map[string]headless.SessionRecord{}
		}
		rec := state.Sessions[d.sessionID]
		if rec.SessionID == "" {
			rec.SessionID = d.sessionID
			rec.StartedAt = d.startedAt
		}
		rec.PID = os.Getpid()
		rec.SocketPath = d.socketPath
		rec.Endpoint = d.opts.Endpoint
		rec.LastSeenAt = now
		rec.Offline = offline
		rec.Status = status
		rec.LastError = strings.TrimSpace(lastErr)
		state.Sessions[d.sessionID] = rec
		return nil
	})
}

func (d *Daemon) ensureSessionAvailable() error {
	state, err := d.store.Load()
	if err != nil {
		return err
	}
	if state != nil {
		if rec, ok := state.Sessions[d.sessionID]; ok {
			if rec.PID > 0 && rec.PID != os.Getpid() && headless.PIDAlive(rec.PID) {
				return fmt.Errorf("headless session %q already running (pid %d)", d.sessionID, rec.PID)
			}
		}
	}
	if socketReachable(d.socketPath) {
		return fmt.Errorf("headless session %q already running (socket %q is active)", d.sessionID, d.socketPath)
	}
	return nil
}

func socketReachable(socketPath string) bool {
	if !headless.SocketExists(socketPath) {
		return false
	}
	conn, err := net.DialTimeout("unix", socketPath, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (d *Daemon) removeStateRecord() error {
	pid := os.Getpid()
	return d.store.WithLock(func(state *headless.State) error {
		rec, ok := state.Sessions[d.sessionID]
		if !ok {
			return nil
		}
		if rec.PID != pid {
			return nil
		}
		delete(state.Sessions, d.sessionID)
		return nil
	})
}

func (d *Daemon) toggleWallInactivityFallback(sessionID string) session.WallInactivityToggleResult {
	result := session.WallInactivityToggleResult{Enabled: false}
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(sessionID) != d.sessionID {
		return result
	}
	d.wallMu.Lock()
	defer d.wallMu.Unlock()
	levels := d.wallLevels
	if len(levels) == 0 {
		levels = config.DefaultWallInactiveAfterLevels()
	}
	if !d.wallEnabled {
		d.wallEnabled = true
		d.wallLevel = 0
		d.wallAfter = levels[0]
		d.wallLastActivity = d.clock.Now().UTC()
		d.wallArmed = true
		result.Enabled = true
		result.InactiveAfter = formatDurationCompact(d.wallAfter)
		return result
	}
	nextLevel := d.wallLevel + 1
	if nextLevel >= len(levels) {
		d.wallEnabled = false
		d.wallLevel = 0
		d.wallAfter = 0
		d.wallArmed = false
		result.Enabled = false
		return result
	}
	d.wallLevel = nextLevel
	d.wallAfter = levels[nextLevel]
	d.wallLastActivity = d.clock.Now().UTC()
	d.wallArmed = true
	result.Enabled = true
	result.InactiveAfter = formatDurationCompact(d.wallAfter)
	return result
}

func (d *Daemon) noteLocalActivity() {
	d.wallMu.Lock()
	d.wallLastActivity = d.clock.Now().UTC()
	if d.wallEnabled && d.wallAfter > 0 {
		d.wallArmed = true
	}
	d.wallMu.Unlock()
}

func (d *Daemon) monitorLocalWallInactivity(ctx context.Context) {
	ticker := d.clock.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		shouldSend := false
		d.wallMu.Lock()
		if d.wallEnabled && d.wallAfter > 0 && d.wallArmed && d.clock.Now().UTC().Sub(d.wallLastActivity) >= d.wallAfter {
			shouldSend = true
			d.wallArmed = false
		}
		d.wallMu.Unlock()
		if !shouldSend {
			continue
		}
		wall := &protocolpb.Wall{
			Sender:         d.sessionID,
			Message:        fmt.Sprintf("%s inactive", d.sessionID),
			TimeoutSeconds: 5,
		}
		d.routeWallEvent(wall, true)
		d.forwardWallToRelayAsync(wall)
	}
}

func formatDurationCompact(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	if d%time.Second == 0 {
		return fmt.Sprintf("%ds", d/time.Second)
	}
	seconds := d.Seconds()
	if seconds < 1 {
		return "1s"
	}
	return fmt.Sprintf("%.1fs", seconds)
}

func randomClientID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("client-%d", time.Now().UnixNano())
	}
	return "client-" + hex.EncodeToString(buf)
}

func blankSnapshot(cols, rows int) *protocolpb.Snapshot {
	if cols <= 0 {
		cols = config.DefaultTerminalCols
	}
	if rows <= 0 {
		rows = config.DefaultTerminalRows
	}
	size := cols * rows
	if size < 0 {
		size = 0
	}
	runes := make([]uint32, size)
	fg := make([]uint32, size)
	bg := make([]uint32, size)
	for i := 0; i < size; i++ {
		runes[i] = ' '
	}
	return &protocolpb.Snapshot{
		Cols:   uint32(cols),
		Rows:   uint32(rows),
		Runes:  runes,
		Fg:     fg,
		Bg:     bg,
		Cursor: &protocolpb.Cursor{X: 0, Y: 0},
	}
}

func frameError(message string) *protocolpb.Frame {
	msg := strings.TrimSpace(message)
	if msg == "" {
		msg = "error"
	}
	return &protocolpb.Frame{Payload: &protocolpb.Frame_Error{Error: &protocolpb.Error{Message: msg}}}
}

func readFrame(ctx context.Context, conn *websocket.Conn) (*protocolpb.Frame, error) {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	var frame protocolpb.Frame
	if err := proto.Unmarshal(data, &frame); err != nil {
		return nil, err
	}
	return &frame, nil
}

func writeFrame(ctx context.Context, conn *websocket.Conn, frame *protocolpb.Frame) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	data, err := proto.Marshal(frame)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageBinary, data)
}
