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

// UserSummary is the user info returned by list operations.
type UserSummary struct {
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

// UserCreateOptions configures user creation.
type UserCreateOptions struct {
	Endpoint    string
	AccessToken string
	Username    string
	Password    string
}

// UserCreateResponse contains the created user details.
type UserCreateResponse struct {
	Username   string    `json:"username"`
	Password   string    `json:"password"`
	TOTPSecret string    `json:"totp_secret"`
	TOTPURL    string    `json:"totp_url"`
	CreatedAt  time.Time `json:"created_at"`
}

// UserPasswordResponse contains a password result.
type UserPasswordResponse struct {
	Password string `json:"password"`
}

// UserTOTPResponse contains TOTP details.
type UserTOTPResponse struct {
	TOTPSecret string `json:"totp_secret"`
	TOTPURL    string `json:"totp_url"`
}

// UsersList lists users.
func UsersList(ctx context.Context, endpoint, accessToken string) ([]UserSummary, error) {
	httpURL, err := normalizeHTTPURL(endpoint)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpURL+"/users", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
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
		return nil, fmt.Errorf("list users failed: %s", resp.Status)
	}
	var out []UserSummary
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// UsersAdd creates a new user.
func UsersAdd(ctx context.Context, opts UserCreateOptions) (UserCreateResponse, error) {
	if strings.TrimSpace(opts.Endpoint) == "" {
		return UserCreateResponse{}, fmt.Errorf("endpoint is required")
	}
	if opts.AccessToken == "" {
		return UserCreateResponse{}, fmt.Errorf("access token is required")
	}
	if strings.TrimSpace(opts.Username) == "" {
		return UserCreateResponse{}, fmt.Errorf("username is required")
	}
	httpURL, err := normalizeHTTPURL(opts.Endpoint)
	if err != nil {
		return UserCreateResponse{}, err
	}
	payload, err := json.Marshal(map[string]string{
		"username": opts.Username,
		"password": opts.Password,
	})
	if err != nil {
		return UserCreateResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, httpURL+"/users", bytes.NewReader(payload))
	if err != nil {
		return UserCreateResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+opts.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	client, err := newHTTPClient()
	if err != nil {
		return UserCreateResponse{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return UserCreateResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return UserCreateResponse{}, fmt.Errorf("user add failed: %s", resp.Status)
	}
	var out UserCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return UserCreateResponse{}, err
	}
	return out, nil
}

// UsersDelete deletes a user by username.
func UsersDelete(ctx context.Context, endpoint, accessToken, username string) error {
	httpURL, err := normalizeHTTPURL(endpoint)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, httpURL+"/users/"+username, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	client, err := newHTTPClient()
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("user delete failed: %s", resp.Status)
	}
	return nil
}

// UsersRotateTOTP rotates a user's TOTP secret.
func UsersRotateTOTP(ctx context.Context, endpoint, accessToken, username string) (UserTOTPResponse, error) {
	httpURL, err := normalizeHTTPURL(endpoint)
	if err != nil {
		return UserTOTPResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, httpURL+"/users/"+username+"/rotate-totp", nil)
	if err != nil {
		return UserTOTPResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	client, err := newHTTPClient()
	if err != nil {
		return UserTOTPResponse{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return UserTOTPResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return UserTOTPResponse{}, fmt.Errorf("rotate totp failed: %s", resp.Status)
	}
	var out UserTOTPResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return UserTOTPResponse{}, err
	}
	return out, nil
}

// UsersChpasswd changes a user's password.
func UsersChpasswd(ctx context.Context, endpoint, accessToken, username, password string) (UserPasswordResponse, error) {
	httpURL, err := normalizeHTTPURL(endpoint)
	if err != nil {
		return UserPasswordResponse{}, err
	}
	payload, err := json.Marshal(map[string]string{"password": password})
	if err != nil {
		return UserPasswordResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, httpURL+"/users/"+username+"/password", bytes.NewReader(payload))
	if err != nil {
		return UserPasswordResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	client, err := newHTTPClient()
	if err != nil {
		return UserPasswordResponse{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return UserPasswordResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return UserPasswordResponse{}, fmt.Errorf("password update failed: %s", resp.Status)
	}
	var out UserPasswordResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return UserPasswordResponse{}, err
	}
	return out, nil
}
