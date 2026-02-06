#!/usr/bin/env bash
set -euo pipefail

ADB_BIN="${ADB:-adb}"
PORT_START=5554
PORT_END=5584

if ! command -v "${ADB_BIN}" >/dev/null 2>&1; then
  echo "adb not found (set ADB=... or install platform-tools)." >&2
  exit 1
fi

devices="$("${ADB_BIN}" devices 2>/dev/null || true)"
used_ports="$(
  printf '%s\n' "${devices}" \
    | awk 'NF>=2 && $1 ~ /^emulator-/ {sub("emulator-","",$1); print $1}'
)"

port="${PORT_START}"
while [[ "${port}" -le "${PORT_END}" ]]; do
  if ! printf '%s\n' "${used_ports}" | grep -qx "${port}"; then
    echo "${port}"
    exit 0
  fi
  port=$((port + 2))
done

echo "No free emulator ports found in ${PORT_START}-${PORT_END}." >&2
exit 1
