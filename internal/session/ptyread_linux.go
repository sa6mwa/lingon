//go:build linux

package session

import (
	"context"
	"os"

	"pkt.systems/lingon/internal/pty"
)

func readPTY(ctx context.Context, file *os.File, buf []byte) (int, error) {
	return pty.Read(ctx, file, buf)
}
