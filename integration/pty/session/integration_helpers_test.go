//go:build integration
// +build integration

package integrationptysession_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"

	"pkt.systems/lingon/internal/terminal"
)

func snapshotCellAt(snap terminal.Snapshot, row, col int) (terminal.Cell, bool) {
	if row < 1 || col < 1 || row > snap.Rows || col > snap.Cols {
		return terminal.Cell{}, false
	}
	idx := (row-1)*snap.Cols + (col - 1)
	if idx < 0 || idx >= len(snap.Cells) {
		return terminal.Cell{}, false
	}
	return snap.Cells[idx], true
}

func snapshotRows(snap terminal.Snapshot) []string {
	lines := make([]string, snap.Rows)
	for y := 0; y < snap.Rows; y++ {
		var row strings.Builder
		for x := 0; x < snap.Cols; x++ {
			idx := y*snap.Cols + x
			if idx < 0 || idx >= len(snap.Cells) {
				row.WriteRune(' ')
				continue
			}
			cell := snap.Cells[idx]
			if cell.Mode&terminal.ModeHidden != 0 {
				row.WriteRune(' ')
				continue
			}
			if cell.Grapheme != "" {
				row.WriteString(cell.Grapheme)
				if w := runewidth.StringWidth(cell.Grapheme); w > 1 {
					x += w - 1
				}
				continue
			}
			if cell.Rune == 0 {
				row.WriteRune(' ')
			} else {
				row.WriteRune(cell.Rune)
			}
		}
		lines[y] = row.String()
	}
	return lines
}

func reconnectShell(t *testing.T) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "reconnect-shell.sh")
	const script = `#!/usr/bin/env bash
set -u
stty -echo -icanon min 1 time 0
cleanup() {
  stty sane 2>/dev/null || true
}
trap cleanup EXIT INT TERM

prompt='PROMPT> '
line=''

draw_prompt() {
  printf '%s' "$prompt"
}

redraw_line() {
  printf '\r\033[2K'
  draw_prompt
  printf '%s' "$line"
}

run_line() {
  printf '\r\n'
  case "$line" in
    echo\ *)
      printf '%s\r\n' "${line#echo }"
      ;;
  esac
  line=''
  draw_prompt
}

clear_screen() {
  printf '\033[H\033[2J'
  line=''
  draw_prompt
}

draw_prompt
while IFS= read -rsn1 ch; do
  if [ -z "$ch" ]; then
    run_line
    continue
  fi
  case "$ch" in
    $'\f')
      clear_screen
      ;;
    $'\r'|$'\n')
      run_line
      ;;
    $'\177'|$'\b')
      if [ -n "$line" ]; then
        line="${line%?}"
        redraw_line
      fi
      ;;
    *)
      line+="$ch"
      printf '%s' "$ch"
      ;;
  esac
done
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write reconnect shell wrapper: %v", err)
	}
	return scriptPath
}

func scrollbackShell(t *testing.T) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "scrollback-shell.sh")
	const script = `#!/usr/bin/env bash
set -u
stty -echo -icanon min 1 time 0
cleanup() {
  stty sane 2>/dev/null || true
}

trap cleanup EXIT INT TERM

prompt='PROMPT> '
line=''

draw_prompt() {
  printf '%s' "$prompt"
}

redraw_line() {
  printf '\r\033[2K'
  draw_prompt
  printf '%s' "$line"
}

emit_printf_literal() {
  local text="$1"
  text="${text//\\n/$'\r\n'}"
  printf '%s' "$text"
}

emit_numbered_lines() {
  local prefix="$1"
  local width="$2"
  local count="$3"
  local i
  for ((i=1; i<=count; i++)); do
    printf '%s-%0*d\r\n' "$prefix" "$width" "$i"
  done
}

run_line() {
  printf '\r\n'
  case "$line" in
    '')
      ;;
    echo\ *)
      printf '%s\r\n' "${line#echo }"
      ;;
    emit\ *)
      printf '%s\r\n' "${line#emit }"
      ;;
    emit-lines\ *)
      local rest="${line#emit-lines }"
      local prefix width count
      read -r prefix width count <<<"$rest"
      if [[ -n "${prefix:-}" && -n "${width:-}" && -n "${count:-}" ]]; then
        emit_numbered_lines "$prefix" "$width" "$count"
      fi
      ;;
  esac
  if [[ "$line" =~ ^printf\ \'(.*)\'$ ]]; then
    emit_printf_literal "${BASH_REMATCH[1]}"
  elif [[ "$line" =~ ^i=1\;\ while\ \[\ \$i\ -le\ ([0-9]+)\ \]\;\ do\ printf\ \'([A-Z]+)-%0([0-9]+)d\\n\'\ \$i\;\ i=\$\(\(i\+1\)\)\;\ done$ ]]; then
    emit_numbered_lines "${BASH_REMATCH[2]}" "${BASH_REMATCH[3]}" "${BASH_REMATCH[1]}"
  fi
  line=''
  draw_prompt
}

clear_screen() {
  printf '\033[H\033[2J'
  line=''
  draw_prompt
}

draw_prompt
while IFS= read -rsn1 ch; do
  if [ -z "$ch" ]; then
    run_line
    continue
  fi
  case "$ch" in
    $'\f')
      clear_screen
      ;;
    $'\r'|$'\n')
      run_line
      ;;
    $'\177'|$'\b')
      if [ -n "$line" ]; then
        line="${line%?}"
        redraw_line
      fi
      ;;
    *)
      line+="$ch"
      printf '%s' "$ch"
      ;;
  esac
done
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write scrollback shell wrapper: %v", err)
	}
	return scriptPath
}

func preservedWideScreenShell(t *testing.T) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "preserved-wide-screen.sh")
	const script = `#!/usr/bin/env bash
set -u
stty -echo -icanon min 1 time 0
cleanup() {
  stty sane 2>/dev/null || true
}
trap cleanup EXIT INT TERM

printf '\033[H\033[2J'
for i in $(seq 1 12); do
  printf '\033[%d;1HROW-%02d-LEFT-1234567890-MID-abcdefghij-RIGHT-%02d' "$i" "$i" "$i"
done
printf '\033[1;1H'

while :; do
  sleep 1
done
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write preserved wide screen shell: %v", err)
	}
	return scriptPath
}

func preservedWideScreenBottomCursorShell(t *testing.T) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "preserved-wide-screen-bottom-cursor.sh")
	const script = `#!/usr/bin/env bash
set -u
stty -echo -icanon min 1 time 0
cleanup() {
  stty sane 2>/dev/null || true
}
trap cleanup EXIT INT TERM

printf '\033[H\033[2J'
for i in $(seq 1 11); do
  printf '\033[%d;1HROW-%02d-LEFT-1234567890-MID-abcdefghij-RIGHT-%02d' "$i" "$i" "$i"
done
printf '\033[12;1HPROMPT> '

while :; do
  sleep 1
done
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write preserved bottom-cursor shell: %v", err)
	}
	return scriptPath
}

func preservedWideScrollOutputShell(t *testing.T) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "preserved-wide-scroll-output.sh")
	const script = `#!/usr/bin/env bash
set -u
stty -echo -icanon min 1 time 0
cleanup() {
  stty sane 2>/dev/null || true
}
trap cleanup EXIT INT TERM

printf '\033[H\033[2J'
for i in $(seq 1 30); do
  printf 'ROW-%02d-LEFT-1234567890-MID-abcdefghij-RIGHT-%02d\r\n' "$i" "$i"
done
printf 'PROMPT> '

while :; do
  sleep 1
done
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write preserved wide scroll output shell: %v", err)
	}
	return scriptPath
}
