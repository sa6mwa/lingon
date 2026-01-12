package main

import (
	"io"
	"os"
	"path/filepath"

	"pkt.systems/lingon"
	"pkt.systems/pslog"
)

func openClientLogger(path string) (pslog.Logger, io.Closer, error) {
	if path == "" {
		path = lingon.DefaultLogPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, err
	}
	logger := pslog.LoggerFromEnv(pslog.WithEnvWriter(file))
	return logger, file, nil
}
