package session

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type lockedBytes struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBytes) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBytes) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestFastOutputDoesNotDropBytes(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "emit.sh")
	script := "#!/bin/sh\n" +
		"for i in $(seq 1 2000); do printf \"LINE%04d %200s\\n\" \"$i\" X; done\n" +
		"printf \"__END__\\n\"\n" +
		"sleep 0.2\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = outR.Close()
		_ = outW.Close()
	})
	if err := syscall.SetNonblock(int(outW.Fd()), true); err != nil {
		t.Fatalf("setnonblock: %v", err)
	}

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = inR.Close()
		_ = inW.Close()
	})

	ptyOut := &lockedBytes{}
	hostOut := &lockedBytes{}

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 1024)
		for {
			n, err := outR.Read(buf)
			if n > 0 {
				_, _ = hostOut.Write(buf[:n])
			}
			if err != nil {
				if err != io.EOF {
					return
				}
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runner := New(Options{
		Cols:       80,
		Rows:       24,
		Shell:      scriptPath,
		Publish:    false,
		Stdin:      inR,
		Stdout:     outW,
		DisableRaw: true,
		OnPTYRead: func(data []byte) {
			_, _ = ptyOut.Write(data)
		},
	})

	runErr := make(chan error, 1)
	go func() {
		runErr <- runner.Run(ctx)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(hostOut.String(), "__END__") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !strings.Contains(ptyOut.String(), "__END__") {
		t.Fatalf("pty output missing __END__")
	}
	if !strings.Contains(hostOut.String(), "__END__") {
		t.Fatalf("host output missing __END__")
	}

	cancel()
	_ = outW.Close()
	_ = outR.Close()

	select {
	case <-readDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("reader did not exit")
	}
	select {
	case <-runErr:
	case <-time.After(5 * time.Second):
		t.Fatalf("runner did not exit")
	}
}
