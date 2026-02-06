package session

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/terminal"
)

func TestLocalSessionRespondsToDSR(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	script := `#!/bin/bash
set -euo pipefail
read_until() {
  local end="$1"
  local buf=""
  for i in {1..128}; do
    if IFS= read -r -t 0.2 -n 1 ch; then
      buf+="$ch"
      if [[ "$ch" == "$end" ]]; then
        printf "%s" "$buf"
        return 0
      fi
    fi
  done
  printf "%s" "$buf"
  return 1
}

printf '\033[5n'
resp=$(read_until "n") || { echo "DSR5-TIMEOUT"; exit 2; }
if [[ "$resp" != $'\x1b[0n' ]]; then
  echo "DSR5-BAD"
  exit 3
fi

printf '\033[6n'
resp=$(read_until "R") || { echo "DSR6-TIMEOUT"; exit 4; }
if [[ "$resp" != $'\x1b['*'R' ]]; then
  echo "DSR6-BAD"
  exit 5
fi

printf '\033[?5n'
resp=$(read_until "n") || { echo "DSR5P-TIMEOUT"; exit 10; }
if [[ "$resp" != $'\x1b[?0n' ]]; then
  echo "DSR5P-BAD"
  exit 11
fi

printf '\033[?6n'
resp=$(read_until "R") || { echo "DSR6P-TIMEOUT"; exit 12; }
if [[ "$resp" != $'\x1b[?'*'R' ]]; then
  echo "DSR6P-BAD"
  exit 13
fi

printf '\033[c'
resp=$(read_until "c") || { echo "DA1-TIMEOUT"; exit 6; }
if [[ "$resp" != $'\x1b[?1;2c' ]]; then
  echo "DA1-BAD"
  exit 7
fi

printf '\033[>c'
resp=$(read_until "c") || { echo "DA2-TIMEOUT"; exit 8; }
if [[ "$resp" != $'\x1b[>0;0;0c' ]]; then
  echo "DA2-BAD"
  exit 9
fi

printf $'\033]10;?\a'
resp=$(read_until $'\a') || { echo "OSC10-TIMEOUT"; exit 14; }
if [[ "$resp" != $'\x1b]10;rgb:ffff/ffff/ffff\a' ]]; then
  echo "OSC10-BAD"
  exit 15
fi

printf $'\033]11;?\a'
resp=$(read_until $'\a') || { echo "OSC11-TIMEOUT"; exit 16; }
if [[ "$resp" != $'\x1b]11;rgb:0000/0000/0000\a' ]]; then
  echo "OSC11-BAD"
  exit 17
fi

printf $'\033]12;?\a'
resp=$(read_until $'\a') || { echo "OSC12-TIMEOUT"; exit 18; }
if [[ "$resp" != $'\x1b]12;rgb:ffff/ffff/ffff\a' ]]; then
  echo "OSC12-BAD"
  exit 19
fi

echo "DSR-OK"
exit 0
`

	dir := t.TempDir()
	path := filepath.Join(dir, "dsr.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	clk := clock.NewMock()
	var buf bytes.Buffer
	var once sync.Once
	done := make(chan struct{})

	local := newLocalSession(ctx, localSessionOptions{
		ID:      "dsr",
		Name:    "dsr",
		Shell:   path,
		Term:    "xterm-256color",
		Cols:    80,
		Rows:    24,
		Respawn: false,
		Clock:   clk,
		OnPTYRead: func(data []byte) {
			buf.Write(data)
			if bytes.Contains(buf.Bytes(), []byte("DSR-OK")) ||
				bytes.Contains(buf.Bytes(), []byte("DSR5-BAD")) ||
				bytes.Contains(buf.Bytes(), []byte("DSR6-BAD")) ||
				bytes.Contains(buf.Bytes(), []byte("DA1-BAD")) ||
				bytes.Contains(buf.Bytes(), []byte("DA2-BAD")) ||
				bytes.Contains(buf.Bytes(), []byte("OSC10-BAD")) ||
				bytes.Contains(buf.Bytes(), []byte("OSC11-BAD")) ||
				bytes.Contains(buf.Bytes(), []byte("OSC12-BAD")) ||
				bytes.Contains(buf.Bytes(), []byte("DSR5-TIMEOUT")) ||
				bytes.Contains(buf.Bytes(), []byte("DSR6-TIMEOUT")) ||
				bytes.Contains(buf.Bytes(), []byte("DA1-TIMEOUT")) ||
				bytes.Contains(buf.Bytes(), []byte("DA2-TIMEOUT")) ||
				bytes.Contains(buf.Bytes(), []byte("OSC10-TIMEOUT")) ||
				bytes.Contains(buf.Bytes(), []byte("OSC11-TIMEOUT")) ||
				bytes.Contains(buf.Bytes(), []byte("OSC12-TIMEOUT")) {
				once.Do(func() { close(done) })
			}
		},
	})

	go local.Run()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for DSR response; output: %q", buf.String())
	}

	out := buf.String()
	if strings.Contains(out, "DSR5-TIMEOUT") || strings.Contains(out, "DSR6-TIMEOUT") ||
		strings.Contains(out, "DSR5P-TIMEOUT") || strings.Contains(out, "DSR6P-TIMEOUT") ||
		strings.Contains(out, "DA1-TIMEOUT") || strings.Contains(out, "DA2-TIMEOUT") ||
		strings.Contains(out, "OSC10-TIMEOUT") || strings.Contains(out, "OSC11-TIMEOUT") ||
		strings.Contains(out, "OSC12-TIMEOUT") {
		t.Fatalf("DSR/DA response not received; output: %q", out)
	}
	if strings.Contains(out, "DSR5-BAD") || strings.Contains(out, "DSR6-BAD") ||
		strings.Contains(out, "DSR5P-BAD") || strings.Contains(out, "DSR6P-BAD") ||
		strings.Contains(out, "DA1-BAD") || strings.Contains(out, "DA2-BAD") ||
		strings.Contains(out, "OSC10-BAD") || strings.Contains(out, "OSC11-BAD") ||
		strings.Contains(out, "OSC12-BAD") {
		t.Fatalf("DSR/DA response malformed; output: %q", out)
	}
	if !strings.Contains(out, "DSR-OK") {
		t.Fatalf("DSR response missing; output: %q", out)
	}
}

func TestLocalSessionDSRUsesCursorQueryOverride(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}

	script := `#!/bin/bash
set -euo pipefail
stty -echo -icanon min 0 time 1
trap 'stty echo icanon' EXIT
printf '\033[6n'
resp=""
for i in {1..64}; do
  if IFS= read -r -t 0.2 -n 1 ch; then
    resp+="$ch"
    if [[ "$ch" == "R" ]]; then
      break
    fi
  fi
done
if [[ "$resp" != *"R" ]]; then
  echo "DSR-TIMEOUT"
  exit 2
fi
if [[ "$resp" == $'\x1b[3;4R' ]]; then
  echo "DSR-OVERRIDE-OK"
  exit 0
fi
echo "DSR-OVERRIDE-BAD"
printf '%q\n' "$resp"
`

	dir := t.TempDir()
	path := filepath.Join(dir, "dsr_override.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	clk := clock.NewMock()
	var buf bytes.Buffer
	var once sync.Once
	done := make(chan struct{})

	local := newLocalSession(ctx, localSessionOptions{
		ID:      "dsr_override",
		Name:    "dsr_override",
		Shell:   path,
		Term:    "xterm-256color",
		Cols:    80,
		Rows:    24,
		Respawn: false,
		Clock:   clk,
		CursorQuery: func(terminal.Snapshot) (row, col int, ok bool) {
			return 3, 4, true
		},
		OnPTYRead: func(data []byte) {
			buf.Write(data)
			if bytes.Contains(buf.Bytes(), []byte("R")) ||
				bytes.Contains(buf.Bytes(), []byte("DSR-TIMEOUT")) {
				once.Do(func() { close(done) })
			}
		},
	})

	go local.Run()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for DSR response; output: %q", buf.String())
	}

	out := buf.String()
	if strings.Contains(out, "DSR-TIMEOUT") {
		t.Fatalf("DSR response not received; output: %q", out)
	}
	if strings.Contains(out, "DSR-OVERRIDE-BAD") {
		t.Fatalf("DSR response did not match override; output: %q", out)
	}
	if !strings.Contains(out, "DSR-OVERRIDE-OK") {
		t.Fatalf("expected DSR override ok; output: %q", out)
	}
}
