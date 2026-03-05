package headless

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"pkt.systems/lingon/internal/config"
)

const (
	// EnvForeground marks the re-exec child process that should run headless foreground mode.
	EnvForeground = "__LINGON__HEADLESS"
	// ForegroundValue is the enabled value for EnvForeground.
	ForegroundValue = "true"
	// DirName is the directory under config dir that stores headless metadata.
	DirName = "headless"
	// StateFileName is the persisted state file name under DirName.
	StateFileName = "state.json"
	// RoutedStatusSenderConnected marks connected-status wall frames routed from headless host.
	RoutedStatusSenderConnected = "__lingon_headless_status_connected__"
	// RoutedStatusSenderLost marks connection-lost wall frames routed from headless host.
	RoutedStatusSenderLost = "__lingon_headless_status_lost__"
	// RoutedStatusSenderBackoff marks reconnect-backoff wall frames routed from headless host.
	RoutedStatusSenderBackoff = "__lingon_headless_status_backoff__"
	// RoutedStatusSenderInfo marks generic info-status wall frames routed from headless host.
	RoutedStatusSenderInfo = "__lingon_headless_status_info__"
	// RoutedStatusSenderError marks generic error-status wall frames routed from headless host.
	RoutedStatusSenderError = "__lingon_headless_status_error__"
)

var sessionSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// BaseDir returns the headless metadata directory for a config directory.
func BaseDir(configDir string) string {
	trimmed := strings.TrimSpace(configDir)
	if trimmed == "" {
		trimmed = config.DefaultConfigDir()
	}
	return filepath.Join(trimmed, DirName)
}

// StatePath returns the persisted headless state file path.
func StatePath(configDir string) string {
	return filepath.Join(BaseDir(configDir), StateFileName)
}

// SocketPath returns the unix socket path for a session id.
func SocketPath(configDir, sessionID string) (string, error) {
	normalized, err := NormalizeSessionID(sessionID)
	if err != nil {
		return "", err
	}
	return filepath.Join(BaseDir(configDir), normalized+".sock"), nil
}

// NormalizeSessionID sanitizes a session id for filesystem/socket usage.
func NormalizeSessionID(sessionID string) (string, error) {
	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" {
		return "", fmt.Errorf("session id is required")
	}
	normalized := sessionSanitizer.ReplaceAllString(trimmed, "-")
	normalized = strings.Trim(normalized, "-.")
	if normalized == "" {
		return "", fmt.Errorf("session id %q is invalid", sessionID)
	}
	return normalized, nil
}

// IsForegroundEnv reports whether the current process should run headless foreground mode.
func IsForegroundEnv(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), ForegroundValue)
}

// IsRoutedStatusSender reports whether sender is a routed headless status marker.
func IsRoutedStatusSender(sender string) bool {
	switch strings.TrimSpace(sender) {
	case RoutedStatusSenderConnected, RoutedStatusSenderLost, RoutedStatusSenderBackoff, RoutedStatusSenderInfo, RoutedStatusSenderError:
		return true
	default:
		return false
	}
}
