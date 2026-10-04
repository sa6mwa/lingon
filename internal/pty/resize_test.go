package pty

import (
	"sync/atomic"
	"testing"

	creackpty "github.com/creack/pty"
)

func TestResizeConcurrentCloseUsesOwnedDescriptor(t *testing.T) {
	for iteration := 0; iteration < 50; iteration++ {
		// Only this fresh test-owned pair can be resized or closed. It has no
		// child or controlling terminal and never uses inherited stdio.
		master, slave, err := creackpty.Open()
		if err != nil {
			t.Fatal(err)
		}
		first, done := make(chan struct{}), make(chan struct{})
		var closing atomic.Bool
		go func() {
			defer close(done)
			for resize := 0; resize < 200; resize++ {
				err := Resize(master, 80+resize%2, 24)
				// Once Close begins, the transport may report any closed-file
				// error. Before that point, the valid owned PTY must resize.
				if err != nil && !closing.Load() {
					t.Errorf("resize before close: %v", err)
				}
				if resize == 0 {
					close(first)
				}
			}
		}()
		<-first
		closing.Store(true)
		_ = master.Close()
		<-done
		_ = slave.Close()
	}
}
