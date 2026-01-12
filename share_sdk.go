package lingon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ShareScope defines the share token scope.
type ShareScope string

const (
	// ShareScopeView grants view-only access.
	ShareScopeView ShareScope = "view"
	// ShareScopeControl grants control access.
	ShareScopeControl ShareScope = "control"
)

// ShareCreateOptions contains inputs for creating a share token.
type ShareCreateOptions struct {
	Endpoint    string
	AccessToken string
	SessionID   string
	Scope       ShareScope
	TTL         time.Duration
}

// ShareCreateResponse is the response for share creation.
type ShareCreateResponse struct {
	Token string `json:"token"`
}

// ShareRevokeOptions contains inputs for revoking a share token.
type ShareRevokeOptions struct {
	Endpoint    string
	AccessToken string
	Token       string
}

// ShareRevokeResponse is the response for share revocation.
type ShareRevokeResponse struct {
	Status string `json:"status"`
}

type shareCreateRequest struct {
	SessionID string `json:"session_id"`
	Scope     string `json:"scope"`
	TTL       string `json:"ttl,omitempty"`
}

type shareRevokeRequest struct {
	Token string `json:"token"`
}

// ShareCreate requests a new share token from the relay.
func ShareCreate(ctx context.Context, opts ShareCreateOptions) (ShareCreateResponse, error) {
	if strings.TrimSpace(opts.Endpoint) == "" {
		return ShareCreateResponse{}, fmt.Errorf("endpoint is required")
	}
	if opts.AccessToken == "" {
		return ShareCreateResponse{}, fmt.Errorf("access token is required")
	}
	if opts.SessionID == "" {
		return ShareCreateResponse{}, fmt.Errorf("session id is required")
	}
	scope := opts.Scope
	if scope == "" {
		scope = ShareScopeView
	}
	if scope != ShareScopeView && scope != ShareScopeControl {
		return ShareCreateResponse{}, fmt.Errorf("invalid share scope")
	}

	httpURL, err := normalizeHTTPURL(opts.Endpoint)
	if err != nil {
		return ShareCreateResponse{}, err
	}

	reqBody := shareCreateRequest{
		SessionID: opts.SessionID,
		Scope:     string(scope),
	}
	if opts.TTL > 0 {
		reqBody.TTL = opts.TTL.String()
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return ShareCreateResponse{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, httpURL+"/share/create", bytes.NewReader(payload))
	if err != nil {
		return ShareCreateResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+opts.AccessToken)

	client, err := newHTTPClient()
	if err != nil {
		return ShareCreateResponse{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return ShareCreateResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ShareCreateResponse{}, fmt.Errorf("share create failed: %s", resp.Status)
	}
	var out ShareCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ShareCreateResponse{}, err
	}
	if out.Token == "" {
		return ShareCreateResponse{}, fmt.Errorf("share token missing from response")
	}
	return out, nil
}

// ShareRevoke revokes a share token.
func ShareRevoke(ctx context.Context, opts ShareRevokeOptions) (ShareRevokeResponse, error) {
	if strings.TrimSpace(opts.Endpoint) == "" {
		return ShareRevokeResponse{}, fmt.Errorf("endpoint is required")
	}
	if opts.AccessToken == "" {
		return ShareRevokeResponse{}, fmt.Errorf("access token is required")
	}
	if opts.Token == "" {
		return ShareRevokeResponse{}, fmt.Errorf("token is required")
	}

	httpURL, err := normalizeHTTPURL(opts.Endpoint)
	if err != nil {
		return ShareRevokeResponse{}, err
	}
	payload, err := json.Marshal(shareRevokeRequest{Token: opts.Token})
	if err != nil {
		return ShareRevokeResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, httpURL+"/share/revoke", bytes.NewReader(payload))
	if err != nil {
		return ShareRevokeResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+opts.AccessToken)

	client, err := newHTTPClient()
	if err != nil {
		return ShareRevokeResponse{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return ShareRevokeResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ShareRevokeResponse{}, fmt.Errorf("share revoke failed: %s", resp.Status)
	}
	var out ShareRevokeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ShareRevokeResponse{}, err
	}
	return out, nil
}
