package relayclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"pkt.systems/lingon/internal/authstore"
	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/tlsmgr"
)

const accessRefreshSkew = time.Minute

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type refreshResponse struct {
	AccessToken      string    `json:"access_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

// EnsureAccessToken loads auth state and refreshes if needed.
func EnsureAccessToken(ctx context.Context, endpoint, authPath string) (authstore.State, error) {
	return EnsureAccessTokenWithTLSDir(ctx, endpoint, authPath, "")
}

// EnsureAccessTokenWithTLSDir loads auth state and refreshes if needed using tlsDir.
func EnsureAccessTokenWithTLSDir(ctx context.Context, endpoint, authPath, tlsDir string) (authstore.State, error) {
	return EnsureAccessTokenWithTLSDirInsecure(ctx, endpoint, authPath, tlsDir, false)
}

// EnsureAccessTokenWithTLSDirInsecure loads auth state and refreshes if needed using tlsDir and insecure mode.
func EnsureAccessTokenWithTLSDirInsecure(ctx context.Context, endpoint, authPath, tlsDir string, insecure bool) (authstore.State, error) {
	var out authstore.State
	err := authstore.WithLock(authPath, func() error {
		normalized, err := authstore.NormalizeEndpoint(endpoint)
		if err != nil {
			return err
		}
		state, err := authstore.LoadForEndpoint(authPath, normalized)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if state.AccessValidAt(now.Add(accessRefreshSkew)) {
			out = state
			return nil
		}
		if !state.RefreshValidAt(now) {
			return errors.New("refresh token expired")
		}
		refreshed, err := RefreshTokenWithTLSDirInsecure(ctx, normalized, state.RefreshToken, tlsDir, insecure)
		if err != nil {
			return err
		}
		refreshed.Endpoint = normalized
		if err := authstore.Save(authPath, refreshed); err != nil {
			return err
		}
		out = refreshed
		return nil
	})
	if err != nil {
		return authstore.State{}, err
	}
	return out, nil
}

// RefreshToken posts a refresh token and returns a new auth state.
func RefreshToken(ctx context.Context, httpURL, refreshToken string) (authstore.State, error) {
	return RefreshTokenWithTLSDir(ctx, httpURL, refreshToken, "")
}

// RefreshTokenWithTLSDir posts a refresh token and returns a new auth state using tlsDir.
func RefreshTokenWithTLSDir(ctx context.Context, httpURL, refreshToken, tlsDir string) (authstore.State, error) {
	return RefreshTokenWithTLSDirInsecure(ctx, httpURL, refreshToken, tlsDir, false)
}

// RefreshTokenWithTLSDirInsecure posts a refresh token and returns a new auth state using tlsDir and insecure mode.
func RefreshTokenWithTLSDirInsecure(ctx context.Context, httpURL, refreshToken, tlsDir string, insecure bool) (authstore.State, error) {
	payload, err := json.Marshal(refreshRequest{RefreshToken: refreshToken})
	if err != nil {
		return authstore.State{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, httpURL+"/auth/refresh", bytes.NewReader(payload))
	if err != nil {
		return authstore.State{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	tlsCfg, err := clientTLSConfig(tlsDir, insecure)
	if err != nil {
		return authstore.State{}, err
	}
	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
	}
	resp, err := client.Do(req)
	if err != nil {
		return authstore.State{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return authstore.State{}, fmt.Errorf("refresh failed: %s", resp.Status)
	}
	var out refreshResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return authstore.State{}, err
	}
	return authstore.State{
		AccessToken:      out.AccessToken,
		AccessExpiresAt:  out.AccessExpiresAt,
		RefreshToken:     out.RefreshToken,
		RefreshExpiresAt: out.RefreshExpiresAt,
	}, nil
}

// Logout posts to /auth/logout using optional refresh/access tokens.
func Logout(ctx context.Context, endpoint, refreshToken, accessToken string) error {
	return LogoutWithTLSDir(ctx, endpoint, refreshToken, accessToken, "")
}

// LogoutWithTLSDir posts to /auth/logout using optional refresh/access tokens and tlsDir.
func LogoutWithTLSDir(ctx context.Context, endpoint, refreshToken, accessToken, tlsDir string) error {
	return LogoutWithTLSDirInsecure(ctx, endpoint, refreshToken, accessToken, tlsDir, false)
}

// LogoutWithTLSDirInsecure posts to /auth/logout using optional refresh/access tokens and tlsDir/insecure.
func LogoutWithTLSDirInsecure(ctx context.Context, endpoint, refreshToken, accessToken, tlsDir string, insecure bool) error {
	normalized, err := authstore.NormalizeEndpoint(endpoint)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(refreshRequest{RefreshToken: refreshToken})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, normalized+"/auth/logout", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	tlsCfg, err := clientTLSConfig(tlsDir, insecure)
	if err != nil {
		return err
	}
	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("logout failed: %s", resp.Status)
	}
	return nil
}

func clientTLSConfig(tlsDir string, insecure bool) (*tls.Config, error) {
	dir := strings.TrimSpace(tlsDir)
	if dir == "" {
		dir = config.DefaultTLSDir()
	}
	pool, err := tlsmgr.LoadLocalCARoots(dir, nil)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		RootCAs:            pool,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: insecure,
	}, nil
}
