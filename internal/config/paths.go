package config

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultConfigDir returns the default Lingon config directory.
func DefaultConfigDir() string {
	if dir := strings.TrimSpace(os.Getenv(ConfigDirEnv)); dir != "" {
		return filepath.Clean(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return DefaultConfigDirName
	}
	return filepath.Join(home, DefaultConfigDirName)
}

// DefaultConfigPath returns the default Lingon config file path.
func DefaultConfigPath() string {
	return filepath.Join(DefaultConfigDir(), DefaultConfigFileName)
}

// DefaultAuthPath returns the default Lingon auth file path.
func DefaultAuthPath() string {
	return filepath.Join(DefaultConfigDir(), DefaultAuthFileName)
}

// DefaultLogPath returns the default Lingon client log file path.
func DefaultLogPath() string {
	if DefaultLogFileName == "" {
		return ""
	}
	return filepath.Join(DefaultConfigDir(), DefaultLogFileName)
}

// DefaultTLSDir returns the default TLS directory.
func DefaultTLSDir() string {
	return filepath.Join(DefaultConfigDir(), DefaultTLSDirName)
}

// DefaultTLSCacheDir returns the default TLS cache directory.
func DefaultTLSCacheDir() string {
	return filepath.Join(DefaultTLSDir(), DefaultTLSCacheDirName)
}

// DefaultUsersPath returns the default users file path.
func DefaultUsersPath() string {
	return filepath.Join(DefaultConfigDir(), DefaultUsersFileName)
}
