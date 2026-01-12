package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"pkt.systems/lingon"
)

func resolveAccessToken(ctx context.Context, endpoint, authPath string) (string, error) {
	state, err := lingon.EnsureAccessToken(ctx, endpoint, authPath)
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
