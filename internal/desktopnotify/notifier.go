package desktopnotify

import (
	"context"
	"strings"
)

// Request describes one desktop notification.
type Request struct {
	Title string
	Body  string
}

// Notifier sends best-effort desktop notifications.
type Notifier interface {
	Notify(context.Context, Request) error
}

// New constructs an environment-aware desktop notifier.
func New() Notifier {
	return newNotifier()
}

// IsInactivityWallMessage reports whether a wall message matches Lingon's inactivity notification shape.
func IsInactivityWallMessage(message string) bool {
	message = strings.TrimSpace(message)
	return message != "" && strings.HasSuffix(message, " inactive")
}

func desktopEnvLikely(getenv func(string) string) bool {
	if getenv == nil {
		return false
	}

	hasDisplay := strings.TrimSpace(getenv("DISPLAY")) != "" || strings.TrimSpace(getenv("WAYLAND_DISPLAY")) != ""
	hasDesktop := strings.TrimSpace(getenv("XDG_CURRENT_DESKTOP")) != "" || strings.TrimSpace(getenv("XDG_SESSION_DESKTOP")) != ""
	sessionType := strings.ToLower(strings.TrimSpace(getenv("XDG_SESSION_TYPE")))
	hasSessionType := sessionType == "x11" || sessionType == "wayland"
	hasBus := strings.TrimSpace(getenv("DBUS_SESSION_BUS_ADDRESS")) != "" || strings.TrimSpace(getenv("XDG_RUNTIME_DIR")) != ""

	return (hasDisplay || hasDesktop || hasSessionType) && hasBus
}
