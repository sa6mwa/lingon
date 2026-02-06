package server

import (
	"fmt"
	"path"
	"strings"
)

// NormalizeBasePath ensures a base path is well-formed and ready for routing.
// It returns an empty string for the root ("/") base.
func NormalizeBasePath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "/" {
		return "", nil
	}

	if strings.Contains(trimmed, "://") || strings.ContainsAny(trimmed, "?#") {
		return "", fmt.Errorf("base path must be a URL path without scheme, query, or fragment")
	}

	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}

	segments := strings.Split(strings.TrimPrefix(trimmed, "/"), "/")
	for _, seg := range segments {
		if seg == "." || seg == ".." {
			return "", fmt.Errorf("base path must not contain '.' or '..' segments")
		}
	}

	cleaned := path.Clean(trimmed)
	if cleaned == "/" || cleaned == "." {
		return "", nil
	}

	return cleaned, nil
}
