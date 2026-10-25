//go:build integration

package integrationptyattach_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/testclock"
)

func TestLiteralCtrlLRetainsBarWithoutAssumingChildClear(t *testing.T) {
	for _, mode := range []string{"host-local", "attach-single", "attach-multi", "host-remote"} {
		for _, clear := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/child-clear-%t", mode, clear), func(t *testing.T) {
				clk := testclock.New()
				h := newHarness(t, ptytest.WithClock(clk))
				const targetID = "literal-target"
				const label = "ctrl-l-target"
				target := h.StartHost(ptytest.HostOptions{
					SessionID: targetID, SessionName: label, Cols: 80, Rows: 12,
					Shell: literalCtrlLChild(t, clear),
				})
				t.Cleanup(target.Cancel)
				waitForSessions(t, clk, h.Endpoint(), h.AccessToken(), []string{targetID})
				terminal := target
				wantTop := label
				switch mode {
				case "attach-single":
					// A standalone transport client has no multi-session bar.
					wantTop = "CHILD_TOP_ROW"
					terminal = h.StartAttach(ptytest.AttachOptions{
						SessionID: targetID, Cols: 80, Rows: 12, RawInput: true, RequestControl: true,
					})
				case "attach-multi":
					terminal = h.StartMultiAttach(ptytest.MultiAttachOptions{SessionID: targetID, Cols: 80, Rows: 12})
				case "host-remote":
					terminal = h.StartHost(ptytest.HostOptions{
						SessionID: "origin", SessionName: "a-origin", Cols: 80, Rows: 12, Shell: "/bin/sh",
					})
					waitForSessions(t, clk, h.Endpoint(), h.AccessToken(), []string{"origin", targetID})
					terminal.Send("printf 'ORIGIN_%s\\n' READY\r")
					waitLiteralCtrlLScreen(t, terminal, "ORIGIN_READY")
					terminal.SendBytes([]byte{12, 'n'})
				}
				t.Cleanup(terminal.Cancel)
				waitLiteralCtrlLScreen(t, terminal, "CHILD_READY")
				clk.Add(4 * time.Second)
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := terminal.WaitForScreen(ctx, func(screen ptytest.Screen) bool {
					return strings.Contains(screen.Row(0), wantTop)
				}); err != nil {
					t.Fatalf("bar not ready: %v\n%s", err, terminal.Screen().String())
				}
				frozen := clk.Now()
				for i, keys := range [][]byte{{12, 'l'}, {12, 12}, {12, 'l', 'x'}} {
					terminal.DrainRaw()
					terminal.SendBytes(keys)
					waitLiteralCtrlLScreen(t, terminal, fmt.Sprintf("CTRL_L_ACK_%d", i+1))
					if i == 2 {
						waitLiteralCtrlLScreen(t, terminal, "ORDINARY_ACK")
					}
					screen := terminal.Screen()
					if (mode != "attach-single" || !clear) && !strings.Contains(screen.Row(0), wantTop) {
						t.Fatalf("literal Ctrl+L %v hid the bar while the child stayed below row 1:\n%s", keys, screen.String())
					}
					if !clear && strings.Contains(terminal.DrainRaw(), "\x1b[2J") {
						t.Fatal("literal input caused a speculative full-screen clear")
					}
					if !clk.Now().Equal(frozen) {
						t.Fatal("literal input handling required advancing application time")
					}
				}
			})
		}
	}
}

func waitLiteralCtrlLScreen(t *testing.T, terminal *ptytest.PTYSession, token string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := terminal.WaitForScreen(ctx, func(screen ptytest.Screen) bool { return screen.Contains(token) }); err != nil {
		t.Fatalf("waiting for child output %q: %v\n%s", token, err, terminal.Screen().String())
	}
}

func literalCtrlLChild(t *testing.T, clear bool) string {
	t.Helper()
	clearOutput := ":"
	if clear {
		clearOutput = "printf '\033[2J'"
	}
	// This script only receives the child PTY allocated and owned by ptytest.
	script := "#!/bin/sh\nset -eu\nold=$(stty -g)\ntrap 'stty \"$old\"' EXIT\nstty -echo -icanon min 1 time 0\n" +
		"printf '\033[1;1HCHILD_TOP_ROW\033[5;1HCHILD_READY'\nn=0\nwhile :; do\n" +
		"b=$(dd bs=1 count=1 2>/dev/null | od -An -tu1 | tr -d ' \\n')\n" +
		"case $b in\n12) n=$((n+1)); " + clearOutput + "; printf '\033[5;1HCTRL_L_ACK_%s\033[K' \"$n\";;\n" +
		"120) printf '\033[6;1HORDINARY_ACK';;\n*) exit 1;;\nesac\ndone\n"
	path := filepath.Join(t.TempDir(), "literal-ctrl-l-child.sh")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
