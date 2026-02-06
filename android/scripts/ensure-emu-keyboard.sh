#!/usr/bin/env bash
set -euo pipefail

ADB_BIN="${ADB:-adb}"
ADB_SERIAL="${ADB_SERIAL:-}"
BOOT_TIMEOUT_SECS="${BOOT_TIMEOUT_SECS:-120}"
ADB_REVERSE_PORT="${ADB_REVERSE_PORT:-12843}"
EMULATOR_ENDPOINT="${EMULATOR_ENDPOINT:-}"

if ! command -v "${ADB_BIN}" >/dev/null 2>&1; then
  echo "adb not found (set ADB=... or install platform-tools)." >&2
  exit 1
fi

adb_cmd() {
  if [[ -n "${ADB_SERIAL}" ]]; then
    "${ADB_BIN}" -s "${ADB_SERIAL}" "$@"
  else
    "${ADB_BIN}" "$@"
  fi
}

if ! adb_cmd wait-for-device >/dev/null 2>&1; then
  echo "adb wait-for-device failed; emulator may not be running yet." >&2
  exit 0
fi

boot_deadline=$(( $(date +%s) + BOOT_TIMEOUT_SECS ))
while [[ "$(date +%s)" -lt "${boot_deadline}" ]]; do
  boot_completed="$(adb_cmd shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')"
  if [[ "${boot_completed}" == "1" ]]; then
    break
  fi
  sleep 2
done

# Ensure an IME is enabled and show virtual keyboard with hardware keyboard.
adb_cmd shell settings put secure show_ime_with_hard_keyboard 1 >/dev/null 2>&1 || true

ime_list="$(adb_cmd shell ime list -s 2>/dev/null | tr -d '\r')"
latin_ime="$(printf '%s\n' "${ime_list}" | grep -m1 -E 'com\.android\.inputmethod\.latin/\.?LatinIME' || true)"
if [[ -z "${latin_ime}" ]]; then
  latin_ime="$(printf '%s\n' "${ime_list}" | head -n1)"
fi

if [[ -n "${latin_ime}" ]]; then
  adb_cmd shell ime set "${latin_ime}" >/dev/null 2>&1 || true
  adb_cmd shell settings put secure default_input_method "${latin_ime}" >/dev/null 2>&1 || true
fi

qemu_prop="$(adb_cmd shell getprop ro.kernel.qemu 2>/dev/null | tr -d '\r')"
if [[ "${qemu_prop}" == "1" ]]; then
  adb_cmd reverse "tcp:${ADB_REVERSE_PORT}" "tcp:${ADB_REVERSE_PORT}" >/dev/null 2>&1 || true
  endpoint_url="${EMULATOR_ENDPOINT}"
  if [[ -z "${endpoint_url}" ]]; then
    endpoint_url="https://localhost:${ADB_REVERSE_PORT}/v1"
  fi
  adb_cmd shell am broadcast -a systems.pkt.lingon.DEBUG_SET_ENDPOINT --es endpoint "${endpoint_url}" >/dev/null 2>&1 || true
  echo "Ensured emulator IME settings and adb reverse tcp:${ADB_REVERSE_PORT} -> tcp:${ADB_REVERSE_PORT}."
else
  echo "Ensured device IME settings via adb."
fi
