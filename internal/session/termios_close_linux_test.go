//go:build linux

package session

import (
	"sync"
	"testing"

	"github.com/creack/pty"
)

// Every ioctl uses a fresh test-owned PTY pair; no child or inherited terminal
// is used. Closed-file errors are permitted only after teardown begins.
func TestTermiosConcurrentCloseUsesOwnedDescriptor(t *testing.T) {
	for i := 0; i < 100; i++ {
		master, slave, err := pty.Open()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = master.Close()
			_ = slave.Close()
		})
		if _, err := getVEOF(slave); err != nil {
			t.Fatal(err)
		}
		if err := setVEOF(slave, 4); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var workers sync.WaitGroup
		workers.Add(2)
		go func() {
			defer workers.Done()
			<-start
			for j := 0; j < 20; j++ {
				_, _ = getVEOF(slave)
				_ = setVEOF(slave, 4)
				_ = filterRemoteInput(slave, []byte{4, 'a'})
				if restore, err := disableTTYEcho(slave); err == nil && restore != nil {
					restore()
				}
			}
		}()
		go func() {
			defer workers.Done()
			<-start
			_ = slave.Close()
		}()
		close(start)
		workers.Wait()
		_ = master.Close()
	}
}

func BenchmarkOwnedTermiosRead(b *testing.B) {
	master, slave, err := pty.Open()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		_ = slave.Close()
		_ = master.Close()
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := getVEOF(slave); err != nil {
			b.Fatal(err)
		}
	}
}
