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
HARNESS_ROOT=""
HARNESS_ROOTS=()
KEEP_EMULATOR="${LINGON_IT_KEEP_EMULATOR:-0}"
RESET_APP_STATE="${LINGON_IT_RESET_APP_STATE:-1}"
REUSE_DEVICE="${LINGON_IT_REUSE_DEVICE:-0}"
APP_ID="systems.pkt.lingon"
TEST_APP_ID="${APP_ID}.test"
CGROUP_MONITOR_PID=""

detect_online_cpu_count() {
  local cores=""
  cores="$(getconf _NPROCESSORS_ONLN 2>/dev/null || true)"
  if [[ ! "${cores}" =~ ^[0-9]+$ ]] || [[ "${cores}" -le 0 ]]; then
    cores="$(nproc 2>/dev/null || true)"
  fi
  if [[ ! "${cores}" =~ ^[0-9]+$ ]] || [[ "${cores}" -le 0 ]]; then
    cores="2"
  fi
  echo "${cores}"
}

default_cpu_quota_percent() {
  local cores="$1"
  local half_quota=$((cores * 50))
  if [[ "${half_quota}" -gt 200 ]]; then
    echo "200"
  else
    echo "${half_quota}"
  fi
}

summarize_cgroup_profile() {
  local profile_dir="$1"
  local exit_status="$2"
  local samples="${profile_dir}/samples.jsonl"
  local summary="${profile_dir}/summary.txt"
  mkdir -p "${profile_dir}"
  {
    echo "Android integration resource profile"
    echo "exit_status=${exit_status}"
    echo "unit=${LINGON_IT_CGROUP_UNIT:-}"
    echo "cpu_quota_percent=${LINGON_IT_CPU_QUOTA_PERCENT_EFFECTIVE:-}"
    echo "memory_max=${LINGON_IT_MEMORY_MAX_EFFECTIVE:-}"
    echo "memory_swap_max=${LINGON_IT_MEMORY_SWAP_MAX_EFFECTIVE:-}"
    if [[ -s "${samples}" ]]; then
      awk '
        function value(line, key, fallback, pattern, raw) {
          pattern="\"" key "\":[^,}]*"
          if (match(line, pattern)) {
            raw=substr(line, RSTART + length(key) + 3, RLENGTH - length(key) - 3)
            gsub(/"/, "", raw)
            return raw + 0
          }
          return fallback
        }
        {
          count += 1
          cores=value($0, "cpu_cores", 0)
          mem=value($0, "memory_current_bytes", 0)
          mem_peak=value($0, "memory_peak_bytes", 0)
          throttled=value($0, "cpu_throttled_usec", 0)
          pressure=value($0, "cpu_some_avg10", 0)
          proc_rss=value($0, "peak_process_rss_kb", 0)
          proc_vsz=value($0, "peak_process_vsz_kb", 0)
          if (cores > peak_cores) peak_cores=cores
          sum_cores += cores
          if (mem > peak_mem) peak_mem=mem
          if (mem_peak > peak_mem_peak) peak_mem_peak=mem_peak
          if (throttled > peak_throttled) peak_throttled=throttled
          if (pressure > peak_pressure) peak_pressure=pressure
          if (proc_rss > peak_proc_rss) peak_proc_rss=proc_rss
          if (proc_vsz > peak_proc_vsz) peak_proc_vsz=proc_vsz
        }
        END {
          if (count == 0) exit
          printf "samples=%d\n", count
          printf "peak_cpu_cores=%.2f\n", peak_cores
          printf "avg_cpu_cores=%.2f\n", sum_cores / count
          printf "peak_memory_current_bytes=%.0f\n", peak_mem
          printf "peak_memory_peak_bytes=%.0f\n", peak_mem_peak
          printf "peak_cpu_throttled_usec=%.0f\n", peak_throttled
          printf "peak_cpu_pressure_some_avg10=%.2f\n", peak_pressure
          printf "peak_process_rss_kb=%.0f\n", peak_proc_rss
          printf "peak_process_vsz_kb=%.0f\n", peak_proc_vsz
        }
      ' "${samples}"
    else
      echo "samples=0"
    fi
    echo "samples_path=${samples}"
    echo "top_processes_path=${profile_dir}/top-processes.txt"
  } > "${summary}"
  echo "Resource profile: ${summary}"
  cat "${summary}"
}

maybe_wrap_in_cgroup_scope() {
  if [[ "${LINGON_IT_CGROUP:-1}" == "0" ]]; then
    return 0
  fi
  if [[ "${LINGON_IT_CGROUP_WRAPPED:-0}" == "1" ]]; then
    return 0
  fi
  if ! command -v systemd-run >/dev/null 2>&1; then
    echo "systemd-run is required for contained Android integration tests; set LINGON_IT_CGROUP=0 to bypass deliberately." >&2
    exit 1
  fi

  local cores quota_percent memory_max memory_swap_max unit profile_dir
  cores="$(detect_online_cpu_count)"
  quota_percent="${LINGON_IT_CPU_QUOTA_PERCENT:-$(default_cpu_quota_percent "${cores}")}"
  memory_max="${LINGON_IT_MEMORY_MAX:-7G}"
  memory_swap_max="${LINGON_IT_MEMORY_SWAP_MAX:-0}"
  unit="lingon-android-it-$$"
  profile_dir="${ARTIFACT_OUT}/resource-profile-$(date +%Y%m%d-%H%M%S)-$$"
  mkdir -p "${profile_dir}"

  echo "Running Android integration tests in systemd scope ${unit}.scope"
  echo "Limits: CPUQuota=${quota_percent}% (${cores} online CPUs, capped integration default), MemoryMax=${memory_max}, MemorySwapMax=${memory_swap_max}"
  echo "Resource profile directory: ${profile_dir}"
  export LINGON_IT_CGROUP_UNIT="${unit}.scope"
  export LINGON_IT_CPU_QUOTA_PERCENT_EFFECTIVE="${quota_percent}"
  export LINGON_IT_MEMORY_MAX_EFFECTIVE="${memory_max}"
  export LINGON_IT_MEMORY_SWAP_MAX_EFFECTIVE="${memory_swap_max}"

  set +e
  systemd-run --user --scope --quiet --collect \
    --unit="${unit}" \
    -p CPUAccounting=yes \
    -p "CPUQuota=${quota_percent}%" \
    -p MemoryAccounting=yes \
    -p "MemoryMax=${memory_max}" \
    -p "MemorySwapMax=${memory_swap_max}" \
    -p OOMPolicy=kill \
    env \
      LINGON_IT_CGROUP_WRAPPED=1 \
      LINGON_IT_CGROUP_UNIT="${LINGON_IT_CGROUP_UNIT}" \
      LINGON_IT_CGROUP_PROFILE_DIR="${profile_dir}" \
      LINGON_IT_CPU_QUOTA_PERCENT_EFFECTIVE="${LINGON_IT_CPU_QUOTA_PERCENT_EFFECTIVE}" \
      LINGON_IT_MEMORY_MAX_EFFECTIVE="${LINGON_IT_MEMORY_MAX_EFFECTIVE}" \
      LINGON_IT_MEMORY_SWAP_MAX_EFFECTIVE="${LINGON_IT_MEMORY_SWAP_MAX_EFFECTIVE}" \
      "${BASH}" "${BASH_SOURCE[0]}" "$@"
  local status=$?
  set -e
  summarize_cgroup_profile "${profile_dir}" "${status}"
  exit "${status}"
}

start_cgroup_monitor() {
  if [[ "${LINGON_IT_CGROUP_WRAPPED:-0}" != "1" ]]; then
    return 0
  fi
  local profile_dir="${LINGON_IT_CGROUP_PROFILE_DIR:-}"
  if [[ -z "${profile_dir}" ]]; then
    return 0
  fi
  mkdir -p "${profile_dir}"
  local cgroup_path cgroup_dir
  cgroup_path="$(awk -F: '$1 == "0" {print $3}' /proc/self/cgroup 2>/dev/null || true)"
  cgroup_dir="/sys/fs/cgroup${cgroup_path}"
  if [[ -z "${cgroup_path}" || ! -d "${cgroup_dir}" ]]; then
    echo "Could not locate integration-test cgroup for resource monitoring." >&2
    return 0
  fi

  (
    set +e
    local samples="${profile_dir}/samples.jsonl"
    local top_processes="${profile_dir}/top-processes.txt"
    local interval="${LINGON_IT_CGROUP_SAMPLE_INTERVAL_SEC:-1}"
    local previous_usage previous_time
    previous_usage="$(awk '$1 == "usage_usec" {print $2}' "${cgroup_dir}/cpu.stat" 2>/dev/null)"
    previous_time="$(date +%s%N)"
    while true; do
      sleep "${interval}"
      local now usage user_usec system_usec nr_throttled throttled_usec memory_current memory_peak pids_current
      now="$(date +%s%N)"
      usage="$(awk '$1 == "usage_usec" {print $2}' "${cgroup_dir}/cpu.stat" 2>/dev/null)"
      user_usec="$(awk '$1 == "user_usec" {print $2}' "${cgroup_dir}/cpu.stat" 2>/dev/null)"
      system_usec="$(awk '$1 == "system_usec" {print $2}' "${cgroup_dir}/cpu.stat" 2>/dev/null)"
      nr_throttled="$(awk '$1 == "nr_throttled" {print $2}' "${cgroup_dir}/cpu.stat" 2>/dev/null)"
      throttled_usec="$(awk '$1 == "throttled_usec" {print $2}' "${cgroup_dir}/cpu.stat" 2>/dev/null)"
      memory_current="$(cat "${cgroup_dir}/memory.current" 2>/dev/null)"
      memory_peak="$(cat "${cgroup_dir}/memory.peak" 2>/dev/null)"
      pids_current="$(cat "${cgroup_dir}/pids.current" 2>/dev/null)"
      local cpu_some_avg10="0.00"
      if [[ -r "${cgroup_dir}/cpu.pressure" ]]; then
        cpu_some_avg10="$(awk -F'[ =]' '$1 == "some" {for (i=1; i<=NF; i++) if ($i == "avg10") {print $(i+1); exit}}' "${cgroup_dir}/cpu.pressure")"
      fi
      local cpu_cores
      cpu_cores="$(awk -v usage="${usage:-0}" -v prev_usage="${previous_usage:-0}" -v now="${now}" -v prev_time="${previous_time}" 'BEGIN {
        elapsed=(now - prev_time) / 1000000000
        if (elapsed <= 0) { print "0.00"; exit }
        printf "%.2f", ((usage - prev_usage) / 1000000) / elapsed
      }')"
      local pids="" peak_process_rss_kb="0" peak_process_vsz_kb="0"
      if [[ -r "${cgroup_dir}/cgroup.procs" ]]; then
        pids="$(tr '\n' ',' < "${cgroup_dir}/cgroup.procs" | sed 's/,$//')"
      fi
      if [[ -n "${pids}" ]]; then
        read -r peak_process_rss_kb peak_process_vsz_kb < <(
          ps -o rss=,vsz= -p "${pids}" 2>/dev/null |
            awk '{if ($1 > rss) rss=$1; if ($2 > vsz) vsz=$2} END {printf "%.0f %.0f\n", rss, vsz}'
        )
      fi
      printf '{"ts":%s,"cpu_cores":%s,"cpu_usage_usec":%s,"cpu_user_usec":%s,"cpu_system_usec":%s,"cpu_nr_throttled":%s,"cpu_throttled_usec":%s,"cpu_some_avg10":%s,"memory_current_bytes":%s,"memory_peak_bytes":%s,"pids_current":%s,"peak_process_rss_kb":%s,"peak_process_vsz_kb":%s}\n' \
        "$(date +%s)" \
        "${cpu_cores}" \
        "${usage:-0}" \
        "${user_usec:-0}" \
        "${system_usec:-0}" \
        "${nr_throttled:-0}" \
        "${throttled_usec:-0}" \
        "${cpu_some_avg10:-0.00}" \
        "${memory_current:-0}" \
        "${memory_peak:-0}" \
        "${pids_current:-0}" \
        "${peak_process_rss_kb:-0}" \
        "${peak_process_vsz_kb:-0}" >> "${samples}"
      {
        echo "--- $(date --iso-8601=seconds) ---"
        if [[ -n "${pids}" ]]; then
          ps -o pid=,ppid=,pcpu=,pmem=,rss=,vsz=,comm=,args= -p "${pids}" 2>/dev/null | sort -k3 -nr | head -n 12
        fi
      } >> "${top_processes}"
      previous_usage="${usage:-0}"
      previous_time="${now}"
    done
  ) &
  CGROUP_MONITOR_PID=$!
}

stop_cgroup_monitor() {
  if [[ -n "${CGROUP_MONITOR_PID}" ]]; then
    kill "${CGROUP_MONITOR_PID}" >/dev/null 2>&1 || true
    wait "${CGROUP_MONITOR_PID}" >/dev/null 2>&1 || true
    CGROUP_MONITOR_PID=""
  fi
}

maybe_wrap_in_cgroup_scope "$@"

collect_test_timing_profile() {
  local profile_dir="${LINGON_IT_CGROUP_PROFILE_DIR:-}"
  if [[ -z "${profile_dir}" ]]; then
    return 0
  fi
  local result_dir="${ANDROID_DIR}/app/build/outputs/androidTest-results/connected/debug"
  if [[ ! -d "${result_dir}" ]]; then
    return 0
  fi
  mkdir -p "${profile_dir}/android-test-results"
  find "${result_dir}" -maxdepth 1 -type f -name 'TEST-*.xml' -exec cp {} "${profile_dir}/android-test-results/" \; 2>/dev/null || true
  run_android_tools test-times --dir "${result_dir}" > "${profile_dir}/test-times.txt" 2>"${profile_dir}/test-times.err" || true
}

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

derive_harness_root() {
  local ca_path="$1"
  if [[ -z "${ca_path}" ]]; then
    return 0
  fi
  local root
  root="$(cd "$(dirname "${ca_path}")/../.." >/dev/null 2>&1 && pwd -P || true)"
  if [[ -n "${root}" ]] && [[ "$(basename "${root}")" == lingon-android-harness-* ]]; then
    printf '%s\n' "${root}"
  fi
}

remove_harness_root() {
  local root="$1"
  if [[ -z "${root}" ]]; then
    return 0
  fi
  if [[ "$(basename "${root}")" != lingon-android-harness-* ]]; then
    return 0
  fi
  case "${root}" in
    /tmp/lingon-android-harness-*|/var/tmp/lingon-android-harness-*)
      rm -rf "${root}" || true
      ;;
  esac
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
  if [[ -n "${HARNESS_ROOT:-}" ]]; then
    remove_harness_root "${HARNESS_ROOT}"
  fi
  local root
  for root in "${HARNESS_ROOTS[@]:-}"; do
    remove_harness_root "${root}"
  done
  if [[ "${EMULATOR_STARTED}" == "1" ]] && [[ "${KEEP_EMULATOR}" != "1" ]] && [[ -n "${DEVICE_SERIAL:-}" ]]; then
    "${ADB_BIN}" -s "${DEVICE_SERIAL}" emu kill >/dev/null 2>&1 || true
  fi
  release_lock
  if [[ -n "${CONFIG_PATH:-}" ]]; then
    rm -f "${CONFIG_PATH}" || true
  fi
  stop_cgroup_monitor
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
start_cgroup_monitor
if [[ "${LINGON_IT_CGROUP_WRAPPED:-0}" == "1" && "${KEEP_EMULATOR}" == "1" ]]; then
  echo "LINGON_IT_KEEP_EMULATOR=1 is ignored for contained integration runs; the managed emulator must exit with the cgroup."
  KEEP_EMULATOR="0"
fi

HARNESS_ARGS=()
SPECIAL_TEST="refreshes_sessions_when_host_starts_late"
QUIET_HOST_TEST="tab_switch_does_not_rearm_wall_inactivity_without_terminal_input"
if [[ -z "${HOST_COLS_OVERRIDE}" ]]; then
  HOST_COLS_OVERRIDE="120"
fi
if [[ -z "${HOST_ROWS_OVERRIDE}" ]]; then
  HOST_ROWS_OVERRIDE="50"
fi
if [[ "${HOST_COLS_OVERRIDE}" =~ ^[0-9]+$ ]]; then
  HARNESS_ARGS+=("-cols" "${HOST_COLS_OVERRIDE}")
fi
if [[ "${HOST_ROWS_OVERRIDE}" =~ ^[0-9]+$ ]]; then
  HARNESS_ARGS+=("-rows" "${HOST_ROWS_OVERRIDE}")
fi

run_android_tools() {
  (cd "${ANDROID_DIR}" && go run -buildvcs=true ./cmd/lingon-android-tools "$@")
}

TEST_SRC="${ANDROID_DIR}/app/src/androidTest/java/systems/pkt/lingon/EndToEndTest.kt"
TESTS=()
SELECTED_TESTS=()
ONLY_TEST="${LINGON_IT_ONLY:-}"
if [[ -f "${TEST_SRC}" ]]; then
  mapfile -t TESTS < <(run_android_tools test-names --file "${TEST_SRC}")
fi
if [[ "${#TESTS[@]}" -eq 0 ]]; then
  echo "No tests found in ${TEST_SRC}" >&2
  exit 1
fi
if [[ -n "${ONLY_TEST}" ]]; then
  IFS=',' read -r -a REQUESTED_TESTS <<< "${ONLY_TEST}"
  for requested in "${REQUESTED_TESTS[@]}"; do
    requested="${requested#"${requested%%[![:space:]]*}"}"
    requested="${requested%"${requested##*[![:space:]]}"}"
    if [[ -z "${requested}" ]]; then
      continue
    fi
    matched=false
    for test_name in "${TESTS[@]}"; do
      if [[ "${test_name}" == "${requested}" ]]; then
        SELECTED_TESTS+=("${test_name}")
        matched=true
        break
      fi
    done
    if [[ "${matched}" != true ]]; then
      echo "No Android integration test matched LINGON_IT_ONLY entry: ${requested}" >&2
      exit 1
    fi
  done
  if [[ "${#SELECTED_TESTS[@]}" -eq 0 ]]; then
    echo "No Android integration tests selected by LINGON_IT_ONLY=${ONLY_TEST}" >&2
    exit 1
  fi
else
  SELECTED_TESTS=("${TESTS[@]}")
fi

echo "Building harness..."
(
  cd "${ROOT_DIR}"
  go build -buildvcs=true -o "${HARNESS_BIN}" ./cmd/lingon-android-harness
)

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
  HARNESS_ROOT="$(derive_harness_root "${CA_PATH:-}")"
  if [[ -n "${HARNESS_ROOT}" ]]; then
    HARNESS_ROOTS+=("${HARNESS_ROOT}")
  fi
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
  if [[ -n "${HARNESS_ROOT:-}" ]]; then
    remove_harness_root "${HARNESS_ROOT}"
    HARNESS_ROOT=""
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

DEVICE_SERIAL=""
if [[ "${REUSE_DEVICE}" == "1" ]]; then
  DEVICE_SERIAL="$("${ADB_BIN}" devices | awk 'NR>1 && $2=="device" {print $1; exit}')"
fi
if [[ -z "${DEVICE_SERIAL}" ]]; then
  AVD_NAME="$(resolve_avd_name)"
  if [[ ! -d "${HOME}/.android/avd/${AVD_NAME}.avd" ]]; then
    echo "Creating AVD ${AVD_NAME} (PRESET=${PRESET})..."
    (cd "${ANDROID_DIR}" && make avd PRESET="${PRESET}")
  fi
  AVD_NAME="${AVD_NAME}" \
    AVD_RAM_MB="${LINGON_IT_AVD_RAM_MB:-2048}" \
    AVD_CPU_CORES="${LINGON_IT_AVD_CPU_CORES:-2}" \
    "${ANDROID_DIR}/scripts/configure-avd.sh"
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
    ./gradlew --no-daemon \
      -Dkotlin.compiler.execution.strategy=in-process \
      "-Dorg.gradle.jvmargs=-Xmx1536m -Dfile.encoding=UTF-8" \
      :app:connectedDebugAndroidTest \
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

  for test_name in "${SELECTED_TESTS[@]}"; do
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
collect_test_timing_profile

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
