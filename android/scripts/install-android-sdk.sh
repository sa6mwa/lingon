#!/usr/bin/env bash
set -euo pipefail

if [[ "${OSTYPE:-}" != linux* ]]; then
  echo "This installer currently supports Linux only." >&2
  exit 1
fi

if ! command -v java >/dev/null 2>&1; then
  echo "Java is required (Java 17+ recommended)." >&2
  exit 1
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required." >&2
  exit 1
fi

if ! command -v unzip >/dev/null 2>&1; then
  echo "unzip is required." >&2
  exit 1
fi

ANDROID_SDK_ROOT="${ANDROID_SDK_ROOT:-$HOME/Android/Sdk}"
CMDLINE_TOOLS_VERSION="${CMDLINE_TOOLS_VERSION:-13114758_latest}"
CMDLINE_TOOLS_URL="https://dl.google.com/android/repository/commandlinetools-linux-${CMDLINE_TOOLS_VERSION}.zip"

workdir=$(mktemp -d)
cleanup() { rm -rf "${workdir}"; }
trap cleanup EXIT

mkdir -p "${ANDROID_SDK_ROOT}/cmdline-tools"

curl -fsSL -o "${workdir}/cmdline-tools.zip" "${CMDLINE_TOOLS_URL}"
unzip -q "${workdir}/cmdline-tools.zip" -d "${workdir}/unpacked"

if [[ -d "${ANDROID_SDK_ROOT}/cmdline-tools/latest" ]]; then
  rm -rf "${ANDROID_SDK_ROOT}/cmdline-tools/latest"
fi
mv "${workdir}/unpacked/cmdline-tools" "${ANDROID_SDK_ROOT}/cmdline-tools/latest"

sdkmanager="${ANDROID_SDK_ROOT}/cmdline-tools/latest/bin/sdkmanager"

"${sdkmanager}" --sdk_root="${ANDROID_SDK_ROOT}" --version >/dev/null

set +o pipefail
yes | "${sdkmanager}" --sdk_root="${ANDROID_SDK_ROOT}" \
  "platform-tools" \
  "emulator" \
  "platforms;android-35" \
  "platforms;android-36" \
  "build-tools;35.0.0" \
  "system-images;android-35;default;x86_64"
sdk_status=${PIPESTATUS[1]}
set -o pipefail
if [[ "${sdk_status}" -ne 0 ]]; then
  exit "${sdk_status}"
fi

echo "Android SDK installed at ${ANDROID_SDK_ROOT}"
