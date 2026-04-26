#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ANDROID_DIR="${ROOT_DIR}/android"

ANDROID_SDK_ROOT="${ANDROID_SDK_ROOT:-$HOME/Android/Sdk}"
ADB_BIN="${ADB:-${ANDROID_SDK_ROOT}/platform-tools/adb}"
EMULATOR_BIN="${EMULATOR:-${ANDROID_SDK_ROOT}/emulator/emulator}"
EMULATOR_FLAGS="${EMULATOR_FLAGS:--gpu host -no-snapshot}"
PRESET="${PRESET:-medium}"
AVD_NAME="${AVD_NAME:-}"
HARNESS_BIN="${ROOT_DIR}/bin/lingon-android-harness"
ARTIFACT_OUT="${ANDROID_DIR}/test-artifacts"
HARNESS_LOG="${ARTIFACT_OUT}/harness.log"
LOCK_PATH="${ARTIFACT_OUT}/integration-test.lock"
CURRENT_TEST_PID=""
CURRENT_TEST_PGID=""
EMULATOR_STARTED="0"
HOST_COLS_OVERRIDE="${HOST_COLS:-}"
HOST_ROWS_OVERRIDE="${HOST_ROWS:-}"
CONFIG_PATH=""
KEEP_EMULATOR="${LINGON_IT_KEEP_EMULATOR:-1}"
RESET_APP_STATE="${LINGON_IT_RESET_APP_STATE:-1}"
APP_ID="systems.pkt.lingon"
TEST_APP_ID="${APP_ID}.test"

resolve_avd_name() {
  if [[ -n "${AVD_NAME}" ]]; then
    echo "${AVD_NAME}"
    return
  fi
  case "${PRESET}" in
    small) echo "lingon-small" ;;
    medium) echo "lingon-medium" ;;
    pixel7) echo "lingon-pixel7" ;;
    pixel9) echo "lingon-pixel9" ;;
    *) echo "lingon-medium" ;;
  esac
}

acquire_lock() {
  if [[ -f "${LOCK_PATH}" ]]; then
    local existing_pid=""
    existing_pid="$(cat "${LOCK_PATH}" 2>/dev/null || true)"
    if [[ -n "${existing_pid}" ]] && kill -0 "${existing_pid}" 2>/dev/null; then
      echo "Another integration test run is active (PID ${existing_pid}). Aborting." >&2
      exit 1
    fi
  fi
  echo "$$" > "${LOCK_PATH}"
}

release_lock() {
  rm -f "${LOCK_PATH}"
}

kill_running_tests() {
  if [[ -n "${CURRENT_TEST_PID}" ]]; then
    if [[ -n "${CURRENT_TEST_PGID}" ]]; then
      kill -TERM -- "-${CURRENT_TEST_PGID}" >/dev/null 2>&1 || true
    fi
    kill -TERM "${CURRENT_TEST_PID}" >/dev/null 2>&1 || true
    pkill -TERM -P "${CURRENT_TEST_PID}" >/dev/null 2>&1 || true
    wait "${CURRENT_TEST_PID}" >/dev/null 2>&1 || true
    CURRENT_TEST_PID=""
    CURRENT_TEST_PGID=""
  fi
}

cleanup() {
  kill_running_tests
  if [[ -n "${HARNESS_PID:-}" ]]; then
    kill "${HARNESS_PID}" >/dev/null 2>&1 || true
    wait "${HARNESS_PID}" >/dev/null 2>&1 || true
  fi
  if [[ "${EMULATOR_STARTED}" == "1" ]] && [[ "${KEEP_EMULATOR}" != "1" ]] && [[ -n "${DEVICE_SERIAL:-}" ]]; then
    "${ADB_BIN}" -s "${DEVICE_SERIAL}" emu kill >/dev/null 2>&1 || true
  fi
  release_lock
  if [[ -n "${CONFIG_PATH:-}" ]]; then
    rm -f "${CONFIG_PATH}" || true
  fi
}

on_interrupt() {
  echo "Interrupted; stopping integration tests..." >&2
  kill_running_tests
  exit 130
}

trap on_interrupt INT
trap on_interrupt TERM
trap cleanup EXIT

mkdir -p "${ROOT_DIR}/bin" "${ARTIFACT_OUT}"
acquire_lock

echo "Building harness..."
(
  cd "${ROOT_DIR}"
  go build -buildvcs=true -o "${HARNESS_BIN}" ./cmd/lingon-android-harness
)

HARNESS_ARGS=()
SPECIAL_TEST="refreshes_sessions_when_host_starts_late"
QUIET_HOST_TEST="tab_switch_does_not_rearm_wall_inactivity_without_terminal_input"
if [[ -z "${HOST_COLS_OVERRIDE}" ]]; then
  HOST_COLS_OVERRIDE="120"
fi
if [[ "${HOST_COLS_OVERRIDE}" =~ ^[0-9]+$ ]]; then
  HARNESS_ARGS+=("-cols" "${HOST_COLS_OVERRIDE}")
fi
if [[ -n "${HOST_ROWS_OVERRIDE}" ]] && [[ "${HOST_ROWS_OVERRIDE}" =~ ^[0-9]+$ ]]; then
  HARNESS_ARGS+=("-rows" "${HOST_ROWS_OVERRIDE}")
fi

run_android_tools() {
  (cd "${ANDROID_DIR}" && go run -buildvcs=true ./cmd/lingon-android-tools "$@")
}

read_config() {
  run_android_tools config-env --config "${CONFIG_PATH}"
}

start_harness() {
  local session_count="$1"
  CONFIG_PATH="$(mktemp)"
  echo "Starting harness (sessions=${session_count})..."
  "${HARNESS_BIN}" -config "${CONFIG_PATH}" -sessions "${session_count}" "${HARNESS_ARGS[@]}" >"${HARNESS_LOG}" 2>&1 &
  HARNESS_PID=$!
  for _ in {1..200}; do
    if [[ -s "${CONFIG_PATH}" ]]; then
      break
    fi
    if ! kill -0 "${HARNESS_PID}" >/dev/null 2>&1; then
      break
    fi
    sleep 0.1
  done
  if [[ ! -s "${CONFIG_PATH}" ]]; then
    echo "Harness did not write config. Log:"
    cat "${HARNESS_LOG}"
    exit 1
  fi
  eval "$(read_config)"
  DEVICE_ENDPOINT="https://localhost:${PORT}/v1"
  echo "Harness endpoint: ${ENDPOINT}"
  echo "Device endpoint: ${DEVICE_ENDPOINT}"
  echo "Sessions: ${SESSIONS}"
  if [[ -n "${SESSIONS2}" ]]; then
    echo "Sessions (user2): ${SESSIONS2}"
  fi
}

stop_harness() {
  if [[ -n "${HARNESS_PID:-}" ]]; then
    kill "${HARNESS_PID}" >/dev/null 2>&1 || true
    wait "${HARNESS_PID}" >/dev/null 2>&1 || true
    HARNESS_PID=""
  fi
  if [[ -n "${CONFIG_PATH:-}" ]]; then
    rm -f "${CONFIG_PATH}"
    CONFIG_PATH=""
  fi
}

reset_test_apps() {
  if [[ "${RESET_APP_STATE}" != "1" ]]; then
    return
  fi

  echo "Resetting Android app state..."
  "${ADB_BIN}" -s "${DEVICE_SERIAL}" wait-for-device >/dev/null 2>&1 || true
  "${ADB_BIN}" -s "${DEVICE_SERIAL}" shell am force-stop "${APP_ID}" >/dev/null 2>&1 || true
  "${ADB_BIN}" -s "${DEVICE_SERIAL}" shell am force-stop "${TEST_APP_ID}" >/dev/null 2>&1 || true
  "${ADB_BIN}" -s "${DEVICE_SERIAL}" shell pm clear "${APP_ID}" >/dev/null 2>&1 || true
  "${ADB_BIN}" -s "${DEVICE_SERIAL}" shell pm clear "${TEST_APP_ID}" >/dev/null 2>&1 || true
  "${ADB_BIN}" -s "${DEVICE_SERIAL}" shell pm grant "${APP_ID}" android.permission.POST_NOTIFICATIONS >/dev/null 2>&1 || true
  "${ADB_BIN}" -s "${DEVICE_SERIAL}" shell cmd appops set "${APP_ID}" POST_NOTIFICATION allow >/dev/null 2>&1 || true
  "${ADB_BIN}" -s "${DEVICE_SERIAL}" shell rm -rf "/sdcard/Android/data/${APP_ID}/files/test-artifacts" >/dev/null 2>&1 || true
  "${ADB_BIN}" -s "${DEVICE_SERIAL}" shell rm -rf "/sdcard/Android/data/${TEST_APP_ID}/files/test-artifacts" >/dev/null 2>&1 || true
}

ensure_adb_reverse() {
  export ADB_REVERSE_PORT="${PORT}"
  echo "Ensuring adb reverse tcp:${PORT}..."
  "${ADB_BIN}" -s "${DEVICE_SERIAL}" reverse --remove-all >/dev/null 2>&1 || true
  "${ADB_BIN}" -s "${DEVICE_SERIAL}" reverse "tcp:${PORT}" "tcp:${PORT}" >/dev/null 2>&1 || true
}

if [[ ! -x "${ADB_BIN}" ]]; then
  echo "adb not found (set ADB or ANDROID_SDK_ROOT)." >&2
  exit 1
fi

"${ANDROID_DIR}/scripts/configure-emulator-host.sh" || true

DEVICE_SERIAL="$("${ADB_BIN}" devices | awk 'NR>1 && $2=="device" {print $1; exit}')"
if [[ -z "${DEVICE_SERIAL}" ]]; then
  AVD_NAME="$(resolve_avd_name)"
  if [[ ! -d "${HOME}/.android/avd/${AVD_NAME}.avd" ]]; then
    echo "Creating AVD ${AVD_NAME} (PRESET=${PRESET})..."
    (cd "${ANDROID_DIR}" && make avd PRESET="${PRESET}")
  fi
  AVD_NAME="${AVD_NAME}" "${ANDROID_DIR}/scripts/cleanup-avd-snapshots.sh" || true
  EMU_PORT="$(ADB="${ADB_BIN}" "${ANDROID_DIR}/scripts/find-emu-port.sh")"
  echo "Starting emulator ${AVD_NAME} on port ${EMU_PORT}..."
  "${EMULATOR_BIN}" -avd "${AVD_NAME}" -port "${EMU_PORT}" ${EMULATOR_FLAGS} >/dev/null 2>&1 &
  DEVICE_SERIAL="emulator-${EMU_PORT}"
  EMULATOR_STARTED="1"
fi

if [[ "${EMULATOR_STARTED}" == "1" ]] && [[ "${KEEP_EMULATOR}" == "1" ]]; then
  echo "Keeping emulator ${DEVICE_SERIAL} running after the test run."
fi

ADB_SERIAL="${DEVICE_SERIAL}"
export ADB_SERIAL
export ADB="${ADB_BIN}"

"${ANDROID_DIR}/scripts/ensure-emu-keyboard.sh" || true

start_harness 2
ensure_adb_reverse

echo "Running connectedDebugAndroidTest..."
set +e
(
  cd "${ANDROID_DIR}"
  TEST_SRC="${ANDROID_DIR}/app/src/androidTest/java/systems/pkt/lingon/EndToEndTest.kt"
  TESTS=()
  ONLY_TEST="${LINGON_IT_ONLY:-}"
  if [[ -f "${TEST_SRC}" ]]; then
    mapfile -t TESTS < <(run_android_tools test-names --file "${TEST_SRC}")
  fi
  if [[ "${#TESTS[@]}" -eq 0 ]]; then
    echo "No tests found in ${TEST_SRC}"
    exit 1
  fi
  TEST_EXIT=0

  run_test_batch() {
    local test_names=("$@")
    if [[ "${#test_names[@]}" -eq 0 ]]; then
      return 0
    fi
    local class_arg=""
    local test_name
    for test_name in "${test_names[@]}"; do
      if [[ -n "${class_arg}" ]]; then
        class_arg+=","
      fi
      class_arg+="systems.pkt.lingon.EndToEndTest#${test_name}"
    done
    reset_test_apps
    ensure_adb_reverse
    if [[ "${#test_names[@]}" -eq 1 ]]; then
      echo "Running ${test_names[0]}..."
    else
      echo "Running ${#test_names[@]} Android tests in one instrumentation batch..."
    fi
    ./gradlew :app:connectedDebugAndroidTest \
      --no-configuration-cache \
      -Plingon.it.class="${class_arg}" \
      -Plingon.it.endpoint="${DEVICE_ENDPOINT}" \
      -Plingon.it.username="${USERNAME}" \
      -Plingon.it.password="${PASSWORD}" \
      -Plingon.it.totp_secret="${TOTP_SECRET}" \
      -Plingon.it.ca_pem_b64="${CA_PEM_B64}" \
      -Plingon.it.sessions="${SESSIONS}" \
      -Plingon.it.view_token="${VIEW_TOKEN}" \
      -Plingon.it.username2="${USERNAME2}" \
      -Plingon.it.password2="${PASSWORD2}" \
      -Plingon.it.totp_secret2="${TOTP_SECRET2}" \
      -Plingon.it.sessions2="${SESSIONS2}" \
      -Plingon.it.view_token2="${VIEW_TOKEN2}" \
      -Plingon.it.host_cols="${HOST_COLS}" \
      -Plingon.it.host_rows="${HOST_ROWS}" &
    CURRENT_TEST_PID=$!
    CURRENT_TEST_PGID="$(ps -o pgid= "${CURRENT_TEST_PID}" | tr -d ' ' || true)"
    wait "${CURRENT_TEST_PID}"
    TEST_EXIT=$?
    CURRENT_TEST_PID=""
    CURRENT_TEST_PGID=""
    return "${TEST_EXIT}"
  }

  BATCH=()
  flush_batch() {
    if [[ "${#BATCH[@]}" -eq 0 ]]; then
      return 0
    fi
    run_test_batch "${BATCH[@]}"
    local status=$?
    BATCH=()
    return "${status}"
  }

  for test_name in "${TESTS[@]}"; do
    if [[ -n "${ONLY_TEST}" ]] && [[ "${test_name}" != "${ONLY_TEST}" ]]; then
      continue
    fi
    if [[ "${test_name}" == "${SPECIAL_TEST}" ]]; then
      flush_batch || break
      stop_harness
      start_harness 0
      ensure_adb_reverse
      run_test_batch "${test_name}" || break
      stop_harness
      start_harness 2
      ensure_adb_reverse
      continue
    fi
    if [[ "${test_name}" == "${QUIET_HOST_TEST}" ]]; then
      flush_batch || break
      stop_harness
      export LINGON_ANDROID_HARNESS_HOST_SHELL=/bin/sh
      start_harness 2
      ensure_adb_reverse
      unset LINGON_ANDROID_HARNESS_HOST_SHELL
      run_test_batch "${test_name}" || break
      stop_harness
      unset LINGON_ANDROID_HARNESS_HOST_SHELL
      start_harness 2
      ensure_adb_reverse
      continue
    fi
    BATCH+=("${test_name}")
  done
  if [[ "${TEST_EXIT}" -eq 0 ]]; then
    flush_batch
    TEST_EXIT=$?
  fi
  stop_harness
  exit "${TEST_EXIT}"
)
TEST_EXIT=$?
set -e

echo "Collecting test artifacts..."
ARTIFACT_DIRS=(
  "/sdcard/Android/data/systems.pkt.lingon/files/test-artifacts"
  "/sdcard/Android/data/systems.pkt.lingon.test/files/test-artifacts"
)
found_artifacts=false
for dir in "${ARTIFACT_DIRS[@]}"; do
  if "${ADB_BIN}" -s "${DEVICE_SERIAL}" shell ls "${dir}" >/dev/null 2>&1; then
    "${ADB_BIN}" -s "${DEVICE_SERIAL}" pull "${dir}" "${ARTIFACT_OUT}" >/dev/null 2>&1 || true
    found_artifacts=true
  fi
done
if [[ "${found_artifacts}" == true ]]; then
  echo "Artifacts pulled to ${ARTIFACT_OUT}"
else
  echo "No test artifacts found."
fi

echo "Collecting logcat..."
"${ADB_BIN}" -s "${DEVICE_SERIAL}" logcat -d > "${ARTIFACT_OUT}/logcat.txt" || true

exit "${TEST_EXIT}"
