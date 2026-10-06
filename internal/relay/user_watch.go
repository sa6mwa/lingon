package relay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/logging"
	"pkt.systems/pslog"
)

const userReloadInterval = 1 * time.Second

// UserReloadOptions configures serialized users-file reloads.
type UserReloadOptions struct {
	Path     string
	Store    *UserStore
	Logger   pslog.Logger
	Clock    clock.Clock
	OnReload UserReloadHook
}

// UserReloadHook reports existing users whose credentials changed or were removed.
type UserReloadHook func(changedUsers []string)

// UserReloader owns its background reload loop and joins it during cleanup.
type UserReloader interface{ Close() error }

type userReloader struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (r *userReloader) Close() error { r.cancel(); <-r.done; return nil }

// NewUserReloader reconciles the file and watches with an injectable clock.
func NewUserReloader(ctx context.Context, opts UserReloadOptions) (UserReloader, error) {
	if opts.Store == nil {
		return nil, fmt.Errorf("user store is nil")
	}
	if opts.Path == "" {
		return nil, fmt.Errorf("users file is required")
	}
	if opts.Logger == nil {
		opts.Logger = logging.Default()
	}
	if opts.Clock == nil {
		opts.Clock = clock.New()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	path := filepath.Clean(opts.Path)
	lastHash := ""
	reload := func() {
		// Hold the same transaction lock as live mutations while reading and
		// replacing the file. An old reload cannot overwrite a newer edit.
		opts.Store.mutationMu.Lock()
		defer opts.Store.mutationMu.Unlock()
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		hash := hashBytes(data)
		if hash == lastHash {
			return
		}
		loaded, err := LoadUserStoreFromBytes(data)
		if err != nil {
			opts.Logger.Warn("relay.users.reload.failed", "err", err)
			return
		}
		changed := credentialChangedUsers(opts.Store.List(), loaded.List())
		opts.Store.ReplaceUsers(loaded.Users)
		if opts.OnReload != nil && len(changed) > 0 {
			opts.OnReload(changed)
		}
		lastHash = hash
	}
	// Reconcile immediately as well as on ticks: file changes between the
	// initial load and watcher registration must invalidate old authority.
	reload()
	ticker := opts.Clock.NewTicker(userReloadInterval)
	r := &userReloader{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				reload()
			}
		}
	}()
	return r, nil
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
