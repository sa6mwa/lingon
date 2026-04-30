package lingon

import (
	"time"

	"pkt.systems/lingon/internal/config"
)

// Config mirrors the Lingon configuration.
type Config = config.Config

// ServerConfig configures the relay/server.
type ServerConfig = config.ServerConfig

// ClientConfig configures client defaults.
type ClientConfig = config.ClientConfig

// TerminalConfig configures terminal defaults.
type TerminalConfig = config.TerminalConfig

// TLSConfig configures TLS for the relay/server.
type TLSConfig = config.TLSConfig

// WebUIConfig configures web UI behavior.
type WebUIConfig = config.WebUIConfig

// WallConfig configures relay wall behavior.
type WallConfig = config.WallConfig

// Loader wraps configuration loading via Viper.
type Loader = config.Loader

const (
	// DefaultConfigDirName is the directory name under the home directory.
	DefaultConfigDirName = config.DefaultConfigDirName
	// ConfigDirEnv is the environment variable that overrides the Lingon config root.
	ConfigDirEnv = config.ConfigDirEnv
	// DefaultConfigFileName is the default config file name.
	DefaultConfigFileName = config.DefaultConfigFileName
	// DefaultAuthFileName is the default auth file name.
	DefaultAuthFileName = config.DefaultAuthFileName
	// DefaultTLSDirName is the TLS directory name under the config directory.
	DefaultTLSDirName = config.DefaultTLSDirName
	// DefaultTLSCacheDirName is the ACME cache directory name under the TLS directory.
	DefaultTLSCacheDirName = config.DefaultTLSCacheDirName
	// DefaultUsersFileName is the default users file name.
	DefaultUsersFileName = config.DefaultUsersFileName
	// DefaultLogFileName is the default client log file name.
	DefaultLogFileName = config.DefaultLogFileName

	// DefaultListenAddr is the default server listen address.
	DefaultListenAddr = config.DefaultListenAddr
	// DefaultBasePath is the default HTTP base path.
	DefaultBasePath = config.DefaultBasePath
	// DefaultTLSMode is the default TLS mode.
	DefaultTLSMode = config.DefaultTLSMode
	// DefaultClientEndpoint is the default client endpoint.
	DefaultClientEndpoint = config.DefaultClientEndpoint
	// DefaultSessionID is the default session ID.
	DefaultSessionID = config.DefaultSessionID
	// DefaultTerminalCols is the default terminal column count.
	DefaultTerminalCols = config.DefaultTerminalCols
	// DefaultTerminalRows is the default terminal row count.
	DefaultTerminalRows = config.DefaultTerminalRows
	// DefaultScrollbackLines is the default buffered scrollback line count.
	DefaultScrollbackLines = config.DefaultScrollbackLines
	// DefaultReplayHistoryBytes is the default byte cap for relay replay history.
	DefaultReplayHistoryBytes = config.DefaultReplayHistoryBytes
	// DefaultTerminalTerm is the default TERM for the PTY session.
	DefaultTerminalTerm = config.DefaultTerminalTerm
	// DefaultTerminalRespawn controls default respawn behavior for local PTYs.
	DefaultTerminalRespawn = config.DefaultTerminalRespawn
	// DefaultTerminalTheme is the default TUI theme name.
	DefaultTerminalTheme = config.DefaultTerminalTheme
	// DefaultTerminalHostnameOnly controls endpoint banner display mode.
	DefaultTerminalHostnameOnly = config.DefaultTerminalHostnameOnly
	// DefaultTerminalDisableDesktopNotifications controls desktop notification delivery.
	DefaultTerminalDisableDesktopNotifications = config.DefaultTerminalDisableDesktopNotifications
	// DefaultConnectLimitDisable disables global connection limiting by default.
	DefaultConnectLimitDisable = config.DefaultConnectLimitDisable
	// DefaultConnectLimitBurst is the default burst size for connection limiting.
	DefaultConnectLimitBurst = config.DefaultConnectLimitBurst
	// DefaultConnectLimitCount is the default count per window for connection limiting.
	DefaultConnectLimitCount = config.DefaultConnectLimitCount
	// DefaultConnectLimitWindow is the default window duration for connection limiting.
	DefaultConnectLimitWindow = config.DefaultConnectLimitWindow
	// DefaultWebUINoBanner disables web UI login branding by default.
	DefaultWebUINoBanner = config.DefaultWebUINoBanner
	// DefaultWallTimeout is the default wall message display duration.
	DefaultWallTimeout = config.DefaultWallTimeout
	// DefaultWallInactiveAfterCSV is the default inactivity cycle levels for wall notifications.
	DefaultWallInactiveAfterCSV = config.DefaultWallInactiveAfterCSV
	// DefaultWallInactiveAfter is the first default inactivity level used by legacy on/off callers.
	DefaultWallInactiveAfter = config.DefaultWallInactiveAfter
)

// NewLoader returns a config loader with defaults wired.
func NewLoader() *config.Loader {
	return config.NewLoader()
}

// DefaultConfig returns default Lingon configuration.
func DefaultConfig() Config {
	return config.DefaultConfig()
}

// BootstrapConfig returns default Lingon configuration with relative paths.
func BootstrapConfig() Config {
	return config.BootstrapConfig()
}

// ApplyConfigOverrides applies dot-delimited overrides to a config.
func ApplyConfigOverrides(cfg Config, overrides map[string]any) (Config, error) {
	return config.ApplyOverrides(cfg, overrides)
}

// ConfigForDir returns default Lingon configuration rooted in the provided config dir.
func ConfigForDir(dir string) Config {
	return config.ForDir(dir)
}

// DefaultConfigDir returns the default config directory.
func DefaultConfigDir() string {
	return config.DefaultConfigDir()
}

// DefaultConfigPath returns the default config path.
func DefaultConfigPath() string {
	return config.DefaultConfigPath()
}

// DefaultAuthPath returns the default auth file path.
func DefaultAuthPath() string {
	return config.DefaultAuthPath()
}

// DefaultLogPath returns the default client log path.
func DefaultLogPath() string {
	return config.DefaultLogPath()
}

// DefaultTerminalTermValue returns the TERM default derived from the environment.
func DefaultTerminalTermValue() string {
	return config.DefaultTerminalTermValue()
}

// DefaultTLSDir returns the default TLS directory.
func DefaultTLSDir() string {
	return config.DefaultTLSDir()
}

// DefaultTLSCacheDir returns the default TLS cache directory.
func DefaultTLSCacheDir() string {
	return config.DefaultTLSCacheDir()
}

// DefaultUsersPath returns the default users file path.
func DefaultUsersPath() string {
	return config.DefaultUsersPath()
}

// DefaultWallInactiveAfterLevels returns default inactivity cycle levels.
func DefaultWallInactiveAfterLevels() []time.Duration {
	return config.DefaultWallInactiveAfterLevels()
}

// ParseWallInactiveAfterLevels parses wall inactivity level CSV configuration.
func ParseWallInactiveAfterLevels(raw string) ([]time.Duration, error) {
	return config.ParseWallInactiveAfterLevels(raw)
}
