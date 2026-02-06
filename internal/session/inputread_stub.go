//go:build !linux

package session

import (
	"context"
	"os"
)

func readInput(_ context.Context, file *os.File, buf []byte) (int, error) {
	if file == nil {
		return 0, os.ErrInvalid
	}
	return file.Read(buf)
}
