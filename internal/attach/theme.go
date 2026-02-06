package attach

import (
	"strings"

	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/theme"
)

func resolveThemeName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return config.DefaultTerminalTheme
	}
	for _, candidate := range theme.Names() {
		if candidate == trimmed {
			return candidate
		}
	}
	return config.DefaultTerminalTheme
}

func nextThemeName(current string) string {
	names := theme.Names()
	if len(names) == 0 {
		return config.DefaultTerminalTheme
	}
	resolved := resolveThemeName(current)
	for i, name := range names {
		if name == resolved {
			return names[(i+1)%len(names)]
		}
	}
	return names[0]
}
