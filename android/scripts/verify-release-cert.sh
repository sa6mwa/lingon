#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

APK_PATH="${1:-}"
EXPECTED_CERT_PATH="${EXPECTED_CERT_PATH:-${ROOT_DIR}/lingon-release-cert.pem}"
APKSIGNER_BIN="${APKSIGNER_BIN:-}"

if [[ -z "${APK_PATH}" ]]; then
  echo "usage: $0 /absolute/path/to/app-release.apk" >&2
  exit 1
fi

if [[ ! -f "${APK_PATH}" ]]; then
  echo "APK not found: ${APK_PATH}" >&2
  exit 1
fi

if [[ ! -f "${EXPECTED_CERT_PATH}" ]]; then
  echo "Expected release certificate not found: ${EXPECTED_CERT_PATH}" >&2
  exit 1
fi

if [[ -z "${APKSIGNER_BIN}" ]]; then
  if ! APKSIGNER_BIN="$(command -v apksigner 2>/dev/null)"; then
    echo "apksigner not found. Install Android build-tools or set APKSIGNER_BIN." >&2
    exit 1
  fi
fi

normalize_fingerprint() {
  printf '%s' "$1" | tr '[:lower:]' '[:upper:]' | tr -d ': \r\n'
}

expected_fp="$(
  openssl x509 -in "${EXPECTED_CERT_PATH}" -noout -fingerprint -sha256 |
    sed -n 's/^sha256 Fingerprint=//p'
)"

if [[ -z "${expected_fp}" ]]; then
  echo "Failed to read SHA-256 fingerprint from ${EXPECTED_CERT_PATH}" >&2
  exit 1
fi

apk_verify_output="$("${APKSIGNER_BIN}" verify --verbose --print-certs "${APK_PATH}")"

apk_fp="$(
  printf '%s\n' "${apk_verify_output}" |
    sed -n 's/^Signer #[0-9][0-9]* certificate SHA-256 digest: //p' |
    head -n 1
)"

if [[ -z "${apk_fp}" ]]; then
  echo "Failed to read signer certificate fingerprint from APK: ${APK_PATH}" >&2
  printf '%s\n' "${apk_verify_output}" >&2
  exit 1
fi

if [[ "$(normalize_fingerprint "${apk_fp}")" != "$(normalize_fingerprint "${expected_fp}")" ]]; then
  echo "Release APK signer certificate does not match ${EXPECTED_CERT_PATH}" >&2
  echo "Expected SHA-256: ${expected_fp}" >&2
  echo "Actual SHA-256:   ${apk_fp}" >&2
  exit 1
fi

echo "Verified release APK signer certificate matches ${EXPECTED_CERT_PATH}"
