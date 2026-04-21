package host

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/logging"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
	"pkt.systems/lingon/internal/terminal/emu"
	"pkt.systems/pslog"
)

// Host connects to the relay and publishes a terminal session.
type Host struct {
	Endpoint    string
	Token       string
	SessionID   string
	SessionName string
	Headless    bool
	Cols        int
	Rows        int
	Command     []string
	TLSDir      string
	Insecure    bool
	// MaxReplayScreens caps buffered output to this many screens before compacting.
	MaxReplayScreens int
	// ScrollbackLines caps the scrollback buffer rows.
	ScrollbackLines int
	OnInput         func([]byte)
	OnPTYRead       func([]byte)
	OnFrame         func(*protocolpb.Frame)

	Logger pslog.Logger

	emulator terminal.Emulator
	emuMu    sync.Mutex
	lastSnap *protocolpb.Snapshot
}

// Run starts the host session.
func (h *Host) Run(ctx context.Context) error {
	if h.Logger == nil {
		h.Logger = logging.Default()
	}
	if h.SessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if h.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}
	if h.Token == "" {
		return fmt.Errorf("access token is required")
	}
	if h.Cols <= 0 {
		h.Cols = config.DefaultTerminalCols
	}
	if h.Rows <= 0 {
		h.Rows = config.DefaultTerminalRows
	}

	wsBase, err := normalizeEndpoint(h.Endpoint)
	if err != nil {
		return err
	}
	tlsCfg, err := clientTLSConfig(h.TLSDir, h.Insecure)
	if err != nil {
		return err
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	cmd, ptyFile, err := startCommand(h.Command)
	if err != nil {
		return err
	}
	go func() {
		<-runCtx.Done()
		_ = ptyFile.Close()
		_ = cmd.Process.Kill()
	}()
	defer func() {
		_ = cmd.Process.Kill()
		_ = ptyFile.Close()
	}()

	h.emulator = emu.New(h.Cols, h.Rows)
	if scrollback, ok := h.emulator.(*emu.Emulator); ok {
		scrollback.SetScrollbackLimit(h.ScrollbackLines)
	}
	if err := resizePTY(ptyFile, h.Cols, h.Rows); err != nil {
		h.Logger.Debug("host.pty.resize.failed", "err", err)
	}

	screens := h.MaxReplayScreens
	if screens <= 0 {
		screens = 10
	}
	outputQueue := newFrameQueue(0)
	if snap, err := h.snapshot(); err == nil {
		snapFrame := &protocolpb.Frame{
			SessionId: h.SessionID,
			Payload:   &protocolpb.Frame_Snapshot{Snapshot: snap},
		}
		outputQueue.SetMaxBytes(proto.Size(snapFrame) * screens)
	}

	httpClient := &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
	}
	ws, _, err := websocket.Dial(runCtx, wsBase+"/ws/host", &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + h.Token}},
		HTTPClient: httpClient,
	})
	if err != nil {
		return err
	}
	ws.SetReadLimit(config.DefaultWSReadLimit)
	defer func() {
		_ = ws.Close(websocket.StatusNormalClosure, "closing")
	}()

	hello := &protocolpb.Frame{
		SessionId: h.SessionID,
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			ClientId:     strings.TrimSpace(h.SessionName),
			Cols:         uint32(h.Cols),
			Rows:         uint32(h.Rows),
			WantsControl: true,
			ClientType:   "host",
			Headless:     h.Headless,
		}},
	}
	if err := writeFrame(runCtx, ws, hello); err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(4)

	ctrlFrames := make(chan *protocolpb.Frame, 4)
	ptyInput := make(chan []byte, 4096)

	go func() {
		defer wg.Done()
		if err := h.writeFrames(runCtx, ws, ctrlFrames, outputQueue); err != nil && runCtx.Err() == nil {
			h.Logger.Debug("host.ws.write.failed", "err", err)
		}
	}()

	go func() {
		defer wg.Done()
		h.writePTY(runCtx, ptyFile, ptyInput)
		cancelRun()
	}()

	go func() {
		defer wg.Done()
		h.readPTY(runCtx, ptyFile, h.emulator, ctrlFrames, outputQueue, screens)
		cancelRun()
	}()
	go func() {
		defer wg.Done()
		h.readWS(runCtx, ws, ptyFile, ctrlFrames, ptyInput, outputQueue, screens)
	}()

	wg.Wait()
	return nil
}

func (h *Host) readPTY(ctx context.Context, ptyFile *os.File, emulator terminal.Emulator, ctrlFrames chan<- *protocolpb.Frame, outputQueue *frameQueue, screens int) {
	reader := bufio.NewReader(ptyFile)
	buf := make([]byte, 4096)

	for {
		n, err := reader.Read(buf)
		if err != nil {
			if err != io.EOF {
				h.Logger.Debug("host.pty.read.failed", "err", err)
			}
			return
		}
		data := buf[:n]
		if h.OnPTYRead != nil {
			cp := make([]byte, len(data))
			copy(cp, data)
			h.OnPTYRead(cp)
		}
		var frame *protocolpb.Frame
		var scrollFrames []*protocolpb.Frame
		h.emuMu.Lock()
		if err := emulator.Write(data); err != nil {
			h.Logger.Debug("host.emulator.write.failed", "err", err)
		}
		snap, err := h.snapshotLocked()
		if err != nil {
			h.emuMu.Unlock()
			h.Logger.Debug("host.emulator.snapshot.failed", "err", err)
			return
		}
		snapFrame := &protocolpb.Frame{
			SessionId: h.SessionID,
			Payload:   &protocolpb.Frame_Snapshot{Snapshot: snap},
		}
		if screens > 0 {
			outputQueue.SetMaxBytes(proto.Size(snapFrame) * screens)
		}
		diff, shouldSendSnapshot := diffSnapshots(h.lastSnap, snap)
		if shouldSendSnapshot {
			h.lastSnap = snap
			frame = snapFrame
		} else if diff != nil {
			h.lastSnap = snap
			frame = &protocolpb.Frame{
				SessionId: h.SessionID,
				Payload:   &protocolpb.Frame_Diff{Diff: diff},
			}
		}
		if scrollback, ok := emulator.(*emu.Emulator); ok {
			rows := scrollback.DrainScrollback()
			scrollFrames = buildScrollbackFrames(h.SessionID, int(snap.Cols), rows, false)
		}
		h.emuMu.Unlock()
		if frame == nil {
			if len(scrollFrames) == 0 {
				continue
			}
		}
		if len(data) > 0 {
			enqueueControl(ctx, ctrlFrames, activityFrame(h.SessionID))
		}
		for _, scrollFrame := range scrollFrames {
			if h.OnFrame != nil {
				h.OnFrame(scrollFrame)
			}
			outputQueue.Enqueue(scrollFrame, snapFrame)
		}
		if frame == nil {
			continue
		}
		if h.OnFrame != nil {
			h.OnFrame(frame)
		}
		outputQueue.Enqueue(frame, snapFrame)
	}
}

func (h *Host) readWS(ctx context.Context, ws *websocket.Conn, ptyFile *os.File, ctrlFrames chan<- *protocolpb.Frame, ptyInput chan []byte, outputQueue *frameQueue, screens int) {
	for {
		frame, err := readFrame(ctx, ws)
		if err != nil {
			h.Logger.Debug("host.ws.read.failed", "err", err)
			return
		}
		if hello := frame.GetHello(); hello != nil {
			if snap, err := h.snapshot(); err == nil {
				h.emuMu.Lock()
				h.lastSnap = snap
				var scrollFrames []*protocolpb.Frame
				if scrollback, ok := h.emulator.(*emu.Emulator); ok {
					rows := scrollback.ScrollbackSnapshot()
					scrollFrames = buildScrollbackFrames(h.SessionID, int(snap.Cols), rows, true)
				}
				h.emuMu.Unlock()
				for _, scrollFrame := range scrollFrames {
					enqueueControl(ctx, ctrlFrames, scrollFrame)
				}
				snapFrame := &protocolpb.Frame{
					SessionId: h.SessionID,
					Payload:   &protocolpb.Frame_Snapshot{Snapshot: snap},
				}
				if screens > 0 {
					outputQueue.SetMaxBytes(proto.Size(snapFrame) * screens)
				}
				enqueueControl(ctx, ctrlFrames, snapFrame)
			}
		}
		if in := frame.GetIn(); in != nil {
			if h.OnInput != nil && len(in.Data) > 0 {
				cp := make([]byte, len(in.Data))
				copy(cp, in.Data)
				h.OnInput(cp)
			}
			if len(in.Data) > 0 {
				cp := make([]byte, len(in.Data))
				copy(cp, in.Data)
				select {
				case <-ctx.Done():
					return
				case ptyInput <- cp:
				}
			}
		}
		if command := frame.GetCommand(); command != nil {
			switch command.GetKind() {
			case protocolpb.CommandKind_COMMAND_KIND_SEND_EOF:
				cp := []byte{0x04}
				if h.OnInput != nil {
					h.OnInput(cp)
				}
				select {
				case <-ctx.Done():
					return
				case ptyInput <- cp:
				}
			}
		}
		if resize := frame.GetResize(); resize != nil {
			cols := int(resize.Cols)
			rows := int(resize.Rows)
			if cols > 0 && rows > 0 {
				if err := resizePTY(ptyFile, cols, rows); err != nil {
					h.Logger.Debug("host.pty.resize.failed", "err", err)
				}
				h.emuMu.Lock()
				h.emulator.Resize(cols, rows)
				snap, err := h.snapshotLocked()
				if err == nil {
					h.lastSnap = snap
				}
				h.emuMu.Unlock()
				if err == nil {
					frame := &protocolpb.Frame{
						SessionId: h.SessionID,
						Payload:   &protocolpb.Frame_Snapshot{Snapshot: snap},
					}
					if screens > 0 {
						outputQueue.SetMaxBytes(proto.Size(frame) * screens)
					}
					enqueueControl(ctx, ctrlFrames, frame)
				}
				h.Cols = cols
				h.Rows = rows
			}
		}
	}
}

func (h *Host) writeFrames(ctx context.Context, ws *websocket.Conn, ctrlFrames <-chan *protocolpb.Frame, outputQueue *frameQueue) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case frame := <-ctrlFrames:
			if frame == nil {
				continue
			}
			if err := writeFrame(ctx, ws, frame); err != nil {
				return err
			}
		default:
		}

		if frame := outputQueue.Pop(); frame != nil {
			if err := writeFrame(ctx, ws, frame); err != nil {
				return err
			}
			continue
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case frame := <-ctrlFrames:
			if frame == nil {
				continue
			}
			if err := writeFrame(ctx, ws, frame); err != nil {
				return err
			}
		case <-outputQueue.Notify():
		}
	}
}

func (h *Host) writePTY(ctx context.Context, ptyFile *os.File, input <-chan []byte) {
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-input:
			if len(data) == 0 {
				continue
			}
			for len(data) > 0 {
				n, err := ptyFile.Write(data)
				if err != nil {
					h.Logger.Debug("host.pty.write.failed", "err", err)
					return
				}
				data = data[n:]
			}
		}
	}
}

func enqueueControl(ctx context.Context, ch chan<- *protocolpb.Frame, frame *protocolpb.Frame) {
	select {
	case <-ctx.Done():
		return
	case ch <- frame:
	}
}

func (h *Host) snapshot() (*protocolpb.Snapshot, error) {
	h.emuMu.Lock()
	defer h.emuMu.Unlock()
	return h.snapshotLocked()
}

func (h *Host) snapshotLocked() (*protocolpb.Snapshot, error) {
	if h.emulator == nil {
		return nil, fmt.Errorf("emulator not initialized")
	}
	snap, err := h.emulator.Snapshot()
	if err != nil {
		return nil, err
	}
	return snapshotToProto(snap), nil
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

func startCommand(command []string) (*exec.Cmd, *os.File, error) {
	if len(command) == 0 {
		return startShell()
	}
	cmd := exec.Command(command[0], command[1:]...)
	ptyFile, err := startPTY(cmd)
	if err != nil {
		return nil, nil, err
	}
	return cmd, ptyFile, nil
}

func startShell() (*exec.Cmd, *os.File, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell)
	ptyFile, err := startPTY(cmd)
	if err != nil {
		return nil, nil, err
	}
	return cmd, ptyFile, nil
}
