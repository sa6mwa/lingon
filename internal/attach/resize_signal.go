package attach

import (
	"os"
	"os/signal"
	"syscall"
)

func subscribeResizeSignals(disabled bool) (<-chan os.Signal, func()) {
	if disabled {
		return nil, func() {}
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	return ch, func() {
		signal.Stop(ch)
	}
}
