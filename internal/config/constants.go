package config

import "time"

const (
	// DefaultConfigDirName is the directory name under the home directory.
	DefaultConfigDirName = ".lingon"
	// ConfigDirEnv is the environment variable that overrides the Lingon config root.
	ConfigDirEnv = "LINGON_CONFIG_DIR"
	// DefaultConfigFileName is the default config file name.
	DefaultConfigFileName = "config.yaml"
	// DefaultAuthFileName is the default auth file name.
	DefaultAuthFileName = "auth.json"
	// DefaultTLSDirName is the TLS directory name under the config directory.
	DefaultTLSDirName = "tls"
	// DefaultTLSCacheDirName is the ACME cache directory name under the TLS directory.
	DefaultTLSCacheDirName = "cache"
	// DefaultUsersFileName is the default users file name.
	DefaultUsersFileName = "users.json"
	// DefaultLogFileName is the default client log file name.
	DefaultLogFileName = ""

	// DefaultListenAddr is the default server listen address.
	DefaultListenAddr = "127.0.0.1:12843"
	// DefaultBasePath is the default HTTP base path.
	DefaultBasePath = "/v1"
	// DefaultTLSMode is the default TLS mode.
	DefaultTLSMode = "auto"
	// DefaultClientEndpoint is the default client endpoint.
	DefaultClientEndpoint = "https://localhost:12843/v1"
	// DefaultSessionID is the default session ID used for local testing.
	DefaultSessionID = "session_test"
	// DefaultTerminalCols is the default terminal columns.
	DefaultTerminalCols = 80
	// DefaultTerminalRows is the default terminal rows.
	DefaultTerminalRows = 24
	// DefaultScrollbackLines is the default buffered scrollback line count.
	DefaultScrollbackLines = 5000
	// DefaultReplayHistoryBytes is the default byte cap for relay replay history.
	DefaultReplayHistoryBytes = 512 * 1024
	// DefaultWSReadLimit is the maximum websocket frame size to accept.
	DefaultWSReadLimit = 1 << 20
	// DefaultTerminalTerm is the fallback TERM for the PTY session.
	DefaultTerminalTerm = "xterm-256color"
	// DefaultTerminalRespawn controls default respawn behavior for local PTYs.
	DefaultTerminalRespawn = false
	// DefaultTerminalTheme is the default TUI theme name.
	DefaultTerminalTheme = "default"
	// DefaultTerminalHostnameOnly controls endpoint banner display mode.
	DefaultTerminalHostnameOnly = false
	// DefaultTerminalDisableDesktopNotifications controls desktop notification delivery.
	DefaultTerminalDisableDesktopNotifications = false

	// DefaultConnectLimitDisable disables global connection limiting by default.
	DefaultConnectLimitDisable = true
	// DefaultConnectLimitBurst is the default burst size for connection limiting.
	DefaultConnectLimitBurst = 40
	// DefaultConnectLimitCount is the default count per window for connection limiting.
	DefaultConnectLimitCount = 200
	// DefaultConnectLimitWindow is the default window duration for connection limiting.
	DefaultConnectLimitWindow = 30 * time.Minute
	// DefaultWebUINoBanner disables web UI login branding by default.
	DefaultWebUINoBanner = false
	// DefaultWallTimeout is the default wall message display duration.
	DefaultWallTimeout = 5 * time.Second
	// DefaultWallInactiveAfterCSV is the default inactivity cycle levels for wall notifications.
	DefaultWallInactiveAfterCSV = "2m,5m,15m"
	// DefaultWallInactiveAfter is the first default inactivity level used by legacy on/off callers.
	DefaultWallInactiveAfter = 2 * time.Minute
)

var defaultWallInactiveAfterLevels = []time.Duration{
	2 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
}

// DefaultWallInactiveAfterLevels returns default inactivity cycle levels.
func DefaultWallInactiveAfterLevels() []time.Duration {
	levels := make([]time.Duration, len(defaultWallInactiveAfterLevels))
	copy(levels, defaultWallInactiveAfterLevels)
	return levels
}
