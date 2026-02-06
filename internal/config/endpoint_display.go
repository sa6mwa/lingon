package config

import (
	"net/url"
	"strings"
)

// EndpointDisplay returns the endpoint string for UI status banners.
// When hostnameOnly is true, only the hostname portion is returned.
func EndpointDisplay(endpoint string, hostnameOnly bool) string {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" || !hostnameOnly {
		return trimmed
	}
	candidate := trimmed
	if !strings.Contains(candidate, "://") {
		candidate = "https://" + candidate
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return trimmed
	}
	if host := strings.TrimSpace(parsed.Hostname()); host != "" {
		return host
	}
	return trimmed
}
