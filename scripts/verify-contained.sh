#!/usr/bin/env bash
set -euo pipefail

# All scopes share one aggregate budget, including nested Android runs and
# Podman children. Per-command limits alone would allow the stack to multiply it.
SLICE=lingonverification.slice
if [[ "${1:-}" == --check ]]; then
  path="$(awk -F: '$1 == "0" {print $3}' /proc/self/cgroup)"
  parent="${path%%/${SLICE}/*}/${SLICE}"
  if [[ "${path}" != *"/${SLICE}/"* ]]; then
    echo "Verification requires scripts/verify-contained.sh (4 CPUs, 4 GiB, no swap)." >&2
    exit 1
  fi
  directory="/sys/fs/cgroup${parent}"
  read -r quota period < "${directory}/cpu.max"
  read -r memory < "${directory}/memory.max"
  read -r swap < "${directory}/memory.swap.max"
  if [[ ! "${quota}" =~ ^[0-9]+$ || ! "${period}" =~ ^[0-9]+$ ||
        ! "${memory}" =~ ^[0-9]+$ || "${quota}" -gt "$((period * 4))" ||
        "${period}" -eq 0 || "${memory}" -gt 4294967296 || "${swap}" != 0 ]]; then
    echo "Verification containment exceeds 4 CPUs / 4 GiB / zero swap; stopping." >&2
    exit 1
  fi
  exit 0
fi
if [[ "$#" -eq 0 ]]; then
  echo "usage: scripts/verify-contained.sh <command> [arguments...]" >&2
  exit 1
fi
verification_java_options="${JAVA_TOOL_OPTIONS:-}"
if [[ "${verification_java_options}" != *"-XX:ActiveProcessorCount=4 -Xmx256m" ]]; then
  verification_java_options="${verification_java_options} -XX:ActiveProcessorCount=4 -Xmx256m"
fi
# Reuse proven containment so Android children remain in the lifecycle scope's
# OOM group and cancellation/cleanup cannot strand a sibling scope.
if bash "${BASH_SOURCE[0]}" --check >/dev/null 2>&1; then
  exec env -u LINGON_VERIFY_UNIT GOMAXPROCS=4 GOFLAGS=-p=1 \
    JAVA_TOOL_OPTIONS="${verification_java_options}" "$@"
fi
unit="${LINGON_VERIFY_UNIT:-lingon-verify-$$-${RANDOM}}"
systemctl --user set-property --runtime "${SLICE}" \
  CPUAccounting=yes CPUQuota=400% MemoryAccounting=yes MemoryMax=4G MemorySwapMax=0
exec systemd-run --user --scope --quiet --collect --unit="${unit}" --slice="${SLICE}" \
  -p CPUQuota=400% -p MemoryMax=4G -p MemorySwapMax=0 -p OOMPolicy=kill \
  env -u LINGON_VERIFY_UNIT GOMAXPROCS=4 GOFLAGS=-p=1 \
    JAVA_TOOL_OPTIONS="${verification_java_options}" "$@"
