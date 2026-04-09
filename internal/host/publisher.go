package host

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/backoff"
	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/logging"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/retryafter"
	"pkt.systems/lingon/internal/terminal"
	"pkt.systems/pslog"
)

// PublishOptions configures relay publishing.
type PublishOptions struct {
	Endpoint         string
	Token            string
	TokenRefresher   func(context.Context) (string, error)
	Clock            clock.Clock
	SessionID        string
	SessionName      string
	Cols             int
	Rows             int
	PublishControl   bool
	MaxReplayScreens int
	TLSDir           string
	Insecure         bool
	Logger           pslog.Logger
	BackoffPolicy    *backoff.Policy
}

// Publisher publishes terminal updates to the relay and receives remote input.
type Publisher struct {
	opts PublishOptions

	Logger            pslog.Logger
	OnInput           func([]byte)
	OnResize          func(cols, rows int)
	OnCommand         func(kind protocolpb.CommandKind)
	OnControl         func(holderID string)
	OnFrame           func(*protocolpb.Frame)
	OnSessions        func([]*protocolpb.SessionInfo)
	OnWall            func(*protocolpb.Wall)
	OnStatus          func(connected bool, err error)
	OnBackoff         func(remaining time.Duration)
	OnSessionRejected func(message string)

	mu           sync.Mutex
	lastSnap     *protocolpb.Snapshot
	lastSent     *protocolpb.Snapshot
	lastActivity atomic.Int64

	conn        *websocket.Conn
	connected   bool
	writeMu     sync.Mutex
	outputQueue *frameQueue
	maxScreens  int
	holderID    string
	wantControl bool

	scrollbackSnapshot func() []terminal.ScrollbackRow

	backoffPolicy  backoff.Policy
	backoffAttempt int

	tokenRefresher func(context.Context) (string, error)
	clock          clock.Clock
	httpClient     *http.Client
	httpTransport  *http.Transport
	offline        bool
	stateChangeCh  chan struct{}
}

// HostControlID identifies the local host controller.
const HostControlID = "host"

var publisherWSDialTimeout = 12 * time.Second
var publisherPingInterval = durationFromEnv("LINGON_HOST_PUBLISHER_PING_INTERVAL", 2*time.Second)
var publisherPingTimeout = durationFromEnv("LINGON_HOST_PUBLISHER_PING_TIMEOUT", 2*time.Second)
var publisherSessionCloseTimeout = 1500 * time.Millisecond

func durationFromEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

// NewPublisher constructs a Publisher.
func NewPublisher(opts PublishOptions) *Publisher {
	if opts.Logger == nil {
		opts.Logger = logging.Default()
	}
	if opts.Clock == nil {
		opts.Clock = clock.New()
	}
	policy := backoff.DefaultPolicy
	if opts.BackoffPolicy != nil {
		policy = *opts.BackoffPolicy
	}
	maxScreens := opts.MaxReplayScreens
	if maxScreens <= 0 {
		maxScreens = 10
	}
	return &Publisher{
		opts:           opts,
		Logger:         opts.Logger,
		backoffPolicy:  policy,
		outputQueue:    newFrameQueue(0),
		maxScreens:     maxScreens,
		tokenRefresher: opts.TokenRefresher,
		clock:          opts.Clock,
		stateChangeCh:  make(chan struct{}, 1),
	}
}

// Run connects to the relay and streams updates until context cancellation.
func (p *Publisher) Run(ctx context.Context) error {
	if p.opts.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}
	if p.opts.Token == "" && p.tokenRefresher == nil {
		return fmt.Errorf("access token is required")
	}
	if p.opts.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	defer p.closeHTTPClient()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if p.Offline() {
			if err := p.waitUntilOnline(ctx); err != nil {
				return err
			}
			p.backoffAttempt = 0
			continue
		}

		connected, err := p.connectAndServe(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if IsSessionRejectedError(err) {
			if err != nil && p.Logger != nil {
				p.Logger.Warn("host.publisher.session.rejected", "err", err)
			}
			p.SetOffline(true)
			if p.OnSessionRejected != nil {
				p.OnSessionRejected(SessionRejectedMessage(err))
			}
			p.backoffAttempt = 0
			continue
		}
		if err != nil {
			p.Logger.Debug("host.publisher.disconnect.done", "err", err)
		}
		if p.OnStatus != nil {
			p.OnStatus(false, err)
		}
		if connected {
			p.backoffAttempt = 0
		}

		if p.Offline() {
			p.backoffAttempt = 0
			continue
		}

		delay := p.backoffPolicy.Next(p.backoffAttempt)
		if retryDelay, ok := retryafter.FromError(err); ok && retryDelay > 0 {
			delay = retryDelay
			p.backoffAttempt = 0
		}
		delay = p.normalizeReconnectDelay(delay)
		p.backoffAttempt++
		if err := p.waitBackoff(ctx, delay); err != nil {
			if errors.Is(err, errPublisherOffline) {
				p.backoffAttempt = 0
				continue
			}
			return err
		}
	}
}

var errPublisherOffline = errors.New("publisher offline")

// SessionRejectedError indicates the relay explicitly rejected the host session.
type SessionRejectedError struct {
	Message string
}

func (e *SessionRejectedError) Error() string {
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = "session rejected by relay"
	}
	return "server error: " + msg
}

// IsSessionRejectedError reports whether err is a relay session rejection.
func IsSessionRejectedError(err error) bool {
	var target *SessionRejectedError
	return errors.As(err, &target)
}

// SessionRejectedMessage extracts the relay rejection message from err.
func SessionRejectedMessage(err error) string {
	var target *SessionRejectedError
	if !errors.As(err, &target) || target == nil {
		return ""
	}
	return strings.TrimSpace(target.Message)
}

func (p *Publisher) waitBackoff(ctx context.Context, delay time.Duration) error {
	delay = p.normalizeReconnectDelay(delay)
	deadline := p.clock.Now().Add(delay)
	if p.OnBackoff != nil {
		p.OnBackoff(delay)
	}

	ticker := p.clock.NewTicker(time.Second)
	defer ticker.Stop()
	timer := p.clock.NewTimer(delay)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		case <-ticker.C:
			remaining := deadline.Sub(p.clock.Now())
			if remaining < 0 {
				remaining = 0
			}
			if p.OnBackoff != nil {
				p.OnBackoff(remaining)
			}
		case <-p.stateChangeCh:
			if p.Offline() {
				return errPublisherOffline
			}
		}
	}
}

func (p *Publisher) waitUntilOnline(ctx context.Context) error {
	for p.Offline() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.stateChangeCh:
		}
	}
	return nil
}

func (p *Publisher) normalizeReconnectDelay(delay time.Duration) time.Duration {
	if delay > 0 {
		return delay
	}
	fallback := p.backoffPolicy.Base
	if fallback <= 0 {
		fallback = backoff.DefaultPolicy.Base
	}
	return fallback
}

// SetOffline toggles relay connectivity for this publisher.
func (p *Publisher) SetOffline(v bool) {
	p.mu.Lock()
	changed := p.offline != v
	p.offline = v
	conn := p.conn
	p.mu.Unlock()
	if !changed {
		return
	}
	if v && conn != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "offline")
	}
	p.notifyStateChange()
}

// Offline reports whether relay connectivity is disabled.
func (p *Publisher) Offline() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.offline
}

func (p *Publisher) notifyStateChange() {
	select {
	case p.stateChangeCh <- struct{}{}:
	default:
	}
}

func (p *Publisher) refreshToken(ctx context.Context) bool {
	if err := p.ensureToken(ctx); err != nil {
		if p.Logger != nil {
			p.Logger.Warn("host.publisher.token.refresh.failed", "err", err)
		}
		return false
	}
	return true
}

func (p *Publisher) ensureToken(ctx context.Context) error {
	if p.tokenRefresher == nil {
		return nil
	}
	token, err := p.tokenRefresher(ctx)
	if err != nil {
		return err
	}
	if token == "" {
		return fmt.Errorf("refresh returned empty token")
	}
	p.mu.Lock()
	p.opts.Token = token
	p.mu.Unlock()
	return nil
}

func (p *Publisher) dialHTTPClient() (*http.Client, error) {
	p.mu.Lock()
	if p.httpClient != nil {
		client := p.httpClient
		p.mu.Unlock()
		return client, nil
	}
	p.mu.Unlock()

	tlsCfg, err := clientTLSConfig(p.opts.TLSDir, p.opts.Insecure)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{
		Timeout:   publisherWSDialTimeout,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		TLSClientConfig:       tlsCfg,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   publisherWSDialTimeout,
		ExpectContinueTimeout: time.Second,
	}
	client := &http.Client{Transport: transport}

	p.mu.Lock()
	if p.httpClient == nil {
		p.httpClient = client
		p.httpTransport = transport
		p.mu.Unlock()
		return client, nil
	}
	existing := p.httpClient
	p.mu.Unlock()
	transport.CloseIdleConnections()
	return existing, nil
}

func (p *Publisher) closeHTTPClient() {
	p.mu.Lock()
	transport := p.httpTransport
	p.httpTransport = nil
	p.httpClient = nil
	p.mu.Unlock()
	if transport != nil {
		transport.CloseIdleConnections()
	}
}

// Publish buffers or sends updates based on connectivity.
func (p *Publisher) Publish(data []byte, snap *protocolpb.Snapshot) {
	if snap == nil {
		return
	}
	snapFrame := &protocolpb.Frame{
		SessionId: p.opts.SessionID,
		Payload:   &protocolpb.Frame_Snapshot{Snapshot: snap},
	}
	if p.outputQueue != nil {
		p.outputQueue.SetMaxBytes(proto.Size(snapFrame) * p.maxScreens)
	}
	frame := p.buildFrame(data, snap)
	if frame == nil {
		return
	}
	if p.OnFrame != nil {
		p.OnFrame(frame)
	}
	p.outputQueue.Enqueue(frame, snapFrame)
}

// Resize records a resized snapshot and publishes it.
func (p *Publisher) Resize(cols, rows int, snap *protocolpb.Snapshot) {
	p.opts.Cols = cols
	p.opts.Rows = rows
	p.Publish(nil, snap)
}

func (p *Publisher) connectAndServe(ctx context.Context) (bool, error) {
	wsBase, err := normalizeEndpoint(p.opts.Endpoint)
	if err != nil {
		return false, err
	}
	if err := p.ensureToken(ctx); err != nil {
		return false, err
	}
	httpClient, err := p.dialHTTPClient()
	if err != nil {
		return false, err
	}
	var ws *websocket.Conn
	for attempt := 0; attempt < 2; attempt++ {
		dialCtx, cancel := context.WithTimeout(ctx, publisherWSDialTimeout)
		conn, resp, dialErr := websocket.Dial(dialCtx, wsBase+"/ws/host", &websocket.DialOptions{
			HTTPHeader: map[string][]string{"Authorization": {"Bearer " + p.opts.Token}},
			HTTPClient: httpClient,
		})
		cancel()
		if dialErr != nil {
			if resp != nil && resp.StatusCode == http.StatusUnauthorized && attempt == 0 && p.refreshToken(ctx) {
				if resp.Body != nil {
					_ = resp.Body.Close()
				}
				continue
			}
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			return false, dialErr
		}
		ws = conn
		ws.SetReadLimit(config.DefaultWSReadLimit)
		break
	}
	if ws == nil {
		return false, fmt.Errorf("failed to connect to host websocket")
	}
	defer func() {
		_ = ws.Close(websocket.StatusNormalClosure, "closing")
	}()

	hello := &protocolpb.Frame{
		SessionId: p.opts.SessionID,
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			ClientId:     strings.TrimSpace(p.opts.SessionName),
			Cols:         uint32(p.opts.Cols),
			Rows:         uint32(p.opts.Rows),
			WantsControl: p.opts.PublishControl,
			ClientType:   "host",
		}},
	}
	if err := writeFrame(ctx, ws, hello); err != nil {
		return false, err
	}

	p.setConn(ws)
	defer p.clearConn()

	p.mu.Lock()
	wantControl := p.wantControl
	p.mu.Unlock()
	if wantControl {
		_ = p.sendControl(ctx, HostControlID)
	}

	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(3)
	var readErr error
	go func() {
		defer wg.Done()
		readErr = p.readWS(readCtx, ws)
		cancel()
	}()
	var writeErr error
	go func() {
		defer wg.Done()
		writeErr = p.writeLoop(readCtx, ws)
		cancel()
	}()
	var pingErr error
	go func() {
		defer wg.Done()
		pingErr = p.pingLoop(readCtx, ws, cancel)
	}()

	wg.Wait()
	if readErr != nil && !errors.Is(readErr, context.Canceled) {
		return true, readErr
	}
	if pingErr != nil && !errors.Is(pingErr, context.Canceled) {
		return true, pingErr
	}
	if writeErr != nil && !errors.Is(writeErr, context.Canceled) {
		return true, writeErr
	}
	if readErr != nil {
		return true, readErr
	}
	if pingErr != nil {
		return true, pingErr
	}
	if writeErr != nil {
		return true, writeErr
	}
	return true, nil
}

func (p *Publisher) setConn(ws *websocket.Conn) {
	p.mu.Lock()
	p.conn = ws
	p.connected = true
	p.mu.Unlock()
	p.touchActivity()
	if p.OnStatus != nil {
		p.OnStatus(true, nil)
	}
}

func (p *Publisher) clearConn() {
	p.mu.Lock()
	p.conn = nil
	p.connected = false
	p.mu.Unlock()
}

func (p *Publisher) readWS(ctx context.Context, ws *websocket.Conn) error {
	for {
		frame, err := readFrame(ctx, ws)
		if err != nil {
			return err
		}
		p.touchActivity()
		if hello := frame.GetHello(); hello != nil {
			p.sendScrollbackSnapshot()
			p.sendSnapshot()
			continue
		}
		if in := frame.GetIn(); in != nil && p.OnInput != nil {
			p.OnInput(in.Data)
			continue
		}
		if command := frame.GetCommand(); command != nil && p.OnCommand != nil {
			p.OnCommand(command.GetKind())
			continue
		}
		if resize := frame.GetResize(); resize != nil && p.OnResize != nil {
			p.OnResize(int(resize.Cols), int(resize.Rows))
			continue
		}
		if welcome := frame.GetWelcome(); welcome != nil {
			p.setHolder(welcome.HolderClientId)
			continue
		}
		if ctrl := frame.GetControl(); ctrl != nil {
			p.setHolder(ctrl.HolderClientId)
			continue
		}
		if sessions := frame.GetSessions(); sessions != nil {
			if p.OnSessions != nil {
				p.OnSessions(sessions.Sessions)
			}
			continue
		}
		if wall := frame.GetWall(); wall != nil {
			if p.OnWall != nil {
				p.OnWall(wall)
			}
			continue
		}
		if errMsg := frame.GetError(); errMsg != nil {
			msg := errMsg.Message
			if msg == "" {
				msg = "connection error"
			}
			if errMsg.GetSessionRejected() {
				return &SessionRejectedError{Message: msg}
			}
			err := fmt.Errorf("server error: %s", msg)
			if errMsg.RetryAfterSeconds > 0 {
				return &retryafter.Error{
					Err:        err,
					RetryAfter: time.Duration(errMsg.RetryAfterSeconds) * time.Second,
				}
			}
			p.Logger.Warn("host.relay.error.received", "message", errMsg.Message)
			return err
		}
	}
}

// SetScrollbackSnapshot sets the callback used to fetch scrollback for new clients.
func (p *Publisher) SetScrollbackSnapshot(fn func() []terminal.ScrollbackRow) {
	p.scrollbackSnapshot = fn
}

// PublishScrollback enqueues scrollback rows for delivery to clients.
func (p *Publisher) PublishScrollback(rows []terminal.ScrollbackRow, cols int, clear bool) {
	frames := buildScrollbackFrames(p.opts.SessionID, cols, rows, clear)
	for _, frame := range frames {
		if p.OnFrame != nil {
			p.OnFrame(frame)
		}
		if p.outputQueue != nil {
			p.outputQueue.Enqueue(frame, nil)
		}
	}
}

func (p *Publisher) sendScrollbackSnapshot() {
	if p.scrollbackSnapshot == nil {
		return
	}
	rows := p.scrollbackSnapshot()
	if len(rows) == 0 {
		return
	}
	cols := p.opts.Cols
	p.mu.Lock()
	if p.lastSnap != nil {
		cols = int(p.lastSnap.Cols)
	}
	p.mu.Unlock()
	frames := buildScrollbackFrames(p.opts.SessionID, cols, rows, true)
	for _, frame := range frames {
		p.sendFrame(frame)
	}
}

func (p *Publisher) sendFrame(frame *protocolpb.Frame) bool {
	p.mu.Lock()
	ws := p.conn
	p.mu.Unlock()
	if ws == nil {
		return false
	}
	p.writeMu.Lock()
	err := writeFrame(context.Background(), ws, frame)
	p.writeMu.Unlock()
	if err != nil {
		_ = ws.Close(websocket.StatusInternalError, "write error")
		p.clearConn()
		return false
	}
	p.touchActivity()
	return true
}

func (p *Publisher) sendSnapshot() {
	p.mu.Lock()
	snap := p.lastSnap
	p.lastSent = snap
	p.mu.Unlock()
	if snap == nil {
		return
	}
	frame := &protocolpb.Frame{
		SessionId: p.opts.SessionID,
		Payload:   &protocolpb.Frame_Snapshot{Snapshot: snap},
	}
	if p.OnFrame != nil {
		p.OnFrame(frame)
	}
	_ = p.sendFrame(frame)
}

// SendSessionClosed notifies relay that the host session terminated intentionally.
func (p *Publisher) SendSessionClosed(reason string) {
	p.mu.Lock()
	ws := p.conn
	p.mu.Unlock()
	if ws == nil {
		return
	}
	frame := &protocolpb.Frame{
		SessionId: p.opts.SessionID,
		Payload: &protocolpb.Frame_SessionClosed{SessionClosed: &protocolpb.SessionClosed{
			Reason: strings.TrimSpace(reason),
		}},
	}
	if p.OnFrame != nil {
		p.OnFrame(frame)
	}
	ctx, cancel := context.WithTimeout(context.Background(), publisherSessionCloseTimeout)
	defer cancel()
	p.writeMu.Lock()
	err := writeFrame(ctx, ws, frame)
	p.writeMu.Unlock()
	if err != nil && p.Logger != nil {
		p.Logger.Debug("host.publisher.session_closed.send.failed", "session", p.opts.SessionID, "err", err)
		return
	}
	p.touchActivity()
}

// TakeControl announces that the host wants controller lease.
func (p *Publisher) TakeControl() {
	p.mu.Lock()
	p.wantControl = true
	p.mu.Unlock()
	if p.holderID == HostControlID {
		return
	}
	_ = p.sendControl(context.Background(), HostControlID)
}

func (p *Publisher) sendControl(ctx context.Context, holderID string) error {
	frame := &protocolpb.Frame{
		SessionId: p.opts.SessionID,
		Payload: &protocolpb.Frame_Control{Control: &protocolpb.Control{
			HolderClientId: holderID,
		}},
	}
	if !p.sendFrame(frame) {
		p.mu.Lock()
		p.holderID = holderID
		p.mu.Unlock()
		return errors.New("control not sent")
	}
	p.setHolder(holderID)
	return nil
}

func (p *Publisher) setHolder(holderID string) {
	p.mu.Lock()
	p.holderID = holderID
	cb := p.OnControl
	p.mu.Unlock()
	if cb != nil {
		cb(holderID)
	}
}

func (p *Publisher) buildFrame(data []byte, snap *protocolpb.Snapshot) *protocolpb.Frame {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastSnap = snap
	diff, shouldSendSnapshot := diffSnapshots(p.lastSent, snap)
	if shouldSendSnapshot {
		p.lastSent = snap
		return &protocolpb.Frame{
			SessionId: p.opts.SessionID,
			Payload:   &protocolpb.Frame_Snapshot{Snapshot: snap},
		}
	}
	if diff != nil {
		p.lastSent = snap
		return &protocolpb.Frame{
			SessionId: p.opts.SessionID,
			Payload:   &protocolpb.Frame_Diff{Diff: diff},
		}
	}
	return nil
}

func (p *Publisher) writeLoop(ctx context.Context, ws *websocket.Conn) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if frame := p.outputQueue.Pop(); frame != nil {
			p.writeMu.Lock()
			err := writeFrame(context.Background(), ws, frame)
			p.writeMu.Unlock()
			if err != nil {
				_ = ws.Close(websocket.StatusInternalError, "write error")
				p.clearConn()
				return err
			}
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.outputQueue.Notify():
		}
	}
}

func (p *Publisher) pingLoop(ctx context.Context, ws *websocket.Conn, cancel context.CancelFunc) error {
	if ws == nil {
		return nil
	}
	interval := publisherPingInterval
	if interval <= 0 {
		return nil
	}
	timeout := publisherPingTimeout
	if timeout <= 0 {
		timeout = interval
	}
	ticker := p.clock.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		if p.idleFor() < interval {
			continue
		}
		if !p.writeMu.TryLock() {
			continue
		}
		pingCtx, pingCancel := context.WithTimeout(ctx, timeout)
		err := ws.Ping(pingCtx)
		pingCancel()
		p.writeMu.Unlock()
		if err == nil {
			p.touchActivity()
			continue
		}
		_ = ws.Close(websocket.StatusInternalError, "ping timeout")
		p.clearConn()
		if cancel != nil {
			cancel()
		}
		return err
	}
}

func (p *Publisher) touchActivity() {
	if p == nil {
		return
	}
	if p.clock == nil {
		return
	}
	p.lastActivity.Store(p.clock.Now().UnixNano())
}

func (p *Publisher) idleFor() time.Duration {
	if p == nil || p.clock == nil {
		return 0
	}
	nanos := p.lastActivity.Load()
	if nanos <= 0 {
		return 0
	}
	last := time.Unix(0, nanos)
	if now := p.clock.Now(); now.After(last) {
		return now.Sub(last)
	}
	return 0
}
