package lingon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/authstore"
	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/relay"
	"pkt.systems/lingon/internal/trace"
	"pkt.systems/pslog"
)

// AttachOptions configures an attach client session.
type AttachOptions struct {
	Endpoint          string
	SessionID         string
	UnixSocket        string
	HeadlessConfigDir string
	AccessToken       string
	ShareToken        string
	RequestControl    bool
	HostnameOnly      bool
	Theme             string
	// AuthFile is the path to the auth state file used for refresh.
	AuthFile string
	TLSDir   string
	Insecure bool
	Logger   pslog.Logger
	Trace    *trace.Writer
}

// Attach connects to a relay session and renders output locally.
func Attach(ctx context.Context, opts AttachOptions) error {
	if opts.HeadlessConfigDir != "" {
		events, stopWatcher, err := headless.StartStateWatcher(ctx, opts.HeadlessConfigDir)
		if err != nil {
			return err
		}
		defer func() {
			_ = stopWatcher()
		}()
		client := &attach.MultiClient{
			Endpoint:           opts.Endpoint,
			SessionID:          opts.SessionID,
			RequestControl:     opts.RequestControl,
			AllowOfflineToggle: true,
			HostnameOnly:       opts.HostnameOnly,
			Theme:              opts.Theme,
			Logger:             opts.Logger,
			Trace:              opts.Trace,
			SessionSource:      headlessSessionSource(opts.HeadlessConfigDir),
			SocketResolver:     headlessSocketResolver(opts.HeadlessConfigDir),
			SessionEvents:      events,
		}
		return client.Run(ctx)
	}
	if opts.UnixSocket != "" {
		client := &attach.Client{
			Endpoint:           opts.Endpoint,
			SessionID:          opts.SessionID,
			UnixSocket:         opts.UnixSocket,
			RequestControl:     opts.RequestControl,
			AllowOfflineToggle: true,
			HostnameOnly:       opts.HostnameOnly,
			Theme:              opts.Theme,
			Logger:             opts.Logger,
			Trace:              opts.Trace,
		}
		return client.Run(ctx)
	}
	if opts.ShareToken != "" {
		client := &attach.Client{
			Endpoint:       opts.Endpoint,
			SessionID:      opts.SessionID,
			AccessToken:    opts.AccessToken,
			ShareToken:     opts.ShareToken,
			RequestControl: opts.RequestControl,
			HostnameOnly:   opts.HostnameOnly,
			Theme:          opts.Theme,
			TLSDir:         opts.TLSDir,
			Insecure:       opts.Insecure,
			Logger:         opts.Logger,
			Trace:          opts.Trace,
		}
		return client.Run(ctx)
	}
	client := &attach.MultiClient{
		Endpoint:       opts.Endpoint,
		SessionID:      opts.SessionID,
		AccessToken:    opts.AccessToken,
		RequestControl: opts.RequestControl,
		HostnameOnly:   opts.HostnameOnly,
		Theme:          opts.Theme,
		AuthFile:       opts.AuthFile,
		TLSDir:         opts.TLSDir,
		Insecure:       opts.Insecure,
		Logger:         opts.Logger,
		Trace:          opts.Trace,
	}
	return client.Run(ctx)
}

// Session represents a relay session summary.
type Session = relay.Session

// ListSessions returns the sessions visible to an authenticated user.
func ListSessions(ctx context.Context, endpoint, accessToken string) ([]Session, error) {
	return ListSessionsWithTLSDir(ctx, endpoint, accessToken, "")
}

// ListSessionsWithTLSDir returns the sessions visible to an authenticated user using tlsDir.
func ListSessionsWithTLSDir(ctx context.Context, endpoint, accessToken, tlsDir string) ([]Session, error) {
	return ListSessionsWithTLSDirInsecure(ctx, endpoint, accessToken, tlsDir, false)
}

// ListSessionsWithTLSDirInsecure returns the sessions visible to an authenticated user using tlsDir and insecure mode.
func ListSessionsWithTLSDirInsecure(ctx context.Context, endpoint, accessToken, tlsDir string, insecure bool) ([]Session, error) {
	httpURL, err := normalizeHTTPURL(endpoint)
	if err != nil {
		return nil, err
	}
	url := httpURL + "/sessions"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	client, err := newHTTPClientWithTLSDir(tlsDir, insecure)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list sessions failed: %s", resp.Status)
	}
	var out []Session
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func normalizeHTTPURL(endpoint string) (string, error) {
	return authstore.NormalizeEndpoint(endpoint)
}
