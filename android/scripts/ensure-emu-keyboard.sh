#!/usr/bin/env bash
set -euo pipefail

ADB_BIN="${ADB:-adb}"
ADB_SERIAL="${ADB_SERIAL:-}"
BOOT_TIMEOUT_SECS="${BOOT_TIMEOUT_SECS:-120}"
ADB_REVERSE_PORT="${ADB_REVERSE_PORT:-12843}"
EMULATOR_ENDPOINT="${EMULATOR_ENDPOINT:-}"
AUTO_PUSH_CA_PEM="${AUTO_PUSH_CA_PEM:-1}"
CA_PEM_PATH="${CA_PEM_PATH:-$HOME/.lingon/tls/ca.pem}"
CA_PEM_DEST="${CA_PEM_DEST:-/sdcard/Download/ca.pem}"

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

should_auto_push_ca_pem() {
  case "${AUTO_PUSH_CA_PEM}" in
    1|true|TRUE|yes|YES|on|ON)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
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

qemu_prop="$(adb_cmd shell getprop ro.kernel.qemu 2>/dev/null | tr -d '\r')"
if [[ "${qemu_prop}" == "1" ]]; then
  adb_cmd reverse "tcp:${ADB_REVERSE_PORT}" "tcp:${ADB_REVERSE_PORT}" >/dev/null 2>&1 || true
  endpoint_url="${EMULATOR_ENDPOINT}"
  if [[ -z "${endpoint_url}" ]]; then
    endpoint_url="https://localhost:${ADB_REVERSE_PORT}/v1"
  fi
  adb_cmd shell am broadcast -a systems.pkt.lingon.DEBUG_SET_ENDPOINT --es endpoint "${endpoint_url}" >/dev/null 2>&1 || true
  if should_auto_push_ca_pem; then
    if [[ -f "${CA_PEM_PATH}" ]]; then
      if adb_cmd push "${CA_PEM_PATH}" "${CA_PEM_DEST}" >/dev/null 2>&1; then
        echo "Auto-pushed CA PEM to ${CA_PEM_DEST}."
      else
        echo "Warning: failed to auto-push CA PEM (${CA_PEM_PATH} -> ${CA_PEM_DEST})." >&2
      fi
    fi
  fi
  echo "Ensured emulator adb reverse tcp:${ADB_REVERSE_PORT} -> tcp:${ADB_REVERSE_PORT}."
else
  echo "Ensured device connectivity settings via adb."
fi
