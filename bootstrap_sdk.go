package lingon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/tlsmgr"
	"pkt.systems/pslog"
)

// BootstrapOptions configures bootstrap behavior.
type BootstrapOptions struct {
	// ConfigPath is the destination config file path. Defaults to DefaultConfigPath.
	ConfigPath string
	// Config is written when bootstrap creates/overwrites the config file.
	Config Config
	// Force overwrites an existing config file.
	Force bool
	// RegenerateCertificates forces TLS certificate regeneration.
	RegenerateCertificates bool
	// Logger records bootstrap output.
	Logger pslog.Logger
}

// Bootstrap initializes TLS assets and writes the config to the default path.
func Bootstrap(ctx context.Context, cfg Config, logger pslog.Logger) (string, error) {
	configPath := ""
	if strings.TrimSpace(cfg.Server.DataDir) != "" {
		configPath = filepath.Join(cfg.Server.DataDir, DefaultConfigFileName)
	}
	return BootstrapWithOptions(ctx, BootstrapOptions{
		ConfigPath: configPath,
		Config:     cfg,
		Logger:     logger,
	})
}

// BootstrapWithOptions writes configuration and initializes TLS assets.
func BootstrapWithOptions(ctx context.Context, opts BootstrapOptions) (string, error) {
	logger := opts.Logger
	if logger == nil {
		logger = pslog.LoggerFromEnv(context.Background())
	}

	configPath := strings.TrimSpace(opts.ConfigPath)
	if configPath == "" {
		configPath = DefaultConfigPath()
	}
	configPath = filepath.Clean(configPath)

	if stat, err := os.Stat(configPath); err == nil && stat.IsDir() {
		return "", fmt.Errorf("config path %q is a directory", configPath)
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}

	configExists, err := fileExists(configPath)
	if err != nil {
		return "", err
	}

	if configExists && !opts.Force && !opts.RegenerateCertificates {
		logger.Warn("config already exists; skipping bootstrap", "path", configPath)
		return configPath, nil
	}

	configDir := filepath.Dir(configPath)
	var cfg Config
	shouldWriteConfig := !configExists || opts.Force
	if shouldWriteConfig {
		cfg = opts.Config
		if err := os.MkdirAll(configDir, 0o700); err != nil {
			return "", err
		}
		data, err := yaml.Marshal(cfg)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(configPath, data, 0o644); err != nil {
			return "", err
		}
		logger.Info("bootstrapped config", "path", configPath)
	} else {
		loader := config.NewLoader()
		loader.SetConfigFile(configPath)
		loaded, err := loader.Load()
		if err != nil {
			return "", err
		}
		cfg = Config(loaded)
		logger.Warn("config already exists; not overwriting", "path", configPath)
	}

	resolved := cfg
	config.ResolvePaths((*config.Config)(&resolved), configPath)

	if opts.RegenerateCertificates {
		return configPath, tlsmgr.RegenerateAll(ctx, resolved.Server.TLS.Dir, resolved.Server.TLS.Hostname, logger)
	}

	if shouldWriteConfig && shouldGenerateTLS(resolved.Server.TLS.Mode) {
		_, err := tlsmgr.EnsureLocalServerCert(ctx, resolved.Server.TLS.Dir, resolved.Server.TLS.Hostname, logger)
		if err != nil {
			return configPath, err
		}
	}

	return configPath, nil
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func shouldGenerateTLS(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "auto", "bundle":
		return true
	default:
		return false
	}
}
