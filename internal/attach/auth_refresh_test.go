package attach_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/authstore"
	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachRefreshesExpiredAccessToken(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "session_auth_refresh",
		SessionName: "session_auth_refresh",
		Shell:       "/bin/sh",
		Cols:        80,
		Rows:        24,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"session_auth_refresh"})

	authPath := filepath.Join(os.Getenv("HOME"), ".lingon", "auth.json")
	refresh := h.RefreshToken()
	state := authstore.State{
		Endpoint:         h.Endpoint(),
		AccessToken:      "expired-token",
		AccessExpiresAt:  ptytest.Now(h.Clock()).Add(-time.Minute),
		RefreshToken:     refresh.Token,
		RefreshExpiresAt: refresh.ExpiresAt,
	}
	if err := authstore.Save(authPath, state); err != nil {
		t.Fatalf("Save auth: %v", err)
	}

	var activeMu sync.Mutex
	activeID := ""
	var viewsMu sync.Mutex
	views := make(map[string]*attach.Client)
	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID:   "session_auth_refresh",
		Cols:        80,
		Rows:        24,
		AccessToken: state.AccessToken,
		AuthFile:    authPath,
		OnActive: func(id string) {
			activeMu.Lock()
			activeID = id
			activeMu.Unlock()
		},
		OnView: func(id string, client *attach.Client) {
			viewsMu.Lock()
			views[id] = client
			viewsMu.Unlock()
		},
	})
	t.Cleanup(attachSess.Cancel)

	_ = waitForActiveSessionReady(t, h.Clock(), &activeMu, &activeID, &viewsMu, views, "", 3*time.Second)
}

func TestAttachFailsOnExpiredRefreshToken(t *testing.T) {
	h := newHarness(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "session_auth_expired",
		SessionName: "session_auth_expired",
		Shell:       "/bin/sh",
		Cols:        80,
		Rows:        24,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"session_auth_expired"})

	authPath := filepath.Join(os.Getenv("HOME"), ".lingon", "auth.json")
	state := authstore.State{
		Endpoint:         h.Endpoint(),
		AccessToken:      "expired-token",
		AccessExpiresAt:  ptytest.Now(h.Clock()).Add(-time.Minute),
		RefreshToken:     "expired-refresh",
		RefreshExpiresAt: ptytest.Now(h.Clock()).Add(-time.Minute),
	}
	if err := authstore.Save(authPath, state); err != nil {
		t.Fatalf("Save auth: %v", err)
	}

	attachSess := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID:   "session_auth_expired",
		Cols:        80,
		Rows:        24,
		AccessToken: state.AccessToken,
		AuthFile:    authPath,
	})

	if ok, err := attachSess.WaitErr(2 * time.Second); !ok {
		attachSess.Cancel()
		t.Fatalf("expected attach to exit on auth failure")
	} else if !errors.Is(err, attach.ErrAuthExpired) {
		t.Fatalf("expected auth expired error, got %v", err)
	}
}
