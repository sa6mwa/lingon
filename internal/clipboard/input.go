package clipboard

import (
	"context"
	"errors"
)

// PasteError distinguishes clipboard failures from raw terminal transport errors.
type PasteError struct{ Err error }

func (e *PasteError) Error() string { return e.Err.Error() }
func (e *PasteError) Unwrap() error { return e.Err }

// IsPasteError reports a failed clipboard operation rather than terminal I/O.
func IsPasteError(err error) bool { var paste *PasteError; return errors.As(err, &paste) }

// Input handles terminal Ctrl+V without interpreting keys inside bracketed
// paste. Use it at interactive input boundaries, never on a raw-input API.
type Input struct {
	Reader    Reader
	bracketed bool
	tail      [6]byte
}

// Forward keeps ordinary bytes in order around an image operation. A missing
// image/display forwards Ctrl+V normally. After an image failure, remaining input
// is still parsed; the first paste error is returned after processing the batch.
func (i *Input) Forward(ctx context.Context, data []byte, raw func([]byte) error, paste func(Image) error) error {
	start := 0
	var pasteErr error
	for pos, b := range data {
		copy(i.tail[:], i.tail[1:])
		i.tail[5] = b
		if string(i.tail[:]) == "\x1b[200~" {
			i.bracketed = true
		}
		if string(i.tail[:]) == "\x1b[201~" {
			i.bracketed = false
		}
		if b != 0x16 || i.bracketed || i.Reader == nil {
			continue
		}
		if pos > start {
			if err := raw(data[start:pos]); err != nil {
				return err
			}
		}
		start = pos
		img, err := i.Reader.Read(ctx)
		if err != nil {
			if pasteErr == nil {
				pasteErr = &PasteError{Err: err}
			}
			start = pos + 1
			continue
		}
		if len(img.Data) == 0 {
			continue
		}
		if err := paste(img); err != nil {
			if pasteErr == nil {
				pasteErr = &PasteError{Err: err}
			}
			start = pos + 1
			continue
		}
		start = pos + 1
	}
	if start < len(data) {
		if err := raw(data[start:]); err != nil {
			return err
		}
	}
	return pasteErr
}
