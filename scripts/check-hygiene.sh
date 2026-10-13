#!/usr/bin/env bash
set -euo pipefail

git rev-parse --show-toplevel >/dev/null
failed=0
reject() {
  printf 'Repository hygiene: %s\n' "$1" >&2
  failed=1
}

# --no-index checks ignore policy even when someone force-added an output.
for path in build/review/result.json build/verification/BUG_TRACKER.md \
  build/lifecycle/run.log dist/release.tar.gz android/build/report.json \
  android/app/build/report.xml android/test-artifacts/report.xml \
  BUG_TRACKER.md .codex-review-last.json codex-review.result.json review-result.md; do
  if ! git check-ignore --no-index -q -- "$path"; then
    reject "$path must remain ignored; restore the output-directory rules."
  fi
done

# Inspect the index, including ignored paths added with --force. NUL delimiters
# preserve filenames with spaces or newlines. Also reject nonignored scratch.
if ! git ls-files --cached --others --exclude-standard -z | (
  failed=0
  while IFS= read -r -d '' path; do
    case "$path" in
      build/*|dist/*|android/build/*|android/app/build/*|android/test-artifacts/*|*.log|BUG_TRACKER.md|.codex-review*|codex-review.*|review-*)
        reject "$path is generated output; remove it from Git and keep evidence under build/."
        ;;
    esac
  done
  exit "$failed"
); then
  reject "Git index contains generated output or could not be inspected."
fi

# Ignored root files would otherwise escape both Git status and the index scan.
shopt -s nullglob dotglob
for path in *.log .codex-review* codex-review.* review-* BUG_TRACKER.md; do
  if [[ -f "$path" || -L "$path" ]]; then
    reject "$path clutters the repository root; move it under build/review/ or build/verification/."
  fi
done

exit "$failed"
