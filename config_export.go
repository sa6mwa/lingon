package lingon

import "pkt.systems/lingon/internal/config"

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

// Loader wraps configuration loading via Viper.
type Loader = config.Loader

const (
	// DefaultConfigDirName is the directory name under the home directory.
	DefaultConfigDirName = config.DefaultConfigDirName
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
	// DefaultBufferLines is the default buffered line count.
	DefaultBufferLines = config.DefaultBufferLines
	// DefaultTerminalTerm is the default TERM for the PTY session.
	DefaultTerminalTerm = config.DefaultTerminalTerm
)

// NewLoader returns a config loader with defaults wired.
func NewLoader() *config.Loader {
	return config.NewLoader()
}

// DefaultConfig returns default Lingon configuration.
func DefaultConfig() Config {
	return config.DefaultConfig()
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
