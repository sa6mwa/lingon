package terminal

import "os"

// WithFileDescriptor runs control while file's descriptor is protected against
// concurrent closure. The descriptor must not be retained after control returns.
func WithFileDescriptor(file *os.File, control func(uintptr)) error {
	if file == nil {
		return os.ErrInvalid
	}
	raw, err := file.SyscallConn()
	if err != nil {
		return err
	}
	return raw.Control(control)
}
