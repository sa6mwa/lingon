package host

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/protocolpb"
	"pkt.systems/lingon/internal/terminal"
	"pkt.systems/lingon/internal/terminal/emu"
	"pkt.systems/pslog"
)

// Host connects to the relay and publishes a terminal session.
type Host struct {
	Endpoint  string
	Token     string
	SessionID string
	Cols      int
	Rows      int
	Command   []string
	OnInput   func([]byte)
	OnPTYRead func([]byte)
	OnFrame   func(*protocolpb.Frame)

	Logger pslog.Logger

	emulator terminal.Emulator
	emuMu    sync.Mutex
	lastSnap *protocolpb.Snapshot
}

// Run starts the host session.
func (h *Host) Run(ctx context.Context) error {
	if h.Logger == nil {
		h.Logger = pslog.LoggerFromEnv()
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
	tlsCfg, err := clientTLSConfig()
	if err != nil {
		return err
	}

	cmd, ptyFile, err := startCommand(h.Command)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		_ = ptyFile.Close()
		_ = cmd.Process.Kill()
	}()
	defer func() {
		_ = cmd.Process.Kill()
		_ = ptyFile.Close()
	}()

	h.emulator = emu.New(h.Cols, h.Rows)
	if err := resizePTY(ptyFile, h.Cols, h.Rows); err != nil {
		h.Logger.Debug("pty resize failed", "err", err)
	}

	httpClient := &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
	}
	ws, _, err := websocket.Dial(ctx, wsBase+"/ws/host", &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Authorization": {"Bearer " + h.Token}},
		HTTPClient: httpClient,
	})
	if err != nil {
		return err
	}
	defer func() {
		_ = ws.Close(websocket.StatusNormalClosure, "closing")
	}()

	hello := &protocolpb.Frame{
		SessionId: h.SessionID,
		Payload: &protocolpb.Frame_Hello{Hello: &protocolpb.Hello{
			Cols:         uint32(h.Cols),
			Rows:         uint32(h.Rows),
			WantsControl: true,
			ClientType:   "host",
		}},
	}
	if err := writeFrame(ctx, ws, hello); err != nil {
		return err
	}

	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		h.readPTY(readCtx, ptyFile, h.emulator, ws)
	}()
	go func() {
		defer wg.Done()
		h.readWS(readCtx, ws, ptyFile)
	}()

	wg.Wait()
	return nil
}

func (h *Host) readPTY(ctx context.Context, ptyFile *os.File, emulator terminal.Emulator, ws *websocket.Conn) {
	reader := bufio.NewReader(ptyFile)
	buf := make([]byte, 4096)

	for {
		n, err := reader.Read(buf)
		if err != nil {
			if err != io.EOF {
				h.Logger.Debug("pty read error", "err", err)
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
		h.emuMu.Lock()
		if err := emulator.Write(data); err != nil {
			h.Logger.Debug("emulator write error", "err", err)
		}
		snap, err := h.snapshotLocked()
		if err != nil {
			h.emuMu.Unlock()
			h.Logger.Debug("snapshot error", "err", err)
			return
		}
		diff, shouldSendSnapshot := diffSnapshots(h.lastSnap, snap)
		if shouldSendSnapshot {
			h.lastSnap = snap
			h.emuMu.Unlock()
			frame = &protocolpb.Frame{
				SessionId: h.SessionID,
				Payload:   &protocolpb.Frame_Snapshot{Snapshot: snap},
			}
		} else if diff != nil {
			h.lastSnap = snap
			h.emuMu.Unlock()
			frame = &protocolpb.Frame{
				SessionId: h.SessionID,
				Payload:   &protocolpb.Frame_Diff{Diff: diff},
			}
		} else {
			h.emuMu.Unlock()
		}
		if frame == nil {
			continue
		}
		if h.OnFrame != nil {
			h.OnFrame(frame)
		}
		if err := writeFrame(ctx, ws, frame); err != nil {
			h.Logger.Debug("ws write error", "err", err)
			return
		}
	}
}

func (h *Host) readWS(ctx context.Context, ws *websocket.Conn, ptyFile *os.File) {
	for {
		frame, err := readFrame(ctx, ws)
		if err != nil {
			return
		}
		if hello := frame.GetHello(); hello != nil {
			_ = h.sendSnapshot(ctx, ws)
		}
		if in := frame.GetIn(); in != nil {
			if h.OnInput != nil && len(in.Data) > 0 {
				cp := make([]byte, len(in.Data))
				copy(cp, in.Data)
				h.OnInput(cp)
			}
			if _, err := ptyFile.Write(in.Data); err != nil {
				h.Logger.Debug("pty write error", "err", err)
				return
			}
		}
		if resize := frame.GetResize(); resize != nil {
			cols := int(resize.Cols)
			rows := int(resize.Rows)
			if cols > 0 && rows > 0 {
				if err := resizePTY(ptyFile, cols, rows); err != nil {
					h.Logger.Debug("pty resize error", "err", err)
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
					_ = writeFrame(ctx, ws, frame)
				}
				h.Cols = cols
				h.Rows = rows
			}
		}
	}
}

func (h *Host) sendSnapshot(ctx context.Context, ws *websocket.Conn) error {
	snap, err := h.snapshot()
	if err != nil {
		return err
	}
	h.emuMu.Lock()
	h.lastSnap = snap
	h.emuMu.Unlock()
	frame := &protocolpb.Frame{
		SessionId: h.SessionID,
		Payload:   &protocolpb.Frame_Snapshot{Snapshot: snap},
	}
	return writeFrame(ctx, ws, frame)
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
