package session

import (
	"os"
	"testing"

	"github.com/creack/pty"
)

func TestTermSizeAnyFallsBackToStdin(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	_ = pty.Setsize(master, &pty.Winsize{Cols: 90, Rows: 30})

	inFile := slave
	outFile, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	t.Cleanup(func() {
		_ = outFile.Close()
	})

	cols, rows := termSizeAny(outFile, inFile)
	if cols != 90 || rows != 30 {
		t.Fatalf("termSizeAny = %dx%d, want 90x30", cols, rows)
	}
}

func TestTermSizeAnyWithNoFilesDoesNotPanic(t *testing.T) {
	cols, rows := termSizeAny()
	if cols < 0 || rows < 0 {
		t.Fatalf("termSizeAny returned negative size")
	}
}
