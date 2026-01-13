package session

import (
	"testing"

	"github.com/creack/pty"
)

func TestUseRendererOnlyOnSizeMismatch(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	_ = pty.Setsize(master, &pty.Winsize{Cols: 80, Rows: 24})

	r := &Runner{
		opts: Options{
			Cols: 80,
			Rows: 24,
		},
	}
	r.setHolder("client")
	if r.useRenderer(slave) {
		t.Fatalf("useRenderer should be false when sizes match")
	}

	_ = pty.Setsize(master, &pty.Winsize{Cols: 100, Rows: 30})
	if !r.useRenderer(slave) {
		t.Fatalf("useRenderer should be true when sizes differ")
	}
}
