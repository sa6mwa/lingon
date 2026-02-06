package relayclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"pkt.systems/lingon/internal/authstore"
)

type wallRequest struct {
	Message string `json:"message"`
}

// WallResponse captures relay wall dispatch status.
type WallResponse struct {
	Status   string `json:"status"`
	Sessions int    `json:"sessions"`
}

type wallInactivityRequest struct {
	SessionID string `json:"session_id"`
	Enabled   *bool  `json:"enabled,omitempty"`
}

// WallInactivityResponse captures relay inactivity wall toggle status.
type WallInactivityResponse struct {
	SessionID     string `json:"session_id"`
	Enabled       bool   `json:"enabled"`
	InactiveAfter string `json:"inactive_after,omitempty"`
}

// SendWall posts a wall message for the authenticated user.
func SendWall(ctx context.Context, endpoint, accessToken, message, tlsDir string, insecure bool) (WallResponse, error) {
	if strings.TrimSpace(endpoint) == "" {
		return WallResponse{}, fmt.Errorf("endpoint is required")
	}
	if strings.TrimSpace(accessToken) == "" {
		return WallResponse{}, fmt.Errorf("access token is required")
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return WallResponse{}, fmt.Errorf("message is required")
	}
	normalized, err := authstore.NormalizeEndpoint(endpoint)
	if err != nil {
		return WallResponse{}, err
	}
	payload, err := json.Marshal(wallRequest{Message: message})
	if err != nil {
		return WallResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, normalized+"/wall", bytes.NewReader(payload))
	if err != nil {
		return WallResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	tlsCfg, err := clientTLSConfig(tlsDir, insecure)
	if err != nil {
		return WallResponse{}, err
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsCfg}}
	resp, err := client.Do(req)
	if err != nil {
		return WallResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return WallResponse{}, fmt.Errorf("wall failed: %s", resp.Status)
	}
	var out WallResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return WallResponse{}, err
	}
	return out, nil
}

// SetWallInactivity sets relay-side inactivity wall notifications for one session.
func SetWallInactivity(ctx context.Context, endpoint, accessToken, sessionID string, enabled bool, tlsDir string, insecure bool) (WallInactivityResponse, error) {
	if strings.TrimSpace(endpoint) == "" {
		return WallInactivityResponse{}, fmt.Errorf("endpoint is required")
	}
	if strings.TrimSpace(accessToken) == "" {
		return WallInactivityResponse{}, fmt.Errorf("access token is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return WallInactivityResponse{}, fmt.Errorf("session id is required")
	}
	return postWallInactivity(ctx, endpoint, accessToken, sessionID, &enabled, tlsDir, insecure)
}

// ToggleWallInactivity flips relay-side inactivity wall notifications for one session.
func ToggleWallInactivity(ctx context.Context, endpoint, accessToken, sessionID string, tlsDir string, insecure bool) (WallInactivityResponse, error) {
	if strings.TrimSpace(endpoint) == "" {
		return WallInactivityResponse{}, fmt.Errorf("endpoint is required")
	}
	if strings.TrimSpace(accessToken) == "" {
		return WallInactivityResponse{}, fmt.Errorf("access token is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return WallInactivityResponse{}, fmt.Errorf("session id is required")
	}
	return postWallInactivity(ctx, endpoint, accessToken, sessionID, nil, tlsDir, insecure)
}

func postWallInactivity(ctx context.Context, endpoint, accessToken, sessionID string, enabled *bool, tlsDir string, insecure bool) (WallInactivityResponse, error) {
	normalized, err := authstore.NormalizeEndpoint(endpoint)
	if err != nil {
		return WallInactivityResponse{}, err
	}
	payload, err := json.Marshal(wallInactivityRequest{SessionID: sessionID, Enabled: enabled})
	if err != nil {
		return WallInactivityResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, normalized+"/wall/inactivity", bytes.NewReader(payload))
	if err != nil {
		return WallInactivityResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	tlsCfg, err := clientTLSConfig(tlsDir, insecure)
	if err != nil {
		return WallInactivityResponse{}, err
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsCfg}}
	resp, err := client.Do(req)
	if err != nil {
		return WallInactivityResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return WallInactivityResponse{}, fmt.Errorf("wall inactivity toggle failed: %s", resp.Status)
	}
	var out WallInactivityResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return WallInactivityResponse{}, err
	}
	return out, nil
}
