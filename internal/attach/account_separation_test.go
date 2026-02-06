package attach_test

import (
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/relay"
)

func TestAttachAccountSeparation(t *testing.T) {
	h := newHarness(t)

	userA := h.CreateUserWithToken("alice", "pass-a")
	userB := h.CreateUserWithToken("bob", "pass-b")

	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:   "alice-session",
		SessionName: "alice-session",
		AccessToken: userA.AccessToken,
	})
	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "bob-session",
		SessionName: "bob-session",
		AccessToken: userB.AccessToken,
	})

	waitUntil(t, h.Clock(), 5*time.Second, func() bool { return h.HasHost("alice-session") })
	waitUntil(t, h.Clock(), 5*time.Second, func() bool { return h.HasHost("bob-session") })

	waitForSessions(t, h.Clock(), h.Endpoint(), userA.AccessToken, []string{"alice-session"})
	idsA, err := fetchSessionIDs(h.Endpoint(), userA.AccessToken)
	if err != nil {
		t.Fatalf("fetch alice sessions: %v", err)
	}
	if idsA["bob-session"] {
		t.Fatalf("alice token unexpectedly saw bob session")
	}

	waitForSessions(t, h.Clock(), h.Endpoint(), userB.AccessToken, []string{"bob-session"})
	idsB, err := fetchSessionIDs(h.Endpoint(), userB.AccessToken)
	if err != nil {
		t.Fatalf("fetch bob sessions: %v", err)
	}
	if idsB["alice-session"] {
		t.Fatalf("bob token unexpectedly saw alice session")
	}

	viewTokenA := h.CreateShareToken("alice-session", relay.ShareScopeView, time.Minute)
	viewTokenB := h.CreateShareToken("bob-session", relay.ShareScopeView, time.Minute)

	shareA := h.StartAttach(ptytest.AttachOptions{
		SessionID:  "alice-session",
		ShareToken: viewTokenA,
	})
	hostA.Send("echo ALPHA\r\n")
	waitUntilDebug(t, h.Clock(), 5*time.Second, func() bool {
		screen := shareA.Screen()
		return screen.Contains("ALPHA") && !screen.Contains("BRAVO")
	}, func() string {
		return shareA.Screen().String()
	})

	shareB := h.StartAttach(ptytest.AttachOptions{
		SessionID:  "bob-session",
		ShareToken: viewTokenB,
	})
	hostB.Send("echo BRAVO\r\n")
	waitUntilDebug(t, h.Clock(), 5*time.Second, func() bool {
		screen := shareB.Screen()
		return screen.Contains("BRAVO") && !screen.Contains("ALPHA")
	}, func() string {
		return shareB.Screen().String()
	})
}
