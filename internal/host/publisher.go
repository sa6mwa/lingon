package host

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/pslog"
)

// PublishOptions configures relay publishing.
type PublishOptions struct {
	Endpoint       string
	Token          string
	SessionID      string
	Cols           int
	Rows           int
	PublishControl bool
	BufferLines    int
	Logger         pslog.Logger
}

// Publisher publishes terminal updates to the relay and receives remote input.
type Publisher struct {
	opts PublishOptions

	Logger   pslog.Logger
	OnInput  func([]byte)
	OnResize func(cols, rows int)
	OnControl func(holderID string)

	mu       sync.Mutex
	lastSnap *protocolpb.Snapshot
	lastSent *protocolpb.Snapshot

	conn        *websocket.Conn
	connected   bool
	writeMu     sync.Mutex
	buffer      []bufferedFrame
	bufferLines int
	bufferUsed  int
	holderID    string
	wantControl bool
}

// HostControlID identifies the local host controller.
const HostControlID = "host"

// NewPublisher constructs a Publisher.
func NewPublisher(opts PublishOptions) *Publisher {
	if opts.Logger == nil {
		opts.Logger = pslog.LoggerFromEnv()
	}
	return &Publisher{
		opts:     opts,
		Logger:   opts.Logger,
		bufferLines: opts.BufferLines,
	}
}

// Run connects to the relay and streams updates until context cancellation.
func (p *Publisher) Run(ctx context.Context) error {
	if p.opts.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}
	if p.opts.Token == "" {
		return fmt.Errorf("access token is required")
	}
	if p.opts.SessionID == "" {
		return fmt.Errorf("session id is required")
	}

	if p.bufferLines <= 0 {
		p.bufferLines = config.DefaultBufferLines
	}

	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		connected, err := p.connectAndServe(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			p.Logger.Debug("publisher disconnected", "err", err)
		}
		if connected {
			backoff = time.Second
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

// Publish buffers or sends updates based on connectivity.
func (p *Publisher) Publish(data []byte, snap *protocolpb.Snapshot) {
	if snap == nil {
		return
	}
	frame, lines := p.buildFrame(data, snap)
	if frame == nil {
		return
	}
	if !p.sendFrame(frame) {
		p.enqueue(frame, lines)
	}
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
	tlsCfg, err := clientTLSConfig()
	if err != nil {
		return false, err
	}
	httpClient := &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
	}
	ws, _, err := websocket.Dial(ctx, wsBase+"/ws/host", &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + p.opts.Token}},
		HTTPClient: httpClient,
	})
	if err != nil {
		return false, err
	}
	defer func() {
		_ = ws.Close(websocket.StatusNormalClosure, "closing")
	}()

	hello := &protocolpb.Frame{
		SessionId: p.opts.SessionID,
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
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

	if err := p.flushBuffer(ctx); err != nil {
		return true, err
	}

	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		p.readWS(readCtx, ws)
		cancel()
	}()

	wg.Wait()
	return true, nil
}

func (p *Publisher) setConn(ws *websocket.Conn) {
	p.mu.Lock()
	p.conn = ws
	p.connected = true
	p.mu.Unlock()
}

func (p *Publisher) clearConn() {
	p.mu.Lock()
	p.conn = nil
	p.connected = false
	p.mu.Unlock()
}

func (p *Publisher) readWS(ctx context.Context, ws *websocket.Conn) {
	for {
		frame, err := readFrame(ctx, ws)
		if err != nil {
			return
		}
		if in := frame.GetIn(); in != nil && p.OnInput != nil {
			p.OnInput(in.Data)
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
		if errMsg := frame.GetError(); errMsg != nil {
			p.Logger.Warn("relay error", "message", errMsg.Message)
			return
		}
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
		p.clearConn()
		return false
	}
	return true
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

func (p *Publisher) flushBuffer(ctx context.Context) error {
	p.mu.Lock()
	queue := make([]bufferedFrame, len(p.buffer))
	copy(queue, p.buffer)
	p.buffer = nil
	p.bufferUsed = 0
	p.mu.Unlock()

	for _, entry := range queue {
		if entry.frame == nil {
			continue
		}
		if !p.sendFrame(entry.frame) {
			p.enqueue(entry.frame, entry.lines)
			return errors.New("disconnected")
		}
	}
	return nil
}

func (p *Publisher) buildFrame(data []byte, snap *protocolpb.Snapshot) (*protocolpb.Frame, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastSnap = snap
	diff, shouldSendSnapshot := diffSnapshots(p.lastSent, snap)
	if shouldSendSnapshot {
		lines := snapshotLines(snap, data)
		p.lastSent = snap
		return &protocolpb.Frame{
			SessionId: p.opts.SessionID,
			Payload:   &protocolpb.Frame_Snapshot{Snapshot: snap},
		}, lines
	}
	if diff != nil {
		lines := diffLines(diff, data)
		p.lastSent = snap
		return &protocolpb.Frame{
			SessionId: p.opts.SessionID,
			Payload:   &protocolpb.Frame_Diff{Diff: diff},
		}, lines
	}
	return nil, 0
}

func (p *Publisher) enqueue(frame *protocolpb.Frame, lines int) {
	if p.bufferLines <= 0 {
		return
	}
	p.mu.Lock()
	p.buffer = append(p.buffer, bufferedFrame{frame: frame, lines: lines})
	p.bufferUsed += lines
	for p.bufferUsed > p.bufferLines && len(p.buffer) > 0 {
		p.bufferUsed -= p.buffer[0].lines
		p.buffer = p.buffer[1:]
	}
	if len(p.buffer) > 0 {
		if p.buffer[0].frame.GetSnapshot() == nil {
			if p.lastSnap != nil {
				p.buffer = []bufferedFrame{{
					frame: &protocolpb.Frame{
						SessionId: p.opts.SessionID,
						Payload:   &protocolpb.Frame_Snapshot{Snapshot: p.lastSnap},
					},
					lines: p.bufferLines,
				}}
				p.bufferUsed = p.bufferLines
			}
		}
	}
	p.mu.Unlock()
}

func countLines(data []byte) int {
	count := 0
	for _, b := range data {
		if b == '\n' {
			count++
		}
	}
	return count
}

func snapshotLines(snap *protocolpb.Snapshot, data []byte) int {
	lines := countLines(data)
	if lines > 0 {
		return lines
	}
	if snap == nil || snap.Rows == 0 {
		return 1
	}
	return int(snap.Rows)
}

func diffLines(diff *protocolpb.Diff, data []byte) int {
	lines := countLines(data)
	if lines > 0 {
		return lines
	}
	if diff == nil || len(diff.DiffRows) == 0 {
		return 1
	}
	return len(diff.DiffRows)
}

type bufferedFrame struct {
	frame *protocolpb.Frame
	lines int
}
