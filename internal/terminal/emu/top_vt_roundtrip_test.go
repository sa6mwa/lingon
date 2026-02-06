package emu

import (
	"bytes"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"pkt.systems/lingon/internal/protocol"
	"pkt.systems/lingon/internal/pty"
	"pkt.systems/lingon/internal/render"
	"pkt.systems/lingon/internal/terminal"
)

func TestTopRenderMatchesReferenceVT(t *testing.T) {
	topPath, err := exec.LookPath("top")
	if err != nil {
		t.Skip("top not available")
	}

	const cols = 80
	const rows = 24

	cmd := exec.Command(topPath)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLUMNS=80",
		"LINES=24",
	)
	ptyFile, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("start top: %v", err)
	}
	defer func() {
		_ = ptyFile.Close()
	}()
	_ = pty.Resize(ptyFile, cols, rows)

	if err := syscall.SetNonblock(int(ptyFile.Fd()), true); err != nil {
		t.Fatalf("set nonblock: %v", err)
	}
	defer func() {
		_ = syscall.SetNonblock(int(ptyFile.Fd()), false)
	}()

	var buf bytes.Buffer
	tmp := make([]byte, 4096)
	deadline := time.Now().Add(900 * time.Millisecond)
	for time.Now().Before(deadline) {
		n, err := ptyFile.Read(tmp)
		if n > 0 {
			_, _ = buf.Write(tmp[:n])
		}
		if err != nil {
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			break
		}
	}

	_, _ = ptyFile.Write([]byte("q"))
	_ = cmd.Wait()

	raw := buf.Bytes()
	if len(raw) == 0 {
		t.Fatalf("no output from top")
	}

	emuA := New(cols, rows)
	if err := emuA.Write(raw); err != nil {
		t.Fatalf("emu write: %v", err)
	}
	snap, err := emuA.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	protoSnap := protocol.SnapshotToProto(snap)
	var rendered bytes.Buffer
	if err := render.Snapshot(&rendered, protoSnap); err != nil {
		t.Fatalf("render snapshot: %v", err)
	}
	out := rendered.Bytes()
	if len(out) == 0 {
		t.Fatalf("rendered output empty")
	}

	emuB := New(cols, rows)
	if err := emuB.Write(out); err != nil {
		t.Fatalf("emu roundtrip write: %v", err)
	}
	roundtrip, err := emuB.Snapshot()
	if err != nil {
		t.Fatalf("roundtrip snapshot: %v", err)
	}

	if diff := diffSnapshot(snap, roundtrip); diff != "" {
		t.Fatalf("snapshot mismatch: %s", diff)
	}
}

func diffSnapshot(a, b terminal.Snapshot) string {
	if a.Cols != b.Cols || a.Rows != b.Rows {
		return "size mismatch"
	}
	if a.Cursor != b.Cursor || a.CursorVisible != b.CursorVisible {
		return "cursor mismatch"
	}
	if a.Mode != b.Mode {
		return "mode mismatch"
	}
	if len(a.Cells) != len(b.Cells) {
		return "cell count mismatch"
	}
	for i := 0; i < len(a.Cells); i++ {
		ca := a.Cells[i]
		cb := b.Cells[i]
		if ca != cb {
			x := i % a.Cols
			y := i / a.Cols
			return "cell(" + itoa(x) + "," + itoa(y) + ") " +
				"a{r:" + itoa(int(ca.Rune)) + " g:" + safeCellString(ca.Grapheme) + " m:" + itoa(int(ca.Mode)) + " fg:" + utoa(ca.FG) + " bg:" + utoa(ca.BG) + "} " +
				"b{r:" + itoa(int(cb.Rune)) + " g:" + safeCellString(cb.Grapheme) + " m:" + itoa(int(cb.Mode)) + " fg:" + utoa(cb.FG) + " bg:" + utoa(cb.BG) + "}"
		}
	}
	return ""
}

func safeCellString(s string) string {
	if s == "" {
		return " "
	}
	return s
}
