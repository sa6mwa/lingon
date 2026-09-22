package relay

import (
	"sync"
	"time"

	"pkt.systems/lingon/internal/clock"
)

const sessionNotifyDelay = 100 * time.Millisecond

type sessionNotifier struct {
	mu      sync.Mutex
	subs    map[string]map[chan struct{}]struct{}
	pending map[string]*clock.Timer
	clock   clock.Clock
}

func newSessionNotifier() *sessionNotifier {
	return &sessionNotifier{
		subs:    make(map[string]map[chan struct{}]struct{}),
		pending: make(map[string]*clock.Timer),
		clock:   clock.New(),
	}
}

func (n *sessionNotifier) Subscribe(username string) (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	if username == "" {
		return ch, func() {}
	}
	n.mu.Lock()
	subs := n.subs[username]
	if subs == nil {
		subs = make(map[chan struct{}]struct{})
		n.subs[username] = subs
	}
	subs[ch] = struct{}{}
	n.mu.Unlock()

	return ch, func() {
		n.mu.Lock()
		if subs := n.subs[username]; subs != nil {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(n.subs, username)
				if timer := n.pending[username]; timer != nil {
					timer.Stop()
					delete(n.pending, username)
				}
			}
		}
		n.mu.Unlock()
	}
}

func (n *sessionNotifier) Notify(username string) {
	if username == "" {
		return
	}
	n.mu.Lock()
	if len(n.subs[username]) == 0 || n.pending[username] != nil {
		n.mu.Unlock()
		return
	}
	var timer *clock.Timer
	timer = n.clock.AfterFunc(sessionNotifyDelay, func() {
		n.mu.Lock()
		if n.pending[username] != timer {
			n.mu.Unlock()
			return
		}
		delete(n.pending, username)
		for ch := range n.subs[username] {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
		n.mu.Unlock()
	})
	n.pending[username] = timer
	n.mu.Unlock()
}
