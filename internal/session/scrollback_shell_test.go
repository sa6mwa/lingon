package session_test

import (
	"os"
	"path/filepath"
	"testing"
)

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
