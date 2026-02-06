//go:build !linux

package session

import "os"

func filterRemoteInput(_ *os.File, data []byte) []byte {
	return data
}
