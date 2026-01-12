package config

// DefaultConfig returns the default configuration values.
func DefaultConfig() Config {
	cfgDir := DefaultConfigDir()

	return Config{
		Server: ServerConfig{
			Listen:    DefaultListenAddr,
			DataDir:   cfgDir,
			UsersFile: DefaultUsersPath(),
			BasePath:  DefaultBasePath,
			TLS: TLSConfig{
				Mode:     DefaultTLSMode,
				Dir:      DefaultTLSDir(),
				CacheDir: DefaultTLSCacheDir(),
			},
		},
		Client: ClientConfig{
			Endpoint:    DefaultClientEndpoint,
			AuthFile:    DefaultAuthPath(),
			LogFile:     DefaultLogPath(),
			BufferLines: DefaultBufferLines,
		},
		Terminal: TerminalConfig{
			Term: DefaultTerminalTerm,
		},
	}
}
