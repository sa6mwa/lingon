package session_test

import (
	"os"
	"path/filepath"
	"testing"
)

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
