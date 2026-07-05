package relay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"pkt.systems/lingon/internal/logging"
	"pkt.systems/pslog"
)

const userReloadInterval = 1 * time.Second

// StartUserReloadLoop watches the users file and reloads users on change.
func StartUserReloadLoop(ctx context.Context, path string, store *UserStore, logger pslog.Logger) error {
	return startUserReloadLoop(ctx, path, store, logger, userReloadInterval, nil)
}

// UserReloadHook is called after the in-memory user store is replaced.
type UserReloadHook func(changedUsers []string)

// StartUserReloadLoopWithHook watches the users file and reports users whose
// existing credentials were changed or removed by a successful reload.
func StartUserReloadLoopWithHook(ctx context.Context, path string, store *UserStore, logger pslog.Logger, hook UserReloadHook) error {
	return startUserReloadLoop(ctx, path, store, logger, userReloadInterval, hook)
}

func startUserReloadLoop(ctx context.Context, path string, store *UserStore, logger pslog.Logger, interval time.Duration, hook UserReloadHook) error {
	if store == nil {
		return fmt.Errorf("user store is nil")
	}
	if path == "" {
		return fmt.Errorf("users file is required")
	}
	if logger == nil {
		logger = logging.Default()
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
					logger.Warn("relay.users.reload.failed", "err", err)
					continue
				}
				changedUsers := credentialChangedUsers(store.List(), loaded.List())
				store.ReplaceUsers(loaded.Users)
				if hook != nil && len(changedUsers) > 0 {
					hook(changedUsers)
				}
				lastHash = hash
			}
		}
	}()
	return nil
}

func credentialChangedUsers(previous, next []User) []string {
	nextByUsername := make(map[string]User, len(next))
	for _, user := range next {
		nextByUsername[user.Username] = user
	}
	changed := make([]string, 0)
	for _, old := range previous {
		updated, ok := nextByUsername[old.Username]
		if !ok ||
			updated.PasswordHash != old.PasswordHash ||
			updated.TOTPSecret != old.TOTPSecret {
			changed = append(changed, old.Username)
		}
	}
	return changed
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
