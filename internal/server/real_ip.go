package server

import (
	"net"
	"net/http"
	"strings"
)

// RealIP returns the best-effort client IP address for a request.
func RealIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if forwarded := r.Header.Get("Forwarded"); forwarded != "" {
		if ip := parseForwarded(forwarded); ip != "" {
			return ip
		}
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip := parseXForwardedFor(xff); ip != "" {
			return ip
		}
	}
	if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
		if ip := cleanIPValue(xrip); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func parseForwarded(value string) string {
	parts := strings.Split(value, ",")
	for _, part := range parts {
		subparts := strings.Split(part, ";")
		for _, sub := range subparts {
			sub = strings.TrimSpace(sub)
			if !strings.HasPrefix(strings.ToLower(sub), "for=") {
				continue
			}
			ip := strings.TrimSpace(sub[4:])
			ip = strings.Trim(ip, "\"")
			if cleaned := cleanIPValue(ip); cleaned != "" {
				return cleaned
			}
		}
	}
	return ""
}

func parseXForwardedFor(value string) string {
	parts := strings.Split(value, ",")
	for _, part := range parts {
		ip := strings.TrimSpace(part)
		if cleaned := cleanIPValue(ip); cleaned != "" {
			return cleaned
		}
	}
	return ""
}

func cleanIPValue(value string) string {
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if lower == "unknown" {
		return ""
	}
	value = strings.TrimSpace(strings.Trim(value, "\""))
	if strings.HasPrefix(value, "[") {
		if idx := strings.Index(value, "]"); idx > 0 {
			value = value[1:idx]
		}
	}
	if host, _, err := net.SplitHostPort(value); err == nil && host != "" {
		return host
	}
	if strings.Contains(value, ":") {
		if ip := net.ParseIP(value); ip != nil {
			return ip.String()
		}
	}
	if ip := net.ParseIP(value); ip != nil {
		return ip.String()
	}
	return value
}
