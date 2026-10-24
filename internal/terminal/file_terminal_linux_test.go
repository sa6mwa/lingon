//go:build linux

package terminal

import (
	"os"
	"sync"
	"testing"

	"github.com/creack/pty"
	"golang.org/x/term"
)

func TestTerminalControlsUseOnlyOwnedPTYDescriptors(t *testing.T) {
	if IsTerminal(nil) {
		t.Fatal("nil file reported as terminal")
	}
	if _, err := MakeRaw(nil); err == nil {
		t.Fatal("nil raw mode accepted")
	}
	if _, err := GetSize(nil); err == nil {
		t.Fatal("nil viewport accepted")
	}
	for i := 0; i < 100; i++ {
		master, slave, err := pty.Open()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = master.Close(); _ = slave.Close() })
		if err := pty.Setsize(slave, &pty.Winsize{Cols: 40, Rows: 10}); err != nil {
			t.Fatal(err)
		}
		if !IsTerminal(slave) {
			t.Fatal("owned slave is not a terminal")
		}
		if size, err := GetSize(slave); err != nil || size != (WindowSize{Cols: 40, Rows: 10}) {
			t.Fatalf("owned viewport: %+v %v", size, err)
		}
		state, err := MakeRaw(slave)
		if err != nil || state == nil {
			t.Fatalf("owned raw state: %v", err)
		}
		if err := WithFileDescriptor(slave, func(fd uintptr) { _ = term.Restore(int(fd), state) }); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var workers sync.WaitGroup
		workers.Add(2)
		go func() {
			defer workers.Done()
			<-start
			for j := 0; j < 20; j++ {
				_ = IsTerminal(slave)
				_, _ = GetSize(slave)
				_, _ = MakeRaw(slave)
			}
		}()
		go func() { defer workers.Done(); <-start; _ = slave.Close() }()
		close(start)
		workers.Wait()
		if IsTerminal(slave) {
			t.Fatal("closed descriptor reported as terminal")
		}
		if _, err := GetSize(slave); err == nil {
			t.Fatal("closed descriptor returned viewport")
		}
		_ = master.Close()
	}
	// A regular pipe remains outside terminal-control behavior.
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
	if IsTerminal(reader) {
		t.Fatal("pipe reported as terminal")
	}
	if _, err := MakeRaw(reader); err == nil {
		t.Fatal("pipe accepted raw mode")
	}
}
