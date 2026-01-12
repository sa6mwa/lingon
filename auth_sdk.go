package lingon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"pkt.systems/lingon/internal/authstore"
)

// DefaultAccessRefreshSkew controls how soon we refresh before access expiry.
const DefaultAccessRefreshSkew = time.Minute

// AuthState holds persisted authentication tokens.
type AuthState = authstore.State

// LoginOptions contains the inputs for login.
type LoginOptions struct {
	Endpoint string
	Username string
	Password string
	TOTP     string
}

// RefreshOptions contains the inputs for refresh.
type RefreshOptions struct {
	Endpoint     string
	RefreshToken string
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TOTP     string `json:"totp"`
}

type loginResponse struct {
	AccessToken      string    `json:"access_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// Login authenticates to the relay and returns auth state.
func Login(ctx context.Context, opts LoginOptions) (AuthState, error) {
	if strings.TrimSpace(opts.Endpoint) == "" {
		return AuthState{}, fmt.Errorf("endpoint is required")
	}
	if opts.Username == "" || opts.Password == "" || opts.TOTP == "" {
		return AuthState{}, fmt.Errorf("username, password, and totp are required")
	}
	httpURL, err := normalizeHTTPURL(opts.Endpoint)
	if err != nil {
		return AuthState{}, err
	}
	payload, err := json.Marshal(loginRequest{
		Username: opts.Username,
		Password: opts.Password,
		TOTP:     opts.TOTP,
	})
	if err != nil {
		return AuthState{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, httpURL+"/auth/login", bytes.NewReader(payload))
	if err != nil {
		return AuthState{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	client, err := newHTTPClient()
	if err != nil {
		return AuthState{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return AuthState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return AuthState{}, fmt.Errorf("login failed: %s", resp.Status)
	}
	var out loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return AuthState{}, err
	}
	return AuthState{
		Endpoint:         httpURL,
		AccessToken:      out.AccessToken,
		AccessExpiresAt:  out.AccessExpiresAt,
		RefreshToken:     out.RefreshToken,
		RefreshExpiresAt: out.RefreshExpiresAt,
	}, nil
}

// Refresh uses the refresh token to obtain a new access token.
func Refresh(ctx context.Context, opts RefreshOptions) (AuthState, error) {
	if strings.TrimSpace(opts.Endpoint) == "" {
		return AuthState{}, fmt.Errorf("endpoint is required")
	}
	if opts.RefreshToken == "" {
		return AuthState{}, fmt.Errorf("refresh token is required")
	}
	httpURL, err := normalizeHTTPURL(opts.Endpoint)
	if err != nil {
		return AuthState{}, err
	}
	payload, err := json.Marshal(refreshRequest{RefreshToken: opts.RefreshToken})
	if err != nil {
		return AuthState{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, httpURL+"/auth/refresh", bytes.NewReader(payload))
	if err != nil {
		return AuthState{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	client, err := newHTTPClient()
	if err != nil {
		return AuthState{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return AuthState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return AuthState{}, fmt.Errorf("refresh failed: %s", resp.Status)
	}
	var out loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return AuthState{}, err
	}
	return AuthState{
		Endpoint:         httpURL,
		AccessToken:      out.AccessToken,
		AccessExpiresAt:  out.AccessExpiresAt,
		RefreshToken:     out.RefreshToken,
		RefreshExpiresAt: out.RefreshExpiresAt,
	}, nil
}

// LoadAuth loads auth state from disk.
func LoadAuth(path string) (AuthState, error) {
	return authstore.Load(path)
}

// SaveAuth saves auth state to disk.
func SaveAuth(path string, state AuthState) error {
	return authstore.Save(path, state)
}

// EnsureAccessToken loads auth state and refreshes if needed.
func EnsureAccessToken(ctx context.Context, endpoint, authPath string) (AuthState, error) {
	state, err := LoadAuth(authPath)
	if err != nil {
		return AuthState{}, err
	}
	normalized, err := normalizeHTTPURL(endpoint)
	if err != nil {
		return AuthState{}, err
	}
	if state.Endpoint != "" && state.Endpoint != normalized {
		return AuthState{}, fmt.Errorf("auth endpoint %s does not match %s", state.Endpoint, normalized)
	}
	state.Endpoint = normalized
	now := time.Now().UTC()
	if state.AccessValidAt(now.Add(DefaultAccessRefreshSkew)) {
		return state, nil
	}
	if !state.RefreshValidAt(now) {
		return AuthState{}, errors.New("refresh token expired")
	}
	refreshed, err := Refresh(ctx, RefreshOptions{
		Endpoint:     normalized,
		RefreshToken: state.RefreshToken,
	})
	if err != nil {
		return AuthState{}, err
	}
	refreshed.Endpoint = normalized
	if err := SaveAuth(authPath, refreshed); err != nil {
		return AuthState{}, err
	}
	return refreshed, nil
}
