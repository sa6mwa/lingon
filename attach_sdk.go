package lingon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"pkt.systems/lingon/internal/attach"
	"pkt.systems/lingon/internal/relay"
	"pkt.systems/pslog"
)

// AttachOptions configures an attach client session.
type AttachOptions struct {
	Endpoint       string
	SessionID      string
	AccessToken    string
	ShareToken     string
	RequestControl bool
	Logger         pslog.Logger
}

// Attach connects to a relay session and renders output locally.
func Attach(ctx context.Context, opts AttachOptions) error {
	client := &attach.Client{
		Endpoint:       opts.Endpoint,
		SessionID:      opts.SessionID,
		AccessToken:    opts.AccessToken,
		ShareToken:     opts.ShareToken,
		RequestControl: opts.RequestControl,
		Logger:         opts.Logger,
	}
	return client.Run(ctx)
}

// Session represents a relay session summary.
type Session = relay.Session

// ListSessions returns the sessions visible to an authenticated user.
func ListSessions(ctx context.Context, endpoint, accessToken string) ([]Session, error) {
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
	client, err := newHTTPClient()
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
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" {
		return "", fmt.Errorf("endpoint must include scheme")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https", "http":
		return strings.TrimRight(endpoint, "/"), nil
	case "wss":
		parsed.Scheme = "https"
	case "ws":
		parsed.Scheme = "http"
	default:
		return "", fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}
