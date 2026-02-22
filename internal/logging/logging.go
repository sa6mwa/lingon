package logging

import (
	"context"
	"sync"

	"pkt.systems/pslog"
)

var (
	defaultLoggerOnce sync.Once
	defaultLogger     pslog.Logger
)

// Default returns the process-wide Lingon logger.
func Default() pslog.Logger {
	defaultLoggerOnce.Do(func() {
		defaultLogger = pslog.LoggerFromEnv(context.Background()).With("app", "lingon")
	})
	return defaultLogger
}
