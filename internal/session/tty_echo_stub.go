//go:build !linux

package session

import "os"

func disableTTYEcho(_ *os.File) (func(), error) {
	return nil, nil
}
