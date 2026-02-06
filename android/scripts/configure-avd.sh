#!/usr/bin/env bash
set -euo pipefail

AVD_NAME="${AVD_NAME:-lingon-aosp-35}"
CONFIG_PATH="${HOME}/.android/avd/${AVD_NAME}.avd/config.ini"

if [[ ! -f "${CONFIG_PATH}" ]]; then
  echo "AVD config not found: ${CONFIG_PATH}" >&2
  exit 1
fi

ensure_kv() {
  local key="$1"
  local value="$2"
  if grep -q "^${key}[[:space:]]*=" "${CONFIG_PATH}"; then
    sed -i "s/^${key}[[:space:]]*=.*/${key} = ${value}/" "${CONFIG_PATH}"
  else
    printf '\n%s = %s\n' "${key}" "${value}" >> "${CONFIG_PATH}"
  fi
}

ensure_kv "hw.keyboard" "yes"
ensure_kv "hw.dPad" "yes"

echo "Updated ${CONFIG_PATH} (hw.keyboard=yes, hw.dPad=yes)"
