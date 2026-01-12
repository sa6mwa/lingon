package relay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"pkt.systems/pslog"
)

const userReloadInterval = 1 * time.Second

// StartUserReloadLoop watches the users file and reloads users on change.
func StartUserReloadLoop(ctx context.Context, path string, store *UserStore, logger pslog.Logger) error {
	return startUserReloadLoop(ctx, path, store, logger, userReloadInterval)
}

func startUserReloadLoop(ctx context.Context, path string, store *UserStore, logger pslog.Logger, interval time.Duration) error {
	if store == nil {
		return fmt.Errorf("user store is nil")
	}
	if path == "" {
		return fmt.Errorf("users file is required")
	}
	if logger == nil {
		logger = pslog.LoggerFromEnv()
	}
	if ctx == nil {
		ctx = context.Background()
	}

	path = filepath.Clean(path)
	lastHash := ""
	if data, err := os.ReadFile(path); err == nil {
		lastHash = hashBytes(data)
	}

	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				data, err := os.ReadFile(path)
				if err != nil {
					continue
				}
				hash := hashBytes(data)
				if hash == lastHash {
					continue
				}
				loaded, err := LoadUserStoreFromBytes(data)
				if err != nil {
					logger.Warn("failed to parse store for reload", "err", err)
					continue
				}
				store.ReplaceUsers(loaded.Users)
				lastHash = hash
			}
		}
	}()
	return nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
