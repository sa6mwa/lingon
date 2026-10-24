package terminal

import (
	"os"

	"golang.org/x/term"
)

// WindowSize is a terminal viewport in character cells.
type WindowSize struct{ Cols, Rows int }

// IsTerminal tests the file while retaining its descriptor through the ioctl.
func IsTerminal(file *os.File) bool {
	valid := false
	_ = WithFileDescriptor(file, func(fd uintptr) { valid = term.IsTerminal(int(fd)) })
	return valid
}

// MakeRaw configures this file's terminal and returns the state for restoration.
// The descriptor is retained through the complete read/modify/write operation.
func MakeRaw(file *os.File) (*term.State, error) {
	var state *term.State
	var ioctlErr error
	if err := WithFileDescriptor(file, func(fd uintptr) {
		state, ioctlErr = term.MakeRaw(int(fd))
	}); err != nil {
		return nil, err
	}
	return state, ioctlErr
}

// GetSize reads the viewport while retaining its descriptor through the ioctl.
func GetSize(file *os.File) (WindowSize, error) {
	var size WindowSize
	var ioctlErr error
	if err := WithFileDescriptor(file, func(fd uintptr) {
		size.Cols, size.Rows, ioctlErr = term.GetSize(int(fd))
	}); err != nil {
		return size, err
	}
	return size, ioctlErr
}
