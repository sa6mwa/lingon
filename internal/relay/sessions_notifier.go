package relay

import "sync"

type sessionNotifier struct {
	mu   sync.Mutex
	subs map[string]map[chan struct{}]struct{}
}

func newSessionNotifier() *sessionNotifier {
	return &sessionNotifier{
		subs: make(map[string]map[chan struct{}]struct{}),
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
	subs := n.subs[username]
	for ch := range subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	n.mu.Unlock()
}
