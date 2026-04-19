package attach

import (
	"context"
	"os"
	"testing"

	"pkt.systems/lingon/internal/desktopnotify"
)

type testNoopNotifier struct{}

func (testNoopNotifier) Notify(context.Context, desktopnotify.Request) error {
	return nil
}

func TestMain(m *testing.M) {
	restore := desktopnotify.SetFactoryForTesting(func() desktopnotify.Notifier {
		return testNoopNotifier{}
	})
	code := m.Run()
	restore()
	os.Exit(code)
}
