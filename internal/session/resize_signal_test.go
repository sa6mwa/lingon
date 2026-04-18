package session

import "testing"

func TestSubscribeResizeSignalsDisabled(t *testing.T) {
	t.Parallel()

	ch, stop := subscribeResizeSignals(true)
	t.Cleanup(stop)

	if ch != nil {
		t.Fatal("expected disabled resize signal subscription to return nil channel")
	}
}

func TestSubscribeResizeSignalsEnabled(t *testing.T) {
	t.Parallel()

	ch, stop := subscribeResizeSignals(false)
	t.Cleanup(stop)

	if ch == nil {
		t.Fatal("expected enabled resize signal subscription to return channel")
	}
}
