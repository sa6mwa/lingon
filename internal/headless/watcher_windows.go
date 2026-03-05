//go:build windows

package headless

import (
	"context"
	"fmt"
)

// StartStateWatcher is unsupported on windows until local socket parity is added.
func StartStateWatcher(context.Context, string) (<-chan struct{}, func() error, error) {
	return nil, nil, fmt.Errorf("headless state watcher is unsupported on windows")
}

func notifyWatcherSocket(string) error {
	return nil
}
