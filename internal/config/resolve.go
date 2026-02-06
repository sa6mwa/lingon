package config

import (
	"path/filepath"
	"strings"
)

func resolveConfigPaths(cfg *Config, configPath string) {
	if cfg == nil || configPath == "" {
		return
	}
	baseDir := filepath.Dir(configPath)
	cfg.Server.DataDir = resolvePath(baseDir, cfg.Server.DataDir)
	cfg.Server.UsersFile = resolvePath(baseDir, cfg.Server.UsersFile)
	cfg.Server.TLS.Dir = resolvePath(baseDir, cfg.Server.TLS.Dir)
	cfg.Server.TLS.CacheDir = resolvePath(baseDir, cfg.Server.TLS.CacheDir)
	cfg.Client.AuthFile = resolvePath(baseDir, cfg.Client.AuthFile)
	cfg.Client.LogFile = resolvePath(baseDir, cfg.Client.LogFile)
}

// ResolvePaths updates path values on cfg relative to the config file path.
func ResolvePaths(cfg *Config, configPath string) {
	resolveConfigPaths(cfg, configPath)
}

func resolvePath(baseDir, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(baseDir, value)
}
