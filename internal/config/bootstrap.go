package config

import "path/filepath"

// BootstrapConfig returns default configuration values using relative paths.
// Paths are intended to be resolved relative to the config file location.
func BootstrapConfig() Config {
	tlsDir := DefaultTLSDirName
	logPath := ""
	if DefaultLogFileName != "" {
		logPath = DefaultLogFileName
	}

	return Config{
		Server: ServerConfig{
			Listen:    DefaultListenAddr,
			DataDir:   ".",
			UsersFile: DefaultUsersFileName,
			BasePath:  DefaultBasePath,
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
			AuthFile:            DefaultAuthFileName,
			LogFile:             logPath,
			LoginNonInteractive: false,
		},
		Terminal: TerminalConfig{
			Term:            DefaultTerminalTermValue(),
			Respawn:         DefaultTerminalRespawn,
			ScrollbackLines: DefaultScrollbackLines,
			Theme:           DefaultTerminalTheme,
		},
	}
}
