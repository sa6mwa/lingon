//go:build !windows

package desktopnotify

import (
	"context"
	"testing"
)

type stubSender struct {
	calls []Request
	err   error
}

func (s *stubSender) Notify(_ context.Context, req Request) error {
	s.calls = append(s.calls, req)
	return s.err
}

func TestIsInactivityWallMessage(t *testing.T) {
	if !IsInactivityWallMessage("session-a inactive") {
		t.Fatalf("expected inactivity message to match")
	}
	if IsInactivityWallMessage("hello from ops") {
		t.Fatalf("expected non-inactivity wall to be ignored")
	}
}

func TestDesktopEnvLikely(t *testing.T) {
	env := map[string]string{
		"DISPLAY":                  ":0",
		"DBUS_SESSION_BUS_ADDRESS": "unix:path=/tmp/dbus",
	}
	if !desktopEnvLikely(func(key string) string { return env[key] }) {
		t.Fatalf("expected desktop env gate to pass")
	}
	delete(env, "DBUS_SESSION_BUS_ADDRESS")
	if desktopEnvLikely(func(key string) string { return env[key] }) {
		t.Fatalf("expected desktop env gate to fail without bus hint")
	}

	env = map[string]string{
		"XDG_CURRENT_DESKTOP": "GNOME",
		"XDG_RUNTIME_DIR":     "/run/user/1000",
	}
	if !desktopEnvLikely(func(key string) string { return env[key] }) {
		t.Fatalf("expected current desktop plus runtime dir to pass")
	}

	env = map[string]string{
		"XDG_SESSION_TYPE": "wayland",
		"XDG_RUNTIME_DIR":  "/run/user/1000",
	}
	if !desktopEnvLikely(func(key string) string { return env[key] }) {
		t.Fatalf("expected wayland session type plus runtime dir to pass")
	}
}

func TestNotifierSkipsWithoutDesktopEnv(t *testing.T) {
	sender := &stubSender{}
	n := &notifier{
		getenv: func(string) string { return "" },
		sender: sender,
	}

	if err := n.Notify(context.Background(), Request{Title: "title", Body: "body"}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(sender.calls) != 0 {
		t.Fatalf("expected sender not to be called, got %d calls", len(sender.calls))
	}
}

func TestNotifierUsesSenderWhenDesktopEnvLooksValid(t *testing.T) {
	sender := &stubSender{}
	env := map[string]string{
		"WAYLAND_DISPLAY":          "wayland-0",
		"DBUS_SESSION_BUS_ADDRESS": "unix:path=/tmp/dbus",
	}
	n := &notifier{
		getenv: func(key string) string { return env[key] },
		sender: sender,
	}

	req := Request{Title: "title", Body: "body"}
	if err := n.Notify(context.Background(), req); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(sender.calls) != 1 {
		t.Fatalf("expected one sender call, got %d", len(sender.calls))
	}
	if sender.calls[0] != req {
		t.Fatalf("unexpected request: %#v", sender.calls[0])
	}
}
