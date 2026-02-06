package main

import (
	"fmt"
	"strings"

	"pkt.systems/lingon"
)

func resolveTheme(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return lingon.DefaultTerminalTheme, nil
	}
	for _, candidate := range lingon.ThemeNames() {
		if candidate == trimmed {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("unknown theme: %s", trimmed)
}
