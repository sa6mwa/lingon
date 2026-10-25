package main

import (
	"bufio"
	"fmt"
	"sync"
)

// hostEchoOutput keeps a complete fixture row together with its flush.
type hostEchoOutput struct {
	mu     sync.Mutex
	writer *bufio.Writer
}

func (o *hostEchoOutput) printf(format string, args ...any) {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, _ = fmt.Fprintf(o.writer, format, args...)
	_ = o.writer.Flush()
}
