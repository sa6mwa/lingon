#!/usr/bin/env bash
set -euo pipefail

ADB_BIN="${ADB:-adb}"

if ! command -v "${ADB_BIN}" >/dev/null 2>&1; then
  echo "adb not found (set ADB=... or install platform-tools)." >&2
  exit 1
fi

devices="$("${ADB_BIN}" devices 2>/dev/null || true)"
offline_serials="$(
  printf '%s\n' "${devices}" \
    | awk 'NF>=2 && $1 ~ /^emulator-/ && $2 == "offline" {print $1}'
)"

if [[ -z "${offline_serials}" ]]; then
  echo "No offline emulator devices found."
  exit 0
fi

echo "Stopping offline emulator devices:"
printf '%s\n' "${offline_serials}" | sed 's/^/  /'

while IFS= read -r serial; do
  [[ -z "${serial}" ]] && continue
  "${ADB_BIN}" -s "${serial}" emu kill >/dev/null 2>&1 || true
done <<< "${offline_serials}"

sleep 2

remaining="$(
  "${ADB_BIN}" devices 2>/dev/null \
    | awk 'NF>=2 && $1 ~ /^emulator-/ && $2 == "offline" {print $1}'
)"

if [[ -n "${remaining}" ]]; then
  echo "Restarting adb server to clear offline entries."
  "${ADB_BIN}" kill-server >/dev/null 2>&1 || true
  "${ADB_BIN}" start-server >/dev/null 2>&1 || true
fi
