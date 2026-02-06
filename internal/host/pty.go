package host

import (
	"os"
	"os/exec"

	internalpty "pkt.systems/lingon/internal/pty"
)

func startPTY(cmd *exec.Cmd) (*os.File, error) {
	return internalpty.Start(cmd)
}

func resizePTY(ptyFile *os.File, cols, rows int) error {
	return internalpty.Resize(ptyFile, cols, rows)
}
