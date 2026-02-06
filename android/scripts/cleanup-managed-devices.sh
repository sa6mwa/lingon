#!/usr/bin/env bash
set -euo pipefail

pattern='GradleManagedDevice'

if ! command -v pgrep >/dev/null 2>&1; then
  echo "pgrep not found; cannot clean Gradle managed devices." >&2
  exit 1
fi

pids="$(pgrep -f "${pattern}" || true)"
if [[ -z "${pids}" ]]; then
  echo "No Gradle managed device processes found."
  exit 0
fi

echo "Stopping Gradle managed device processes:"
echo "${pids}" | tr ' ' '\n' | sed 's/^/  /'

kill ${pids} >/dev/null 2>&1 || true
sleep 2

remaining="$(pgrep -f "${pattern}" || true)"
if [[ -n "${remaining}" ]]; then
  echo "Force killing remaining processes:"
  echo "${remaining}" | tr ' ' '\n' | sed 's/^/  /'
  kill -9 ${remaining} >/dev/null 2>&1 || true
fi
