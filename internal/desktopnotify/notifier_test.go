package desktopnotify

import "testing"

func TestNewDefaultsToNoopNotifierUnderGoTest(t *testing.T) {
	restore := SetFactoryForTesting(defaultFactory)
	defer restore()

	got := New()
	if _, ok := got.(noopNotifier); !ok {
		t.Fatalf("New() notifier = %T, want noopNotifier in test binary", got)
	}
}

func TestRunningUnderTestBinary(t *testing.T) {
	t.Run("suffix", func(t *testing.T) {
		if !runningUnderTestBinary([]string{"/tmp/desktopnotify.test"}) {
			t.Fatalf("expected .test binary to be recognized")
		}
	})
	t.Run("flag", func(t *testing.T) {
		if !runningUnderTestBinary([]string{"lingon", "-test.v"}) {
			t.Fatalf("expected -test flag to be recognized")
		}
	})
	t.Run("normal", func(t *testing.T) {
		if runningUnderTestBinary([]string{"lingon"}) {
			t.Fatalf("did not expect normal binary to be treated as test")
		}
	})
}
