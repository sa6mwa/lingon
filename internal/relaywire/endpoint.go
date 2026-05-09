package relaywire

import (
	"fmt"
	"net/url"
	"strings"
)

// NormalizeEndpoint converts an HTTP relay endpoint into its websocket base URL.
func NormalizeEndpoint(endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", fmt.Errorf("endpoint must include scheme")
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
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
