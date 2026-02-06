//go:build linux

package session

import (
	"context"
	"os"
)

func readInput(ctx context.Context, file *os.File, buf []byte) (int, error) {
	return readPTY(ctx, file, buf)
}
