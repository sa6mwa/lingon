package sessionmanage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Call executes one management request. It deliberately never retries mutations.
func Call(ctx context.Context, client *http.Client, endpoint, token string, req Request) (Result, error) {
	if err := Validate(req); err != nil {
		return Result{}, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return Result{}, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/sessions/manage", bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(r)
	if err != nil {
		return Result{}, fmt.Errorf("session operation outcome unknown; refresh sessions before trying again: %w", err)
	}
	defer resp.Body.Close()
	var result Result
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&result); err != nil {
		return Result{}, fmt.Errorf("session operation outcome unknown; refresh sessions before trying again: %w", err)
	}
	if resp.StatusCode != http.StatusOK || result.Error != "" {
		return result, fmt.Errorf("session management: %s", result.Error)
	}
	return result, nil
}
