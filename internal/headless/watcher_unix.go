//go:build !windows

package headless

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const watcherSocketPrefix = "watch-"

// StartStateWatcher starts a local unix-datagram watcher and returns
// a channel that receives signals when meaningful headless session state changes.
func StartStateWatcher(ctx context.Context, configDir string) (<-chan struct{}, func() error, error) {
	baseDir := BaseDir(configDir)
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return nil, nil, err
	}
	watcherID, err := randomWatcherID()
	if err != nil {
		return nil, nil, err
	}
	socketPath := filepath.Join(baseDir, watcherSocketPrefix+watcherID+".sock")
	_ = os.Remove(socketPath)

	addr, err := net.ResolveUnixAddr("unixgram", socketPath)
	if err != nil {
		return nil, nil, err
	}
	conn, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		return nil, nil, err
	}
	_ = os.Chmod(socketPath, 0o600)

	store := NewStore(configDir)
	now := time.Now().UTC()
	if err := store.WithLock(func(state *State) error {
		state.Watchers[watcherID] = WatcherRecord{
			ID:         watcherID,
			PID:        os.Getpid(),
			SocketPath: socketPath,
			StartedAt:  now,
			LastSeenAt: now,
		}
		return nil
	}); err != nil {
		_ = conn.Close()
		_ = os.Remove(socketPath)
		return nil, nil, err
	}

	events := make(chan struct{}, 1)
	done := make(chan struct{})
	var stopOnce sync.Once
	stopFn := func() error {
		var stopErr error
		stopOnce.Do(func() {
			close(done)
			_ = conn.Close()
			if err := store.WithLock(func(state *State) error {
				delete(state.Watchers, watcherID)
				return nil
			}); err != nil {
				stopErr = err
			}
			_ = os.Remove(socketPath)
		})
		return stopErr
	}

	go func() {
		defer close(events)
		buf := make([]byte, 16)
		for {
			if _, _, err := conn.ReadFromUnix(buf); err != nil {
				select {
				case <-done:
					return
				default:
					return
				}
			}
			select {
			case events <- struct{}{}:
			default:
			}
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = stopFn()
		case <-done:
		}
	}()
	return events, stopFn, nil
}

func notifyWatcherSocket(path string) error {
	target := strings.TrimSpace(path)
	if target == "" {
		return nil
	}
	addr, err := net.ResolveUnixAddr("unixgram", target)
	if err != nil {
		return err
	}
	conn, err := net.DialUnix("unixgram", nil, addr)
	if err != nil {
		return err
	}
	defer func() {
		_ = conn.Close()
	}()
	_, err = conn.Write([]byte{1})
	return err
}

func randomWatcherID() (string, error) {
	var buf [6]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate watcher id: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}
