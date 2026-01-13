//go:build !linux

package session

import (
	"errors"
	"os"
)

func getVEOF(_ *os.File) (uint8, error) {
	return 0, errors.New("veof unsupported")
}

func setVEOF(_ *os.File, _ uint8) error {
	return errors.New("veof unsupported")
}
