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

func TestTopRoundTripRender(t *testing.T) {
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
	deadline := time.Now().Add(800 * time.Millisecond)
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

	emuA := New(cols, rows)
	if err := emuA.Write(buf.Bytes()); err != nil {
		t.Fatalf("emuA write: %v", err)
	}
	snapA, err := emuA.Snapshot()
	if err != nil {
		t.Fatalf("snapA: %v", err)
	}

	protoSnap := protocol.SnapshotToProto(snapA)
	var rendered bytes.Buffer
	if err := render.Snapshot(&rendered, protoSnap); err != nil {
		t.Fatalf("render snapshot: %v", err)
	}

	emuB := New(cols, rows)
	if err := emuB.Write(rendered.Bytes()); err != nil {
		t.Fatalf("emuB write: %v", err)
	}
	snapB, err := emuB.Snapshot()
	if err != nil {
		t.Fatalf("snapB: %v", err)
	}

	if diff := firstCellDiff(snapA, snapB); diff != "" {
		t.Fatalf("roundtrip mismatch: %s", diff)
	}
}

func firstCellDiff(a, b terminal.Snapshot) string {
	if a.Cols != b.Cols || a.Rows != b.Rows {
		return "size mismatch"
	}
	for y := 0; y < a.Rows; y++ {
		for x := 0; x < a.Cols; x++ {
			ca, errA := a.CellAt(x, y)
			cb, errB := b.CellAt(x, y)
			if errA != nil || errB != nil {
				return "cell access error"
			}
			if ca.Rune != cb.Rune || ca.Mode != cb.Mode || ca.FG != cb.FG || ca.BG != cb.BG {
				return formatCellDiff(x, y, ca, cb)
			}
		}
	}
	return ""
}

func formatCellDiff(x, y int, a, b terminal.Cell) string {
	return formatCell(x, y, a, b)
}

func formatCell(x, y int, a, b terminal.Cell) string {
	return "cell(" + itoa(x) + "," + itoa(y) + ") " +
		"a{r:" + runeString(a.Rune) + " m:" + itoa(int(a.Mode)) + " fg:" + utoa(a.FG) + " bg:" + utoa(a.BG) + "} " +
		"b{r:" + runeString(b.Rune) + " m:" + itoa(int(b.Mode)) + " fg:" + utoa(b.FG) + " bg:" + utoa(b.BG) + "}"
}

func runeString(r rune) string {
	if r == 0 {
		return "0"
	}
	if r < 32 || r == 127 {
		return "0x" + itoa(int(r))
	}
	return string(r)
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := false
	if v < 0 {
		neg = true
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func utoa(v uint32) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
