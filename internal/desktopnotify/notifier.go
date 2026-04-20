package desktopnotify

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"pkt.systems/lingon/internal/protocolpb"
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

type noopNotifier struct{}

func (noopNotifier) Notify(context.Context, Request) error {
	return nil
}

func defaultFactory() Notifier {
	if runningUnderTestBinary(os.Args) {
		return noopNotifier{}
	}
	return newNotifier()
}

var newFactory = defaultFactory

// New constructs an environment-aware desktop notifier.
func New() Notifier {
	return newFactory()
}

// SetFactoryForTesting replaces the default notifier factory until the returned
// restore function is called. It is intended for tests that must guarantee no
// real desktop notification backend is reached.
func SetFactoryForTesting(factory func() Notifier) func() {
	prev := newFactory
	if factory == nil {
		newFactory = func() Notifier { return nil }
	} else {
		newFactory = factory
	}
	return func() {
		newFactory = prev
	}
}

func runningUnderTestBinary(args []string) bool {
	if len(args) == 0 {
		return false
	}
	name := filepath.Base(strings.TrimSpace(args[0]))
	if strings.HasSuffix(name, ".test") {
		return true
	}
	for _, arg := range args[1:] {
		arg = strings.TrimSpace(arg)
		if strings.HasPrefix(arg, "-test.") {
			return true
		}
	}
	return false
}

// IsInactivityWallMessage reports whether a wall message matches Lingon's inactivity notification shape.
func IsInactivityWallMessage(message string) bool {
	message = strings.TrimSpace(message)
	return message != "" && strings.HasSuffix(message, " inactive")
}

// IsInactivityWall reports whether a wall carries Lingon's explicit inactivity kind.
func IsInactivityWall(wall *protocolpb.Wall) bool {
	return wall != nil && wall.GetKind() == protocolpb.WallKind_WALL_KIND_INACTIVITY
}

// FormatWallSource formats the transport sender and human session name as one label.
func FormatWallSource(wall *protocolpb.Wall) string {
	if wall == nil {
		return ""
	}
	sender := strings.TrimSpace(wall.GetSender())
	sessionName := strings.TrimSpace(wall.GetSourceSessionName())
	if sender == "" {
		return sessionName
	}
	if sessionName == "" {
		return sender
	}
	return sender + "#" + sessionName
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
