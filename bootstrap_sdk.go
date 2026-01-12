package lingon

import (
	"context"
	"fmt"
	"os"

	"go.yaml.in/yaml/v3"

	"pkt.systems/lingon/internal/tlsmgr"
	"pkt.systems/pslog"
)

// Bootstrap initializes TLS assets and writes the config to the default path.
func Bootstrap(ctx context.Context, cfg Config, logger pslog.Logger) (string, error) {
	if logger == nil {
		logger = pslog.LoggerFromEnv()
	}

	if err := tlsmgr.GenerateAll(ctx, cfg.Server.TLS.Dir, cfg.Server.TLS.Hostname, logger); err != nil {
		return "", err
	}

	path := DefaultConfigPath()
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("config already exists at %s", path)
	} else if !os.IsNotExist(err) {
		return "", err
	}

	if err := os.MkdirAll(DefaultConfigDir(), 0o700); err != nil {
		return "", err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	logger.Info("bootstrapped config", "path", path)
	return path, nil
}
