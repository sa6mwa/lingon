package main

import (
	"fmt"

	"pkt.systems/lingon/internal/sharetoken"
)

func resolveShareToken(raw string) (string, string, error) {
	if raw == "" {
		return "", "", nil
	}
	parsed, err := sharetoken.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("invalid share token: %w", err)
	}
	bare, err := sharetoken.BareToken(parsed)
	if err != nil {
		return "", "", fmt.Errorf("invalid share token: %w", err)
	}
	endpoint := ""
	if parsed.Kind == sharetoken.KindEmbedded {
		endpoint = parsed.Endpoint
	}
	return bare, endpoint, nil
}
