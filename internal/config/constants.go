package config

const (
	// DefaultConfigDirName is the directory name under the home directory.
	DefaultConfigDirName = ".lingon"
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
	DefaultLogFileName = "lingon.log"

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
	// DefaultBufferLines is the default buffered line count for offline publishing.
	DefaultBufferLines = 5000
	// DefaultTerminalTerm is the default TERM for the PTY session.
	DefaultTerminalTerm = "tmux-256color"
)
