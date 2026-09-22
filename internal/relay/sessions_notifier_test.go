package relay

import (
	"testing"

	"pkt.systems/lingon/internal/clock"
)

func TestSessionNotifierCoalescesReconnectBurst(t *testing.T) {
	mock := clock.NewMock()
	n := newSessionNotifier()
	n.clock = mock
	const subscribers = 100
	channels := make([]chan struct{}, 0, subscribers)
	for range subscribers {
		ch, unsubscribe := n.Subscribe("mike")
		defer unsubscribe()
		channels = append(channels, ch)
	}

	for range 100 {
		n.Notify("mike")
	}
	for _, ch := range channels {
		select {
		case <-ch:
			t.Fatal("reconnect burst notified before the coalescing window")
		default:
		}
	}

	mock.Add(sessionNotifyDelay)
	for _, ch := range channels {
		select {
		case <-ch:
		default:
			t.Fatal("reconnect burst did not notify every subscriber")
		}
		select {
		case <-ch:
			t.Fatal("reconnect burst produced more than one notification per subscriber")
		default:
		}
	}

	n.Notify("mike")
	mock.Add(sessionNotifyDelay)
	for _, ch := range channels {
		select {
		case <-ch:
		default:
			t.Fatal("later change was lost after the burst")
		}
	}
}

func TestSessionNotifierKeepsUsersSeparate(t *testing.T) {
	mock := clock.NewMock()
	n := newSessionNotifier()
	n.clock = mock
	mike, unsubscribeMike := n.Subscribe("mike")
	defer unsubscribeMike()
	other, unsubscribeOther := n.Subscribe("other")
	defer unsubscribeOther()
	n.Notify("mike")
	mock.Add(sessionNotifyDelay)
	select {
	case <-mike:
	default:
		t.Fatal("mike was not notified")
	}
	select {
	case <-other:
		t.Fatal("unrelated user was notified")
	default:
	}
}
