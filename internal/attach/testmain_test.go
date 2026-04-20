package attach

import (
	"context"
	"os"
	"testing"

	"pkt.systems/lingon/internal/desktopnotify"
	"pkt.systems/lingon/internal/testpty"
)

type testNoopNotifier struct{}

func (testNoopNotifier) Notify(context.Context, desktopnotify.Request) error {
	return nil
}

func TestMain(m *testing.M) {
	if handled, code, err := testpty.MaybeReexecOwnedPTY(); handled {
		if err != nil {
			_, _ = os.Stderr.WriteString(err.Error() + "\n")
			if code == 0 {
				code = 1
			}
		}
		os.Exit(code)
	}
	restore := desktopnotify.SetFactoryForTesting(func() desktopnotify.Notifier {
		return testNoopNotifier{}
	})
	code := m.Run()
	restore()
	os.Exit(code)
}
