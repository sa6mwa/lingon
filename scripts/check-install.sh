#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
mkdir -p "${root}/build/verification"
work="$(mktemp -d "${root}/build/verification/install.XXXXXX")"
trap 'rm -rf "${work}"' EXIT

env -u DBUS_SESSION_BUS_ADDRESS -u XDG_RUNTIME_DIR \
  make --no-print-directory -C "${root}" build BIN="${work}/lingon" CGO_ENABLED=0 \
    VERIFY="${work}/verification-unavailable"

# Installation must work in sudo's stripped environment, without a Go compiler
# or access to a user session bus. Install only into this check's owned prefix.
env -u DBUS_SESSION_BUS_ADDRESS -u XDG_RUNTIME_DIR \
  make --no-print-directory -C "${root}" install \
    BIN="${work}/lingon" PREFIX="${work}/install prefix" \
    BINDIR="${work}/install prefix/bin" \
    GO="${work}/go-unavailable" VERIFY="${work}/verification-unavailable"

cmp "${work}/lingon" "${work}/install prefix/bin/lingon"
test "$(readlink "${work}/install prefix/bin/lingonx")" = lingon
"${work}/install prefix/bin/lingonx" version --semver
