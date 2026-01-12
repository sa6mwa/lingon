package attach

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/term"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/tlsmgr"
	"pkt.systems/pslog"
)

// Client attaches to a remote Lingon session.
type Client struct {
	Endpoint       string
	SessionID      string
	AccessToken    string
	ShareToken     string
	RequestControl bool
	ClientID       string
	Stdin          io.Reader
	Stdout         io.Writer
	Stderr         io.Writer
	TermSize       func() (int, int)

	Logger pslog.Logger

	holderID string

	mu              sync.RWMutex
	lastSnapshot    *protocolpb.Snapshot
	lastSeq         uint64
	needsResync     bool
	resyncRequested bool
	renderMu        sync.Mutex
	writeMu         sync.Mutex
	stdin           io.Reader
	stdout          io.Writer
	stderr          io.Writer
	stdinCloser     io.Closer
	errOnce         sync.Once
	runErr          error
	controlCh       chan struct{}
	ws              *websocket.Conn
}

// Run attaches to a session and renders output.
func (c *Client) Run(ctx context.Context) error {
	if c.Logger == nil {
		c.Logger = pslog.LoggerFromEnv()
	}
	if c.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}

	wsURL, httpURL, err := normalizeEndpoint(c.Endpoint)
	if err != nil {
		return err
	}

	if c.ClientID == "" {
		c.ClientID = newClientID()
	}

	c.stdin = c.stdinReader()
	c.stdout = c.stdoutWriter()
	c.stderr = c.stderrWriter()
	if closer, ok := c.stdin.(io.Closer); ok {
		c.stdinCloser = closer
	}

	cols, rows := c.terminalSize()
	if cols == 0 || rows == 0 {
		cols, rows = config.DefaultTerminalCols, config.DefaultTerminalRows
	}

	clientTLS, err := clientTLSConfig()
	if err != nil {
		return err
	}

	dialOptions := &websocket.DialOptions{
		HTTPClient: &http.Client{
			Transport: &http.Transport{TLSClientConfig: clientTLS},
		},
	}

	if c.ShareToken == "" {
		token := c.AccessToken
		if token == "" {
			return fmt.Errorf("access token is required")
		}
		dialOptions.HTTPHeader = http.Header{"Authorization": {"Bearer " + token}}
	} else {
		wsURL = wsURL + "?token=" + url.QueryEscape(c.ShareToken)
	}

	ws, _, err := websocket.Dial(ctx, wsURL+"/ws/client", dialOptions)
	if err != nil {
		return err
	}
	c.ws = ws
	if c.controlCh == nil {
		c.controlCh = make(chan struct{}, 1)
	}
	defer func() {
		_ = ws.Close(websocket.StatusNormalClosure, "closing")
	}()

	hello := &protocolpb.Frame{
		SessionId: c.SessionID,
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			ClientId:     c.ClientID,
			Cols:         uint32(cols),
			Rows:         uint32(rows),
			WantsControl: c.RequestControl,
			ClientType:   "attach",
		}},
	}
	if err := c.writeFrame(ctx, ws, hello); err != nil {
		return err
	}

	if stdinFile, ok := c.stdin.(*os.File); ok && term.IsTerminal(int(stdinFile.Fd())) {
		stdinState, err := term.MakeRaw(int(stdinFile.Fd()))
		if err != nil {
			return err
		}
		defer func() {
			_ = term.Restore(int(stdinFile.Fd()), stdinState)
		}()
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	wsDone := make(chan struct{})
	inputDone := make(chan struct{})
	go func() {
		defer close(wsDone)
		c.readWS(ctx, ws)
		cancel()
	}()
	go func() {
		defer close(inputDone)
		c.readInput(ctx, ws)
	}()

	go func() {
		c.handleResize(ctx, ws)
	}()

	if c.shouldWaitForSignals() {
		waitForSignals(ctx, cancel)
	} else {
		<-ctx.Done()
	}
	if c.stdinCloser != nil && !c.shouldWaitForSignals() {
		_ = c.stdinCloser.Close()
	}
	<-wsDone
	if c.shouldWaitForSignals() {
		select {
		case <-inputDone:
		case <-time.After(200 * time.Millisecond):
		}
	} else {
		<-inputDone
	}

	_ = httpURL
	return c.error()
}

func (c *Client) readWS(ctx context.Context, ws *websocket.Conn) {
	for {
		frame, err := readFrame(ctx, ws)
		if err != nil {
			return
		}
		if !c.acceptSeq(frame.Seq) {
			_ = c.requestResync(ctx, ws)
			continue
		}
		if snapshot := frame.GetSnapshot(); snapshot != nil {
			c.handleSnapshot(frame.Seq, snapshot)
			continue
		}
		if diff := frame.GetDiff(); diff != nil {
			if snap := c.applyDiff(diff); snap != nil {
				c.renderSnapshot(snap)
			}
			continue
		}
		if welcome := frame.GetWelcome(); welcome != nil {
			c.handleControl(welcome.HolderClientId)
			continue
		}
		if ctrl := frame.GetControl(); ctrl != nil {
			c.handleControl(ctrl.HolderClientId)
			continue
		}
		if errMsg := frame.GetError(); errMsg != nil {
			c.setError(fmt.Errorf("server error: %s", errMsg.Message))
			return
		}
	}
}

func (c *Client) handleControl(holder string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if holder == c.holderID {
		return
	}
	c.holderID = holder
	if c.controlCh == nil {
		return
	}
	select {
	case c.controlCh <- struct{}{}:
	default:
	}
}

func (c *Client) isController() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.holderID != "" && c.holderID == c.ClientID
}

func (c *Client) handleSnapshot(seq uint64, snap *protocolpb.Snapshot) {
	if snap == nil {
		return
	}
	c.mu.Lock()
	c.lastSnapshot = snap
	if seq != 0 {
		c.lastSeq = seq
	}
	c.needsResync = false
	c.resyncRequested = false
	c.mu.Unlock()
	c.renderSnapshot(snap)
}

func (c *Client) getSnapshot() *protocolpb.Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastSnapshot
}

func (c *Client) applyDiff(diff *protocolpb.Diff) *protocolpb.Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()

	if diff == nil {
		return c.lastSnapshot
	}
	cols := int(diff.Cols)
	rows := int(diff.Rows)
	if cols <= 0 || rows <= 0 {
		if c.lastSnapshot == nil {
			return nil
		}
		cols = int(c.lastSnapshot.Cols)
		rows = int(c.lastSnapshot.Rows)
	}

	if c.lastSnapshot == nil || int(c.lastSnapshot.Cols) != cols || int(c.lastSnapshot.Rows) != rows {
		c.lastSnapshot = &protocolpb.Snapshot{
			Cols:  uint32(cols),
			Rows:  uint32(rows),
			Runes: make([]uint32, cols*rows),
			Modes: make([]int32, cols*rows),
			Fg:    make([]uint32, cols*rows),
			Bg:    make([]uint32, cols*rows),
		}
	}

	snap := c.lastSnapshot
	for _, row := range diff.DiffRows {
		y := int(row.Row)
		if y < 0 || y >= rows {
			continue
		}
		start := y * cols
		for x := 0; x < cols; x++ {
			idx := start + x
			if x < len(row.Runes) {
				snap.Runes[idx] = row.Runes[x]
			}
			if x < len(row.Modes) {
				snap.Modes[idx] = row.Modes[x]
			}
			if x < len(row.Fg) {
				snap.Fg[idx] = row.Fg[x]
			}
			if x < len(row.Bg) {
				snap.Bg[idx] = row.Bg[x]
			}
		}
	}
	if diff.Cursor != nil {
		snap.Cursor = diff.Cursor
	}
	snap.CursorVisible = diff.CursorVisible
	snap.Mode = diff.Mode
	snap.Title = diff.Title
	return snap
}

func (c *Client) renderSnapshot(snap *protocolpb.Snapshot) {
	c.renderMu.Lock()
	defer c.renderMu.Unlock()
	cols, rows := c.terminalSize()
	if cols == 0 || rows == 0 {
		cols, rows = int(snap.Cols), int(snap.Rows)
	}
	_ = RenderSnapshotViewport(c.stdoutWriter(), snap, cols, rows)
}

func (c *Client) acceptSeq(seq uint64) bool {
	if seq == 0 {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.needsResync {
		return false
	}
	if c.lastSeq != 0 && seq != c.lastSeq+1 {
		c.needsResync = true
		c.resyncRequested = false
		return false
	}
	c.lastSeq = seq
	return true
}

func (c *Client) requestResync(ctx context.Context, ws *websocket.Conn) error {
	c.mu.Lock()
	if !c.needsResync || c.resyncRequested {
		c.mu.Unlock()
		return nil
	}
	c.resyncRequested = true
	c.mu.Unlock()
	return c.sendHello(ctx, ws)
}

func (c *Client) setError(err error) {
	if err == nil {
		return
	}
	c.errOnce.Do(func() {
		c.runErr = err
	})
}

func (c *Client) error() error {
	return c.runErr
}

func (c *Client) sendHello(ctx context.Context, ws *websocket.Conn) error {
	cols, rows := c.terminalSize()
	if cols == 0 || rows == 0 {
		cols, rows = config.DefaultTerminalCols, config.DefaultTerminalRows
	}
	c.mu.RLock()
	lastSeq := c.lastSeq
	c.mu.RUnlock()
	frame := &protocolpb.Frame{
		SessionId: c.SessionID,
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			ClientId:     c.ClientID,
			Cols:         uint32(cols),
			Rows:         uint32(rows),
			WantsControl: c.RequestControl,
			LastSeq:      lastSeq,
			ClientType:   "attach",
		}},
	}
	return c.writeFrame(ctx, ws, frame)
}

func newClientID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("client-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func (c *Client) writeFrame(ctx context.Context, ws *websocket.Conn, frame *protocolpb.Frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return writeFrame(ctx, ws, frame)
}

func (c *Client) readInput(ctx context.Context, ws *websocket.Conn) {
	reader := bufio.NewReader(c.stdinReader())
	buf := make([]byte, 1024)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := reader.Read(buf)
		if err != nil {
			if err != io.EOF {
				c.Logger.Debug("stdin read error", "err", err)
			}
			return
		}
		frame := &protocolpb.Frame{Payload: &protocolpb.Frame_In{In: &protocolpb.In{Data: buf[:n]}}}
		if err := c.writeFrame(ctx, ws, frame); err != nil {
			c.Logger.Debug("ws write error", "err", err)
			c.setError(err)
			return
		}
	}
}

func normalizeEndpoint(endpoint string) (string, string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", "", err
	}
	if parsed.Scheme == "" {
		return "", "", fmt.Errorf("endpoint must include scheme")
	}
	wsURL := *parsed
	httpURL := *parsed

	switch strings.ToLower(parsed.Scheme) {
	case "https":
		wsURL.Scheme = "wss"
	case "http":
		wsURL.Scheme = "ws"
	case "wss":
		httpURL.Scheme = "https"
	case "ws":
		httpURL.Scheme = "http"
	default:
		return "", "", fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}

	return wsURL.String(), httpURL.String(), nil
}

func clientTLSConfig() (*tls.Config, error) {
	pool, err := tlsmgr.LoadLocalCARoots(config.DefaultTLSDir(), nil)
	if err != nil {
		return nil, err
	}
	return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, nil
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

func (c *Client) handleResize(ctx context.Context, ws *websocket.Conn) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	defer signal.Stop(ch)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			cols, rows := c.terminalSize()
			if snap := c.getSnapshot(); snap != nil {
				c.renderSnapshot(snap)
			}
			if c.isController() {
				frame := &protocolpb.Frame{Payload: &protocolpb.Frame_Resize{Resize: &protocolpb.Resize{Cols: uint32(cols), Rows: uint32(rows)}}}
				_ = c.writeFrame(ctx, ws, frame)
			}
		case <-c.controlCh:
			if !c.isController() || ws == nil {
				continue
			}
			cols, rows := c.terminalSize()
			frame := &protocolpb.Frame{Payload: &protocolpb.Frame_Resize{Resize: &protocolpb.Resize{Cols: uint32(cols), Rows: uint32(rows)}}}
			_ = c.writeFrame(ctx, ws, frame)
		}
	}
}

func waitForSignals(ctx context.Context, cancel func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(ch)
	select {
	case <-ctx.Done():
		return
	case <-ch:
		cancel()
	}
}

func (c *Client) stdinReader() io.Reader {
	if c.stdin != nil {
		return c.stdin
	}
	if c.Stdin != nil {
		return c.Stdin
	}
	return os.Stdin
}

func (c *Client) stdoutWriter() io.Writer {
	if c.stdout != nil {
		return c.stdout
	}
	if c.Stdout != nil {
		return c.Stdout
	}
	return os.Stdout
}

func (c *Client) stderrWriter() io.Writer {
	if c.stderr != nil {
		return c.stderr
	}
	if c.Stderr != nil {
		return c.Stderr
	}
	return os.Stderr
}

func (c *Client) terminalSize() (int, int) {
	if c.TermSize != nil {
		return c.TermSize()
	}
	if outFile, ok := c.stdoutWriter().(*os.File); ok && term.IsTerminal(int(outFile.Fd())) {
		cols, rows, err := term.GetSize(int(outFile.Fd()))
		if err == nil {
			return cols, rows
		}
	}
	return 0, 0
}

func (c *Client) shouldWaitForSignals() bool {
	if inFile, ok := c.stdinReader().(*os.File); ok && term.IsTerminal(int(inFile.Fd())) {
		return true
	}
	return false
}
