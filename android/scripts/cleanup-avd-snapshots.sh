#!/usr/bin/env bash
set -euo pipefail

AVD_NAME="${AVD_NAME:-lingon-aosp-35}"
AVD_DIR="${HOME}/.android/avd/${AVD_NAME}.avd"
CONFIG_PATH="${AVD_DIR}/config.ini"
QUICKBOOT_CHOICE_PATH="${AVD_DIR}/quickbootChoice.ini"

if [[ ! -d "${AVD_DIR}" ]]; then
  echo "AVD dir not found: ${AVD_DIR} (skipping snapshot cleanup)"
  exit 0
fi

remove_path_if_present() {
  local path="$1"
  if [[ -e "${path}" ]]; then
    rm -rf "${path}"
    echo "Removed ${path}"
  fi
}

ensure_kv() {
  local key="$1"
  local value="$2"
  if [[ ! -f "${CONFIG_PATH}" ]]; then
    return
  fi
  if grep -q "^${key}[[:space:]]*=" "${CONFIG_PATH}"; then
    sed -i "s/^${key}[[:space:]]*=.*/${key} = ${value}/" "${CONFIG_PATH}"
  else
    printf '\n%s = %s\n' "${key}" "${value}" >> "${CONFIG_PATH}"
  fi
}

remove_path_if_present "${AVD_DIR}/snapshots"
remove_path_if_present "${AVD_DIR}/snapshot.trace"
remove_path_if_present "${AVD_DIR}/read-snapshot.txt"
remove_path_if_present "${QUICKBOOT_CHOICE_PATH}"

ensure_kv "fastboot.forceFastBoot" "no"
ensure_kv "fastboot.forceColdBoot" "yes"
ensure_kv "fastboot.forceChosenSnapshotBoot" "no"
ensure_kv "firstboot.bootFromDownloadableSnapshot" "no"
ensure_kv "firstboot.bootFromLocalSnapshot" "no"
ensure_kv "firstboot.saveToLocalSnapshot" "no"

printf 'saveOnExit = false\n' > "${QUICKBOOT_CHOICE_PATH}"

echo "Snapshot state reset for ${AVD_NAME}; next launch will cold boot without Quick Boot restore/save."
