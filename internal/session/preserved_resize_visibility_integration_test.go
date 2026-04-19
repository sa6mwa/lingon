package session

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"

	"pkt.systems/lingon/internal/terminal"
	"pkt.systems/lingon/internal/terminal/emu"
)

func TestHostShrinkHidesPreservedRowsUntilLocalPTYExpands(t *testing.T) {
	shell := preservedResizeShell(t)

	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open: %v", err)
	}
	if err := pty.Setsize(slave, &pty.Winsize{Cols: 40, Rows: 12}); err != nil {
		t.Fatalf("pty.Setsize: %v", err)
	}
	screen := newTestPTYView(t, master, slave, 40, 12)
	t.Cleanup(func() {
		screen.Close()
		_ = master.Close()
		_ = slave.Close()
	})

	runner := New(Options{
		SessionID:           "preserved-live-visibility",
		SessionName:         "preserved-live-visibility",
		Shell:               shell,
		Cols:                40,
		Rows:                12,
		Publish:             false,
		Stdin:               slave,
		Stdout:              slave,
		DisableRaw:          true,
		DisableSignalResize: true,
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		_ = runner.Run(ctx)
	}()

	screen.Eventually(3*time.Second, 50*time.Millisecond, func(scr testScreen) error {
		if !scr.Contains("KEEP-12") {
			return fmt.Errorf("waiting for fixed screen paint:\n%s", scr.String())
		}
		return nil
	})

	local := runner.localSession("preserved-live-visibility")
	if local == nil {
		t.Fatal("expected local session")
	}
	if _, err := local.Resize(40, 6); err != nil {
		t.Fatalf("local.Resize shrink: %v", err)
	}
	runner.forceRedraw(slave)

	screen.Eventually(2*time.Second, 50*time.Millisecond, func(scr testScreen) error {
		for row := 6; row < 12; row++ {
			if strings.Contains(scr.Row(row), "KEEP-") {
				return fmt.Errorf("expected preserved lower rows hidden after shrink; row=%d line=%q\nscreen:\n%s", row+1, scr.Row(row), scr.String())
			}
		}
		return nil
	})

	if _, err := local.Resize(40, 12); err != nil {
		t.Fatalf("local.Resize expand: %v", err)
	}
	runner.forceRedraw(slave)

	screen.Eventually(2*time.Second, 50*time.Millisecond, func(scr testScreen) error {
		if !scr.Contains("KEEP-12") {
			return fmt.Errorf("expected preserved lower rows restored after expand:\n%s", scr.String())
		}
		return nil
	})
}

func preservedResizeShell(t *testing.T) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "preserved-resize-shell.sh")
	const script = `#!/usr/bin/env bash
stty -echo -icanon min 1 time 0
cleanup() {
  stty sane 2>/dev/null || true
}
trap cleanup EXIT INT TERM

printf '\033[H\033[2J'
for i in $(seq 1 12); do
  printf '\033[%d;1HKEEP-%02d' "$i" "$i"
done
printf '\033[1;1H'

while :; do
  sleep 1
done
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write preserved resize shell: %v", err)
	}
	return scriptPath
}

type testScreen struct {
	Rows  int
	Lines []string
}

func (s testScreen) String() string {
	return strings.Join(s.Lines, "\n")
}

func (s testScreen) Row(i int) string {
	if i < 0 || i >= len(s.Lines) {
		return ""
	}
	return s.Lines[i]
}

func (s testScreen) Contains(substr string) bool {
	return strings.Contains(s.String(), substr)
}

type testPTYView struct {
	t      *testing.T
	master *os.File
	slave  *os.File
	emu    terminal.Emulator
	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	rawBuf bytes.Buffer
}

func newTestPTYView(t *testing.T, master, slave *os.File, cols, rows int) *testPTYView {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	v := &testPTYView{
		t:      t,
		master: master,
		slave:  slave,
		emu:    emu.New(cols, rows),
		ctx:    ctx,
		cancel: cancel,
	}
	go v.readLoop()
	return v
}

func (v *testPTYView) readLoop() {
	buf := make([]byte, 4096)
	pfd := []unix.PollFd{{Fd: int32(v.master.Fd()), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
	for {
		select {
		case <-v.ctx.Done():
			return
		default:
		}
		ready, err := unix.Poll(pfd, 1)
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			return
		}
		if ready == 0 {
			continue
		}
		n, err := v.master.Read(buf)
		if n > 0 {
			v.mu.Lock()
			_, _ = v.rawBuf.Write(buf[:n])
			_ = v.emu.Write(buf[:n])
			v.mu.Unlock()
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			return
		}
	}
}

func (v *testPTYView) Close() {
	v.cancel()
}

func (v *testPTYView) Send(data string) {
	_, _ = v.master.Write([]byte(data))
}

func (v *testPTYView) screen() testScreen {
	v.mu.Lock()
	defer v.mu.Unlock()
	snap, err := v.emu.Snapshot()
	if err != nil {
		v.t.Fatalf("snapshot: %v", err)
	}
	lines := make([]string, snap.Rows)
	for y := 0; y < snap.Rows; y++ {
		var line strings.Builder
		for x := 0; x < snap.Cols; x++ {
			idx := y*snap.Cols + x
			cell := snap.Cells[idx]
			if cell.Grapheme != "" {
				line.WriteString(cell.Grapheme)
				continue
			}
			r := cell.Rune
			if r == 0 {
				r = ' '
			}
			line.WriteRune(r)
		}
		lines[y] = line.String()
	}
	return testScreen{Rows: snap.Rows, Lines: lines}
}

func (v *testPTYView) Eventually(timeout, step time.Duration, check func(testScreen) error) {
	v.t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		scr := v.screen()
		err := check(scr)
		if err == nil {
			return
		}
		lastErr = err
		time.Sleep(step)
	}
	if lastErr != nil {
		v.t.Fatal(lastErr)
	}
	v.t.Fatalf("timeout after %v", timeout)
}
