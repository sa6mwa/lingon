package host

import (
	"fmt"
	"net/url"
	"strings"
)

func normalizeEndpoint(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" {
		return "", fmt.Errorf("endpoint must include scheme")
	}
	wsURL := *parsed
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		wsURL.Scheme = "wss"
	case "http":
		wsURL.Scheme = "ws"
	case "wss", "ws":
	default:
		return "", fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}
	return strings.TrimRight(wsURL.String(), "/"), nil
}
