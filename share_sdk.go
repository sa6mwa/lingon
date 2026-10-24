package lingon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	TLSDir      string
	Insecure    bool
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
	TLSDir      string
	Insecure    bool
}

// ShareRevokeResponse is the response for share revocation.
type ShareRevokeResponse struct {
	Status string `json:"status"`
}

// ShareTokenInfo represents a share token entry.
type ShareTokenInfo struct {
	Token     string     `json:"token"`
	SessionID string     `json:"session_id"`
	Scope     ShareScope `json:"scope"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// ShareListStatus filters share tokens returned by the relay.
type ShareListStatus string

// Share list status filters.
const (
	ShareListStatusValid   ShareListStatus = "valid"
	ShareListStatusRevoked ShareListStatus = "revoked"
	ShareListStatusExpired ShareListStatus = "expired"
)

// ShareListOptions contains inputs for listing share tokens.
type ShareListOptions struct {
	Endpoint    string
	AccessToken string
	Statuses    []ShareListStatus
	TLSDir      string
	Insecure    bool
}

// ShareRevokeAllOptions contains inputs for revoking all share tokens.
type ShareRevokeAllOptions struct {
	Endpoint    string
	AccessToken string
	TLSDir      string
	Insecure    bool
}

// ShareRevokeAllResponse is the response for revoking all share tokens.
type ShareRevokeAllResponse struct {
	Status  string `json:"status"`
	Revoked int    `json:"revoked"`
}

type shareCreateRequest struct {
	SessionID string `json:"session_id"`
	Scope     string `json:"scope"`
	TTL       string `json:"ttl,omitempty"`
}

type shareRevokeRequest struct {
	Token string `json:"token"`
}

// ShareList lists share tokens visible to the authenticated user.
func ShareList(ctx context.Context, opts ShareListOptions) ([]ShareTokenInfo, error) {
	if strings.TrimSpace(opts.Endpoint) == "" {
		return nil, fmt.Errorf("endpoint is required")
	}
	if opts.AccessToken == "" {
		return nil, fmt.Errorf("access token is required")
	}
	statuses := opts.Statuses
	if len(statuses) == 0 {
		statuses = []ShareListStatus{ShareListStatusValid}
	}

	httpURL, err := normalizeHTTPURL(opts.Endpoint)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpURL+"/share/list", nil)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	for _, status := range statuses {
		if status == "" {
			continue
		}
		query.Add("status", string(status))
	}
	req.URL.RawQuery = query.Encode()
	req.Header.Set("Authorization", "Bearer "+opts.AccessToken)

	client, err := newHTTPClientWithTLSDir(opts.TLSDir, opts.Insecure)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("share list failed: %s", resp.Status)
	}
	var out []ShareTokenInfo
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
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
	if opts.TTL < 0 {
		return ShareCreateResponse{}, fmt.Errorf("ttl must be non-negative")
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

	client, err := newHTTPClientWithTLSDir(opts.TLSDir, opts.Insecure)
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

// ShareRevokeAll revokes all share tokens for the authenticated user.
func ShareRevokeAll(ctx context.Context, opts ShareRevokeAllOptions) (ShareRevokeAllResponse, error) {
	if strings.TrimSpace(opts.Endpoint) == "" {
		return ShareRevokeAllResponse{}, fmt.Errorf("endpoint is required")
	}
	if opts.AccessToken == "" {
		return ShareRevokeAllResponse{}, fmt.Errorf("access token is required")
	}

	httpURL, err := normalizeHTTPURL(opts.Endpoint)
	if err != nil {
		return ShareRevokeAllResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, httpURL+"/share/revoke-all", bytes.NewReader([]byte("{}")))
	if err != nil {
		return ShareRevokeAllResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+opts.AccessToken)

	client, err := newHTTPClientWithTLSDir(opts.TLSDir, opts.Insecure)
	if err != nil {
		return ShareRevokeAllResponse{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return ShareRevokeAllResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ShareRevokeAllResponse{}, fmt.Errorf("share revoke-all failed: %s", resp.Status)
	}
	var out ShareRevokeAllResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ShareRevokeAllResponse{}, err
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

	client, err := newHTTPClientWithTLSDir(opts.TLSDir, opts.Insecure)
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
