package ptytest

import "testing"

func TestImmediateCancelKeepsOwnedPTYReaderIsolated(t *testing.T) {
	for i := 0; i < 200; i++ {
		master, slave := OpenPTY(t, 40, 10)
		s := NewPTYSession(t, master, slave, 40, 10)
		s.cleanup = func() {
			_ = master.Close()
			_ = slave.Close()
		}
		s.Cancel()
		select {
		case <-s.readerDone:
		default:
			t.Fatal("Cancel returned before the owned screen reader finished")
		}
	}
}
