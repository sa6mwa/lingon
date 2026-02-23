#!/usr/bin/env bash
set -euo pipefail

AVD_NAME="${AVD_NAME:-lingon-aosp-35}"
CONFIG_PATH="${HOME}/.android/avd/${AVD_NAME}.avd/config.ini"
AVD_RAM_MB="${AVD_RAM_MB:-4096}"

if [[ ! -f "${CONFIG_PATH}" ]]; then
  echo "AVD config not found: ${CONFIG_PATH}" >&2
  exit 1
fi

drop_avd_conf_key() {
  local key="$1"
  local avd_conf_path="${HOME}/.android/avd/${AVD_NAME}.avd/AVD.conf"
  if [[ ! -f "${avd_conf_path}" ]]; then
    return
  fi
  sed -i "/^${key}=.*/d" "${avd_conf_path}"
}

ensure_kv() {
  local key="$1"
  local value="$2"
  if grep -q "^${key}[[:space:]]*=" "${CONFIG_PATH}"; then
    sed -i "s/^${key}[[:space:]]*=.*/${key} = ${value}/" "${CONFIG_PATH}"
  else
    printf '\n%s = %s\n' "${key}" "${value}" >> "${CONFIG_PATH}"
  fi
}

ensure_kv "hw.gpu.enabled" "yes"
ensure_kv "hw.gpu.mode" "host"
ensure_kv "hw.ramSize" "${AVD_RAM_MB}"
ensure_kv "hw.cpu.ncore" "6"
ensure_kv "showDeviceFrame" "no"
ensure_kv "hw.keyboard" "yes"
ensure_kv "hw.keyboard.lid" "yes"
ensure_kv "fastboot.forceFastBoot" "no"
ensure_kv "fastboot.forceColdBoot" "yes"
ensure_kv "fastboot.forceChosenSnapshotBoot" "no"
ensure_kv "firstboot.bootFromDownloadableSnapshot" "no"
ensure_kv "firstboot.bootFromLocalSnapshot" "no"
ensure_kv "firstboot.saveToLocalSnapshot" "no"
drop_avd_conf_key 'set\\enforceKeycodeForwarding'

printf 'saveOnExit = false\n' > "${HOME}/.android/avd/${AVD_NAME}.avd/quickbootChoice.ini"

echo "Updated ${CONFIG_PATH} (perf mode: host GPU, ${AVD_RAM_MB}MB RAM, 6 cores, no device frame; hardware keyboard enabled)"
if [[ -f "${HOME}/.android/avd/${AVD_NAME}.avd/AVD.conf" ]]; then
  echo "Updated ${HOME}/.android/avd/${AVD_NAME}.avd/AVD.conf (removed enforceKeycodeForwarding override if present)"
fi
