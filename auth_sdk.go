package lingon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"pkt.systems/lingon/internal/authstore"
	"pkt.systems/lingon/internal/relayclient"
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
	TLSDir   string
	Insecure bool
}

// RefreshOptions contains the inputs for refresh.
type RefreshOptions struct {
	Endpoint     string
	RefreshToken string
	TLSDir       string
	Insecure     bool
}

// LogoutOptions contains the inputs for logout.
type LogoutOptions struct {
	Endpoint string
	AuthFile string
	// RefreshToken overrides the stored refresh token when set.
	RefreshToken string
	// AccessToken overrides the stored access token when set.
	AccessToken string
	TLSDir      string
	Insecure    bool
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
	client, err := newHTTPClientWithTLSDir(opts.TLSDir, opts.Insecure)
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
	client, err := newHTTPClientWithTLSDir(opts.TLSDir, opts.Insecure)
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

// LoadAuthForEndpoint loads auth state for a specific endpoint from disk.
func LoadAuthForEndpoint(path, endpoint string) (AuthState, error) {
	return authstore.LoadForEndpoint(path, endpoint)
}

// SaveAuth saves auth state to disk.
func SaveAuth(path string, state AuthState) error {
	return authstore.Save(path, state)
}

// DeleteAuthForEndpoint deletes auth state for a specific endpoint from disk.
func DeleteAuthForEndpoint(path, endpoint string) error {
	return authstore.Delete(path, endpoint)
}

// EnsureAccessToken loads auth state and refreshes if needed.
func EnsureAccessToken(ctx context.Context, endpoint, authPath string) (AuthState, error) {
	return EnsureAccessTokenWithTLSDir(ctx, endpoint, authPath, "")
}

// EnsureAccessTokenWithTLSDir loads auth state and refreshes if needed using tlsDir.
func EnsureAccessTokenWithTLSDir(ctx context.Context, endpoint, authPath, tlsDir string) (AuthState, error) {
	return EnsureAccessTokenWithTLSDirInsecure(ctx, endpoint, authPath, tlsDir, false)
}

// EnsureAccessTokenWithTLSDirInsecure loads auth state and refreshes if needed using tlsDir and insecure mode.
func EnsureAccessTokenWithTLSDirInsecure(ctx context.Context, endpoint, authPath, tlsDir string, insecure bool) (AuthState, error) {
	state, err := relayclient.EnsureAccessTokenWithTLSDirInsecure(ctx, endpoint, authPath, tlsDir, insecure)
	if err != nil {
		return AuthState{}, err
	}
	return AuthState(state), nil
}

// Logout logs out the selected endpoint remotely (when possible) and removes local stored auth.
func Logout(ctx context.Context, opts LogoutOptions) error {
	if strings.TrimSpace(opts.Endpoint) == "" {
		return fmt.Errorf("endpoint is required")
	}
	normalized, err := normalizeHTTPURL(opts.Endpoint)
	if err != nil {
		return err
	}

	refreshToken := opts.RefreshToken
	accessToken := opts.AccessToken
	if opts.AuthFile != "" && (refreshToken == "" || accessToken == "") {
		state, loadErr := authstore.LoadForEndpoint(opts.AuthFile, normalized)
		if loadErr == nil {
			if refreshToken == "" {
				refreshToken = state.RefreshToken
			}
			if accessToken == "" {
				accessToken = state.AccessToken
			}
		} else if !errors.Is(loadErr, os.ErrNotExist) {
			return loadErr
		}
	}

	var remoteErr error
	if refreshToken != "" || accessToken != "" {
		remoteErr = relayclient.LogoutWithTLSDirInsecure(ctx, normalized, refreshToken, accessToken, opts.TLSDir, opts.Insecure)
	}

	var localErr error
	if opts.AuthFile != "" {
		localErr = authstore.WithLock(opts.AuthFile, func() error {
			return authstore.Delete(opts.AuthFile, normalized)
		})
	}

	if remoteErr != nil && localErr != nil {
		return fmt.Errorf("remote logout failed: %v; local logout failed: %w", remoteErr, localErr)
	}
	if localErr != nil {
		return localErr
	}
	if remoteErr != nil {
		return remoteErr
	}
	return nil
}
