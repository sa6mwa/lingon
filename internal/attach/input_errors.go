package attach

import (
	"errors"
	"io"
	"os"
	"syscall"
)

func isBenignStdinReadErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) {
		return true
	}
	return errors.Is(err, syscall.EIO) || errors.Is(err, syscall.EBADF)
}
