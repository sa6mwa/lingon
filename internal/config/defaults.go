package config

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultConfig returns the default configuration values.
func DefaultConfig() Config {
	return ForDir(DefaultConfigDir())
}

// ForDir returns configuration values rooted in the provided config dir.
func ForDir(dir string) Config {
	cfgDir := strings.TrimSpace(dir)
	if cfgDir == "" {
		cfgDir = DefaultConfigDir()
	} else {
		cfgDir = filepath.Clean(cfgDir)
	}
	tlsDir := filepath.Join(cfgDir, DefaultTLSDirName)
	logPath := ""
	if DefaultLogFileName != "" {
		logPath = filepath.Join(cfgDir, DefaultLogFileName)
	}

	return Config{
		Server: ServerConfig{
			Listen:             DefaultListenAddr,
			DataDir:            cfgDir,
			UsersFile:          filepath.Join(cfgDir, DefaultUsersFileName),
			BasePath:           DefaultBasePath,
			ReplayHistoryBytes: DefaultReplayHistoryBytes,
			TLS: TLSConfig{
				Mode:     DefaultTLSMode,
				Dir:      tlsDir,
				CacheDir: filepath.Join(tlsDir, DefaultTLSCacheDirName),
			},
			WebUI: WebUIConfig{
				NoBanner: DefaultWebUINoBanner,
			},
			Wall: WallConfig{
				Timeout:       DefaultWallTimeout,
				InactiveAfter: DefaultWallInactiveAfterCSV,
			},
			ConnectLimit: ConnectLimitCfg{
				Disable: DefaultConnectLimitDisable,
				Burst:   DefaultConnectLimitBurst,
				Count:   DefaultConnectLimitCount,
				Window:  DefaultConnectLimitWindow,
			},
		},
		Client: ClientConfig{
			Endpoint:            DefaultClientEndpoint,
			AuthFile:            filepath.Join(cfgDir, DefaultAuthFileName),
			LogFile:             logPath,
			LoginNonInteractive: false,
		},
		Terminal: TerminalConfig{
			Term:                        DefaultTerminalTermValue(),
			Respawn:                     DefaultTerminalRespawn,
			ScrollbackLines:             DefaultScrollbackLines,
			Theme:                       DefaultTerminalTheme,
			HostnameOnly:                DefaultTerminalHostnameOnly,
			DisableDesktopNotifications: DefaultTerminalDisableDesktopNotifications,
			WallInactiveAfter:           DefaultWallInactiveAfterCSV,
		},
	}
}

// DefaultTerminalTermValue returns the TERM default derived from the environment.
func DefaultTerminalTermValue() string {
	term := os.Getenv("TERM")
	if term == "" {
		term = DefaultTerminalTerm
	}
	return term
}
