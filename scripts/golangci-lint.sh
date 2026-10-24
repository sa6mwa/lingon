#!/usr/bin/env bash
set -euo pipefail

# Build the pinned linter with the active Go toolchain, rather than reusing a
# system binary that may not understand that toolchain's export-data format.
exec env GOTOOLCHAIN=local "${GO:-go}" run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 "$@"
