package relayclient

import (
	"context"
	"fmt"
	"sync"
)

// TokenRefresher ensures access tokens are current and updates callers.
// It always re-reads the auth file, so other processes' refreshes are honored.
func TokenRefresher(endpoint, authPath, tlsDir string, insecure bool, onUpdate func(string)) func(context.Context) (string, error) {
	if endpoint == "" || authPath == "" {
		return nil
	}
	var mu sync.Mutex
	return func(ctx context.Context) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		state, err := EnsureAccessTokenWithTLSDirInsecure(ctx, endpoint, authPath, tlsDir, insecure)
		if err != nil {
			return "", err
		}
		if state.AccessToken == "" {
			return "", fmt.Errorf("refresh returned empty token")
		}
		if onUpdate != nil {
			onUpdate(state.AccessToken)
		}
		return state.AccessToken, nil
	}
}
