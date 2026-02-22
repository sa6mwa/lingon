package main

import (
	"context"
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
	if path == "" {
		return pslog.NoopLogger(), io.NopCloser(&noopReader{}), nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, err
	}
	logger := pslog.LoggerFromEnv(context.Background(), pslog.WithEnvWriter(file))
	return logger.With("app", "lingon"), file, nil
}

type noopReader struct{}

func (noopReader) Read(_ []byte) (int, error) {
	return 0, io.EOF
}
