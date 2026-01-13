package emu

import (
	"bytes"
	"image/color"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"

	"pkt.systems/lingon/internal/protocol"
	"pkt.systems/lingon/internal/pty"
	"pkt.systems/lingon/internal/render"
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

	ref := vt.NewEmulator(cols, rows)
	if _, err := ref.Write(raw); err != nil {
		t.Fatalf("vt ref write: %v", err)
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

	got := vt.NewEmulator(cols, rows)
	if _, err := got.Write(out); err != nil {
		t.Fatalf("vt got write: %v", err)
	}

	if diff := diffVT(ref, got); diff != "" {
		t.Fatalf("vt mismatch: %s", diff)
	}
}

type vtCellInfo struct {
	content string
	fg      uint32
	bg      uint32
	attrs   uv.StyleAttr
}

func diffVT(a, b *vt.Emulator) string {
	w := a.Width()
	h := a.Height()
	if b.Width() != w || b.Height() != h {
		return "size mismatch"
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			aa := vtCellAt(a, x, y)
			bb := vtCellAt(b, x, y)
			if aa != bb {
				return "cell(" + itoa(x) + "," + itoa(y) + ") " +
					"a{c:" + safeCellString(aa.content) + " attrs:" + itoa(int(aa.attrs)) + " fg:" + utoa(uint32(aa.fg)) + " bg:" + utoa(uint32(aa.bg)) + "} " +
					"b{c:" + safeCellString(bb.content) + " attrs:" + itoa(int(bb.attrs)) + " fg:" + utoa(uint32(bb.fg)) + " bg:" + utoa(uint32(bb.bg)) + "}"
			}
		}
	}
	return ""
}

func vtCellAt(t *vt.Emulator, x, y int) vtCellInfo {
	cell := t.CellAt(x, y)
	if cell == nil {
		return vtCellInfo{
			content: " ",
			fg:      colorKey(t.ForegroundColor()),
			bg:      colorKey(t.BackgroundColor()),
		}
	}
	content := cell.Content
	if content == "" {
		content = " "
	}
	fg := cell.Style.Fg
	bg := cell.Style.Bg
	if fg == nil {
		fg = t.ForegroundColor()
	}
	if bg == nil {
		bg = t.BackgroundColor()
	}
	fgKey := colorKey(fg)
	bgKey := colorKey(bg)
	if cell.Style.Attrs&uv.AttrReverse != 0 {
		fgKey, bgKey = bgKey, fgKey
	}
	return vtCellInfo{
		content: content,
		fg:      fgKey,
		bg:      bgKey,
		attrs:   cell.Style.Attrs,
	}
}

func colorKey(c color.Color) uint32 {
	if c == nil {
		return 0
	}
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	return uint32(n.R)<<24 | uint32(n.G)<<16 | uint32(n.B)<<8 | uint32(n.A)
}

func safeCellString(s string) string {
	if s == "" {
		return " "
	}
	return s
}
