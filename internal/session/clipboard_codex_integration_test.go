package session_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clipboard"
	"pkt.systems/lingon/internal/ptytest"
)

// An explicitly supplied Codex runs only inside the host's test-owned sub-PTY,
// with an isolated profile, no shared daemon and no submitted model request.
// Ordinary regression coverage models its X11 reader shutdown without requiring
// a user-installed application; this additionally proves actual interoperability.
func TestClipboardCodexThroughOwnedTMUX(t *testing.T) {
	codex := os.Getenv("LINGON_TEST_CODEX")
	if codex == "" {
		t.Skip("set LINGON_TEST_CODEX for Codex interoperability")
	}
	t.Setenv("DISPLAY", "")
	t.Setenv("XAUTHORITY", "")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	root := t.TempDir()
	profile := filepath.Join(root, "codex")
	for _, dir := range []string{profile, filepath.Join(root, "home"), filepath.Join(root, "tmp"), filepath.Join(root, "work")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	config := fmt.Sprintf(`model = "clipboard-fixture"
model_provider = "clipboard_fixture"
check_for_update_on_startup = false
[model_providers.clipboard_fixture]
name = "Clipboard fixture"
base_url = "http://127.0.0.1:9/v1"
wire_api = "responses"
requires_openai_auth = false
[projects.%q]
trust_level = "trusted"
`, filepath.Join(root, "work"))
	if err := os.WriteFile(filepath.Join(profile, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	shell := filepath.Join(root, "codex-shell")
	script := fmt.Sprintf("#!/bin/sh\nunset TMUX TMUX_PANE WAYLAND_DISPLAY WAYLAND_SOCKET DBUS_SESSION_BUS_ADDRESS\nexport HOME=%s CODEX_HOME=%s TMPDIR=%s TERM=xterm-256color\nexec %s --no-daemon --no-alt-screen --sandbox read-only --ask-for-approval never --cd %s\n", quote(filepath.Join(root, "home")), quote(profile), quote(filepath.Join(root, "tmp")), quote(codex), quote(filepath.Join(root, "work")))
	if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	recorder := ptytest.NewWSRecorder()
	h := ptytest.New(t, ptytest.WithWSRecorder(recorder))
	defer h.Close()
	host := h.StartHost(ptytest.HostOptions{SessionID: "tmux-image", Shell: shell, Cols: 110, Rows: 24})
	defer host.Close()
	waitClipboardRegistration(t, ctx, recorder, "tmux-image")
	source, err := clipboard.NewEndpoint(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	terminal := startClipboardOwnedTMUX(t, ctx, h, source)
	defer terminal.Close()
	waitClipboardScreen(t, ctx, terminal, "clipboard-fixture")
	terminal.Send("BEFORE_PASTE_23")
	waitClipboardScreen(t, ctx, terminal, "BEFORE_PASTE_23")
	// Codex/arboard requests image/png, so exercise its native image format
	// twice. Encoded JPEG delivery is covered by the protocol-reader fixture.
	for i := range 2 {
		if err := source.Set(ctx, clipboardPTYImage(t, "image/png")); err != nil {
			t.Fatal(err)
		}
		terminal.SendBytes([]byte{0x16})
		waitClipboardScreen(t, ctx, terminal, fmt.Sprintf("[Image #%d]", i+1))
		marker := fmt.Sprintf("AFTER_PASTE_23_%d", i+1)
		terminal.Send(marker)
		waitClipboardScreen(t, ctx, terminal, marker)
	}
}
