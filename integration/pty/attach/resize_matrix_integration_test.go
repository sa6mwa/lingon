//go:build integration
// +build integration

package integrationptyattach_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/ptytest"
	"pkt.systems/lingon/internal/testutil"
)

func TestSingleAttachRelayViewportMatrixMatchesHostAcrossResizesAndLongOutput(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	h := newHarness(t)
	shell := fixedPromptEmitRowsBash(t)
	host := h.StartHost(ptytest.HostOptions{
		SessionID:   "single-relay-matrix",
		SessionName: "single-relay-matrix",
		Shell:       shell,
		Cols:        40,
		Rows:        10,
	})
	t.Cleanup(host.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"single-relay-matrix"})
	attach := startLingonAttachCLI(t, h, "single-relay-matrix", 40, 10)
	t.Cleanup(attach.Cancel)

	ensureAttachPromptVisibleRealTime(t, host, attach, 10*time.Second)
	runRelayResizeMatrix(t, host, attach, func(cols, rows int) { attach.Resize(cols, rows) })
}

func TestMultiAttachRelayViewportMatrixMatchesHostAcrossResizesAndLongOutput(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	h := newHarness(t)
	shell := fixedPromptEmitRowsBash(t)
	hostA := h.StartHost(ptytest.HostOptions{
		SessionID:   "multi-relay-matrix-a",
		SessionName: "multi-relay-matrix-a",
		Shell:       shell,
		Cols:        40,
		Rows:        10,
	})
	t.Cleanup(hostA.Cancel)
	hostB := h.StartHost(ptytest.HostOptions{
		SessionID:   "multi-relay-matrix-b",
		SessionName: "multi-relay-matrix-b",
		Shell:       shell,
		Cols:        40,
		Rows:        10,
	})
	t.Cleanup(hostB.Cancel)

	waitForSessions(t, h.Clock(), h.Endpoint(), h.AccessToken(), []string{"multi-relay-matrix-a", "multi-relay-matrix-b"})
	attach := startMultiAttachWithoutExplicitTermSize(t, h, ptytest.MultiAttachOptions{
		SessionID: "multi-relay-matrix-a",
		Cols:      40,
		Rows:      10,
	})
	t.Cleanup(attach.session.Cancel)

	ensureAttachPromptVisibleRealTime(t, hostA, attach.session, 10*time.Second)
	runMultiRelayResizeMatrix(t, hostA, attach.session, attach.resize)
}

func TestSingleAttachHeadlessResizeMatrixRendersExpectedViewport(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	cfgDir := shortConfigDir(t)
	const sessionID = "single-headless-matrix"
	stop := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Shell:     fixedPromptEmitRowsBash(t),
	})
	defer stop()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 1
	})

	socketPath, err := headless.SocketPath(cfgDir, sessionID)
	if err != nil {
		t.Fatalf("SocketPath(%q): %v", sessionID, err)
	}

	h := newHarness(t)
	attach := h.StartAttach(ptytest.AttachOptions{
		Endpoint:       "local://headless",
		UnixSocket:     socketPath,
		SessionID:      sessionID,
		RequestControl: true,
		Cols:           40,
		Rows:           10,
		RawInput:       true,
	})
	t.Cleanup(attach.Cancel)

	waitForPromptVisible(t, attach, 8*time.Second)
	runHeadlessResizeMatrix(t, attach, func(cols, rows int) { attach.Resize(cols, rows) })
}

func TestMultiAttachHeadlessResizeMatrixRendersExpectedViewport(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	cfgDir := shortConfigDir(t)
	const sessionID = "multi-headless-matrix-a"
	stopA := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: sessionID,
		Shell:     fixedPromptEmitRowsBash(t),
	})
	defer stopA()
	stopB := startHeadlessDaemon(t, headlessDaemonSpec{
		ConfigDir: cfgDir,
		SessionID: "multi-headless-matrix-b",
		Shell:     fixedPromptEmitRowsBash(t),
	})
	defer stopB()

	waitUntilLocal(t, 8*time.Second, func() bool {
		return localSessionCount(cfgDir) >= 2
	})

	h := newHarness(t)
	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		Endpoint:           "local://headless",
		SessionID:          sessionID,
		Cols:               40,
		Rows:               10,
		AllowOfflineToggle: true,
		RefreshInterval:    120 * time.Millisecond,
		SessionSource:      localHeadlessSessionSource(cfgDir),
		SocketResolver:     localHeadlessSocketResolver(cfgDir),
	})
	t.Cleanup(attach.Cancel)

	waitForPromptVisible(t, attach, 8*time.Second)
	runMultiHeadlessResizeMatrix(t, attach, func(cols, rows int) { attach.Resize(cols, rows) })
}

func runRelayResizeMatrix(t *testing.T, host, attach *ptytest.PTYSession, resize func(cols, rows int)) {
	t.Helper()
	steps := []struct {
		name string
		cols int
		rows int
	}{
		{name: "same", cols: 40, rows: 10},
		{name: "smaller", cols: 30, rows: 8},
		{name: "larger", cols: 60, rows: 14},
		{name: "same-again", cols: 40, rows: 10},
		{name: "smaller-again", cols: 30, rows: 8},
	}

	for _, step := range steps {
		resize(step.cols, step.rows)
		ptytest.Advance(host.Clock(), 250*time.Millisecond)
		runRelaySizeProbe(t, host, attach, step.cols, step.rows, step.name+"-size")
		runRelayLongOutputProbe(t, host, attach, step.cols, step.rows, step.name+"-rows")
	}
}

func runRelaySizeProbe(t *testing.T, host, attach *ptytest.PTYSession, cols, rows int, phase string) {
	t.Helper()
	token := "__SIZE_DONE__"
	attach.Send("stty size; echo " + token + "\r")
	eventuallyWithClockAttach(t, host.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		if !strings.Contains(host.Screen().String(), token) {
			return fmt.Errorf("waiting for size token %s", token)
		}
		if !strings.Contains(host.Screen().String(), "10 40") {
			return fmt.Errorf("host PTY size changed unexpectedly:\n%s", host.Screen().String())
		}
		want := trimRowsRight(cropPadHostBody(host.Screen(), cols, rows))
		got := trimRowsRight(attachBody(attach.Screen()))
		if got != want {
			return fmt.Errorf("%s mismatch\nwant:\n%s\n\ngot:\n%s\n\nhost:\n%s\n\nattach:\n%s", phase, want, got, host.Screen().String(), attach.Screen().String())
		}
		return nil
	})
}

func runRelayLongOutputProbe(t *testing.T, host, attach *ptytest.PTYSession, cols, rows int, phase string) {
	t.Helper()
	attach.Send("emitrows 18\r")
	eventuallyWithClockAttach(t, host.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		if !strings.Contains(host.Screen().String(), "ROW-18") {
			return fmt.Errorf("waiting for long output on host")
		}
		want := trimRowsRight(cropPadHostBody(host.Screen(), cols, rows))
		got := trimRowsRight(attachBody(attach.Screen()))
		if got != want {
			return fmt.Errorf("%s mismatch\nwant:\n%s\n\ngot:\n%s\n\nhost:\n%s\n\nattach:\n%s", phase, want, got, host.Screen().String(), attach.Screen().String())
		}
		return nil
	})
}

func runHeadlessResizeMatrix(t *testing.T, attach *ptytest.PTYSession, resize func(cols, rows int)) {
	t.Helper()
	steps := []struct {
		name string
		cols int
		rows int
	}{
		{name: "same", cols: 40, rows: 10},
		{name: "smaller", cols: 30, rows: 8},
		{name: "larger", cols: 60, rows: 14},
		{name: "same-again", cols: 40, rows: 10},
		{name: "smaller-again", cols: 30, rows: 8},
	}

	for _, step := range steps {
		resize(step.cols, step.rows)
		ptytest.Advance(attach.Clock(), 250*time.Millisecond)
		assertHeadlessPTYSizeViaShell(t, attach, step.cols, step.rows, step.name+"-size")
		assertHeadlessLongOutputScreen(t, attach, step.cols, step.rows, step.name+"-rows")
	}
}

func runMultiHeadlessResizeMatrix(t *testing.T, attach *ptytest.PTYSession, resize func(cols, rows int)) {
	t.Helper()
	steps := []struct {
		name string
		cols int
		rows int
	}{
		{name: "same", cols: 40, rows: 10},
		{name: "smaller", cols: 30, rows: 8},
		{name: "larger", cols: 60, rows: 14},
		{name: "same-again", cols: 40, rows: 10},
		{name: "smaller-again", cols: 30, rows: 8},
	}

	for _, step := range steps {
		resize(step.cols, step.rows)
		ptytest.Advance(attach.Clock(), 250*time.Millisecond)
		assertHeadlessPTYSizeViaShell(t, attach, step.cols, step.rows, step.name+"-size")
		assertMultiHeadlessLongOutputBody(t, attach, step.cols, step.rows, step.name+"-rows")
	}
}

func assertHeadlessPTYSizeViaShell(t *testing.T, attach *ptytest.PTYSession, cols, rows int, phase string) {
	t.Helper()
	token := "__SIZE_HEADLESS__"
	want := fmt.Sprintf("%d %d", rows, cols)
	attach.Send("stty size; echo " + token + "\r")
	eventuallyWithClockAttach(t, attach.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		body := cropPadHostScreen(attach.Screen(), cols, rows)
		if !strings.Contains(body, token) {
			return fmt.Errorf("waiting for headless size token in %s:\n%s", phase, attach.Screen().String())
		}
		if !strings.Contains(body, want) {
			return fmt.Errorf("expected headless PTY size %s in %s, got:\n%s", want, phase, attach.Screen().String())
		}
		return nil
	})
}

func assertHeadlessLongOutputScreen(t *testing.T, attach *ptytest.PTYSession, cols, rows int, phase string) {
	t.Helper()
	lineCount := rows + 6
	attach.Send(fmt.Sprintf("emitrows %d\r", lineCount))
	eventuallyWithClockAttach(t, attach.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		screen := trimRowsRight(cropPadHostScreen(attach.Screen(), cols, rows))
		if !strings.Contains(screen, fmt.Sprintf("ROW-%02d", lineCount)) {
			return fmt.Errorf("waiting for headless long output %s:\n%s", phase, attach.Screen().String())
		}
		want := trimRowsRight(expectedHeadlessScreen(rows, lineCount))
		if screen != want {
			return fmt.Errorf("%s body mismatch\nwant:\n%s\n\ngot:\n%s\n\nscreen:\n%s", phase, want, screen, attach.Screen().String())
		}
		return nil
	})
}

func assertMultiHeadlessLongOutputBody(t *testing.T, attach *ptytest.PTYSession, cols, rows int, phase string) {
	t.Helper()
	lineCount := rows + 6
	attach.Send(fmt.Sprintf("emitrows %d\r", lineCount))
	eventuallyWithClockAttach(t, attach.Clock(), 4*time.Second, 50*time.Millisecond, func() error {
		body := trimRowsRight(attachTerminalContent(attach.Screen()))
		if !strings.Contains(body, fmt.Sprintf("ROW-%02d", lineCount)) {
			return fmt.Errorf("waiting for multi headless long output %s:\n%s", phase, attach.Screen().String())
		}
		want := trimRowsRight(expectedTerminalContent(rows-1, lineCount))
		if body != want {
			return fmt.Errorf("%s body mismatch\nwant:\n%s\n\ngot:\n%s\n\nscreen:\n%s", phase, want, body, attach.Screen().String())
		}
		return nil
	})
}

func expectedHeadlessScreen(rows, lineCount int) string {
	if rows < 1 {
		return ""
	}
	lines := make([]string, 0, lineCount+1)
	for i := 1; i <= lineCount; i++ {
		lines = append(lines, fmt.Sprintf("ROW-%02d", i))
	}
	lines = append(lines, "PROMPT>")
	if len(lines) < rows {
		for len(lines) < rows {
			lines = append(lines, "")
		}
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-rows:], "\n")
}

func expectedTerminalContent(rows, lineCount int) string {
	if rows <= 0 {
		return ""
	}
	lines := make([]string, 0, lineCount+1)
	for i := 1; i <= lineCount; i++ {
		lines = append(lines, fmt.Sprintf("ROW-%02d", i))
	}
	lines = append(lines, "PROMPT>")
	if len(lines) < rows {
		for len(lines) < rows {
			lines = append(lines, "")
		}
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-rows:], "\n")
}

func attachTerminalContent(screen ptytest.Screen) string {
	return strings.Join(terminalStateFromScreen(screen), "\n")
}

func terminalStateFromScreen(screen ptytest.Screen) []string {
	start := 0
	for start < screen.Rows {
		line := strings.TrimRight(screen.Row(start), " ")
		if strings.HasPrefix(line, "ROW-") || strings.HasPrefix(line, "PROMPT>") {
			break
		}
		start++
	}
	lines := make([]string, 0, maxInt(0, screen.Rows-start))
	for row := start; row < screen.Rows; row++ {
		lines = append(lines, screen.Row(row))
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func cropPadHostScreen(screen ptytest.Screen, cols, rows int) string {
	if rows <= 0 || cols <= 0 {
		return ""
	}
	lines := make([]string, 0, rows)
	for row := 0; row < screen.Rows; row++ {
		line := screen.Row(row)
		runes := []rune(line)
		if len(runes) > cols {
			line = string(runes[:cols])
		}
		lines = append(lines, line)
	}
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return strings.Join(lines[:rows], "\n")
}

func trimRowsRight(s string) string {
	rows := strings.Split(s, "\n")
	for i := range rows {
		rows[i] = strings.TrimRight(rows[i], " ")
	}
	return strings.Join(rows, "\n")
}

func waitForPromptVisible(t *testing.T, sess *ptytest.PTYSession, timeout time.Duration) {
	t.Helper()
	eventuallyWithClockAttach(t, sess.Clock(), timeout, 50*time.Millisecond, func() error {
		if !strings.Contains(sess.Screen().String(), "PROMPT>") {
			return fmt.Errorf("prompt not visible yet:\n%s", sess.Screen().String())
		}
		return nil
	})
}

func runMultiRelayResizeMatrix(t *testing.T, host, attach *ptytest.PTYSession, resize func(cols, rows int)) {
	t.Helper()
	steps := []struct {
		name string
		cols int
		rows int
	}{
		{name: "same", cols: 40, rows: 10},
		{name: "smaller", cols: 30, rows: 8},
		{name: "larger", cols: 60, rows: 14},
		{name: "same-again", cols: 40, rows: 10},
		{name: "smaller-again", cols: 30, rows: 8},
	}

	for _, step := range steps {
		resize(step.cols, step.rows)
		ptytest.Advance(host.Clock(), 250*time.Millisecond)
		runMultiRelaySizeProbe(t, host, attach, step.cols, step.rows, step.name+"-size")
		runMultiRelayLongOutputProbe(t, host, attach, step.cols, step.rows, step.name+"-rows")
	}
}

func runMultiRelaySizeProbe(t *testing.T, host, attach *ptytest.PTYSession, cols, rows int, phase string) {
	t.Helper()
	token := "__SIZE_DONE__"
	attach.Send("stty size; echo " + token + "\r")
	eventuallyWithClockAttach(t, host.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		if !strings.Contains(host.Screen().String(), token) {
			return fmt.Errorf("waiting for size token %s", token)
		}
		if !strings.Contains(host.Screen().String(), "10 40") {
			return fmt.Errorf("host PTY size changed unexpectedly:\n%s", host.Screen().String())
		}
		want := trimRowsRight(cropPadHostBody(host.Screen(), cols, rows))
		got := trimRowsRight(attachBody(attach.Screen()))
		if got != want {
			return fmt.Errorf("%s mismatch\nwant:\n%s\n\ngot:\n%s\n\nhost:\n%s\n\nattach:\n%s", phase, want, got, host.Screen().String(), attach.Screen().String())
		}
		return nil
	})
}

func runMultiRelayLongOutputProbe(t *testing.T, host, attach *ptytest.PTYSession, cols, rows int, phase string) {
	t.Helper()
	attach.Send("emitrows 18\r")
	eventuallyWithClockAttach(t, host.Clock(), 3*time.Second, 50*time.Millisecond, func() error {
		if !strings.Contains(host.Screen().String(), "ROW-18") {
			return fmt.Errorf("waiting for long output on host")
		}
		want := trimRowsRight(cropPadHostBody(host.Screen(), cols, rows))
		got := trimRowsRight(attachBody(attach.Screen()))
		if got != want {
			return fmt.Errorf("%s mismatch\nwant:\n%s\n\ngot:\n%s\n\nhost:\n%s\n\nattach:\n%s", phase, want, got, host.Screen().String(), attach.Screen().String())
		}
		return nil
	})
}

func cropPadHostBody(screen ptytest.Screen, cols, rows int) string {
	bodyRows := rows - 1
	if bodyRows <= 0 || cols <= 0 {
		return ""
	}
	lines := make([]string, 0, bodyRows)
	for row := 1; row < screen.Rows; row++ {
		line := screen.Row(row)
		runes := []rune(line)
		if len(runes) > cols {
			line = string(runes[:cols])
		}
		lines = append(lines, line)
	}
	if len(lines) > bodyRows {
		lines = lines[len(lines)-bodyRows:]
	}
	for len(lines) < bodyRows {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func fixedPromptEmitRowsBash(t *testing.T) string {
	t.Helper()
	rcPath := filepath.Join(testutil.TempDir(t), "lingon-attach-rc")
	rc := strings.Join([]string{
		"export PS1='PROMPT>'",
		"emitrows() {",
		"  local n=\"$1\"",
		"  local i=1",
		"  while [ \"$i\" -le \"$n\" ]; do",
		"    printf 'ROW-%02d\\n' \"$i\"",
		"    i=$((i+1))",
		"  done",
		"}",
	}, "\n") + "\n"
	if err := os.WriteFile(rcPath, []byte(rc), 0o644); err != nil {
		t.Fatalf("write rc file: %v", err)
	}
	shellPath := filepath.Join(testutil.TempDir(t), "lingon-attach-shell.sh")
	script := "#!/usr/bin/env bash\nexec /bin/bash --noprofile --rcfile " + shellQuote(rcPath) + " -i\n"
	if err := os.WriteFile(shellPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write shell wrapper: %v", err)
	}
	return shellPath
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
