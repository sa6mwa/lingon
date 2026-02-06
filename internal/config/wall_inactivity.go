package config

import (
	"fmt"
	"strings"
	"time"
)

// ParseWallInactiveAfterLevels parses CSV inactivity levels used by wall inactivity cycling.
func ParseWallInactiveAfterLevels(raw string) ([]time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultWallInactiveAfterLevels(), nil
	}

	parts := strings.Split(raw, ",")
	levels := make([]time.Duration, 0, len(parts))
	seen := make(map[time.Duration]struct{}, len(parts))
	for i, part := range parts {
		token := strings.TrimSpace(part)
		if token == "" {
			return nil, fmt.Errorf("wall inactive level %d is empty", i+1)
		}
		d, err := time.ParseDuration(token)
		if err != nil {
			return nil, fmt.Errorf("invalid wall inactive duration %q: %w", token, err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("wall inactive duration must be > 0: %q", token)
		}
		if _, exists := seen[d]; exists {
			continue
		}
		seen[d] = struct{}{}
		levels = append(levels, d)
	}
	if len(levels) == 0 {
		return nil, fmt.Errorf("wall inactive levels are empty")
	}
	return levels, nil
}
