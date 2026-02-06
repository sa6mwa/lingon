package attach

import "testing"

func TestClientCompositorEnsuresRuntime(t *testing.T) {
	c := &Client{}
	if c.Compositor() == nil {
		t.Fatalf("expected compositor to be initialized")
	}
}

func TestClientSetCompositorNilInitializesRuntime(t *testing.T) {
	c := &Client{
		Endpoint:  "https://relay.example/v1",
		SessionID: "session-a",
		Theme:     "default",
	}
	c.SetCompositor(nil)
	comp := c.Compositor()
	if comp == nil {
		t.Fatalf("expected compositor to be initialized when setting nil")
	}
	state := comp.State()
	if state.SessionID != "session-a" {
		t.Fatalf("expected session id propagated, got %q", state.SessionID)
	}
	if state.Endpoint != "https://relay.example/v1" {
		t.Fatalf("expected endpoint propagated, got %q", state.Endpoint)
	}
}
