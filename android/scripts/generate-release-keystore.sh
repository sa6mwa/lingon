#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

SIGNING_PROPERTIES="${SIGNING_PROPERTIES:-${ROOT_DIR}/signing.properties}"
KEYSTORE_DIR="${KEYSTORE_DIR:-${ROOT_DIR}/.keystore}"
KEYSTORE_PATH="${KEYSTORE_PATH:-${KEYSTORE_DIR}/lingon-release.jks}"
KEY_ALIAS="${KEY_ALIAS:-lingon}"
KEYSTORE_PASSWORD="${KEYSTORE_PASSWORD:-lingon-dev}"
KEY_PASSWORD="${KEY_PASSWORD:-${KEYSTORE_PASSWORD}}"
KEYSTORE_DNAME="${KEYSTORE_DNAME:-CN=Lingon, OU=Dev, O=Lingon, L=Local, S=Local, C=US}"

if [[ -f "${SIGNING_PROPERTIES}" ]]; then
  echo "signing.properties already exists; leaving it unchanged."
  exit 0
fi

if ! command -v keytool >/dev/null 2>&1; then
  echo "keytool not found (install a JDK to generate a release keystore)." >&2
  exit 1
fi

mkdir -p "${KEYSTORE_DIR}"

if [[ ! -f "${KEYSTORE_PATH}" ]]; then
  keytool -genkeypair -v \
    -keystore "${KEYSTORE_PATH}" \
    -alias "${KEY_ALIAS}" \
    -storepass "${KEYSTORE_PASSWORD}" \
    -keypass "${KEY_PASSWORD}" \
    -keyalg RSA \
    -keysize 2048 \
    -validity 10000 \
    -dname "${KEYSTORE_DNAME}"
fi

cat > "${SIGNING_PROPERTIES}" <<EOF
storeFile=${KEYSTORE_PATH}
storePassword=${KEYSTORE_PASSWORD}
keyAlias=${KEY_ALIAS}
keyPassword=${KEY_PASSWORD}
EOF

echo "Generated signing.properties at ${SIGNING_PROPERTIES} (local dev key)."
