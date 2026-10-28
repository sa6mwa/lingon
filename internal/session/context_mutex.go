package session

import (
	"context"
	"sync"
)

// contextMutex serializes PTY writes and resize/termios transactions while
// allowing a revoked transfer to leave the wait without acquiring the writer.
// Its zero value is ready for use.
type contextMutex struct {
	once sync.Once
	gate chan struct{}
}

func (m *contextMutex) Lock(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.once.Do(func() { m.gate = make(chan struct{}, 1) })
	select {
	case m.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			m.Unlock()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *contextMutex) Unlock() { <-m.gate }
