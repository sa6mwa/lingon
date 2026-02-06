package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/authstore"
)

func resolveAccessToken(ctx context.Context, endpoint, authPath, tlsDir string, insecure bool) (string, error) {
	state, err := lingon.EnsureAccessTokenWithTLSDirInsecure(ctx, endpoint, authPath, tlsDir, insecure)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("auth file not found at %s; run `lingon login -e %s`", authPath, endpoint)
		}
		return "", fmt.Errorf("%s; run `lingon login -e %s`", err.Error(), endpoint)
	}
	if state.AccessToken == "" {
		return "", fmt.Errorf("access token missing; run `lingon login -e %s`", endpoint)
	}
	return state.AccessToken, nil
}

func hasValidRefreshToken(endpoint, authPath string, now time.Time) (bool, error) {
	state, err := authstore.LoadForEndpoint(authPath, endpoint)
	if err != nil {
		return false, err
	}
	return state.RefreshValidAt(now), nil
}
