package mvu

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// ConnectedToMessage formats a connected status banner message.
func ConnectedToMessage(endpoint string) string {
	label := strings.TrimSpace(endpoint)
	if label == "" {
		return "connected"
	}
	return fmt.Sprintf("connected to %s", label)
}

// ConnectionLostMessage formats a persistent reconnect status banner message.
func ConnectionLostMessage(endpoint string) string {
	label := strings.TrimSpace(endpoint)
	if label == "" {
		return "connection lost, reconnecting"
	}
	return fmt.Sprintf("connection lost to %s, reconnecting", label)
}

// ConnectionLostBackoffMessage formats a reconnect countdown status banner message.
func ConnectionLostBackoffMessage(endpoint string, remaining time.Duration) string {
	seconds := int(math.Ceil(remaining.Seconds()))
	if seconds < 0 {
		seconds = 0
	}
	label := strings.TrimSpace(endpoint)
	if label == "" {
		return fmt.Sprintf("connection lost, reconnecting in %ds", seconds)
	}
	return fmt.Sprintf("connection lost to %s, reconnecting in %ds", label, seconds)
}
