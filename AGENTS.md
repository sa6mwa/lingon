# AGENTS.md

This file is a repository-specific supplement for Lingon. Keep it limited to
Lingon-specific constraints.

## Absolute first rule: terminal isolation

Never run or write tests, harnesses, scripts, or ad hoc verification that can
touch, reconfigure, resize, signal, detach, clear, or otherwise mutate the
inherited terminal/tty/pty of the shell, tmux client, or Lingon session that
launched the command.

This is a hard stop, not a preference:

- Do not use `/dev/tty` in tests or test helpers.
- Do not put the caller's `os.Stdin`, `os.Stdout`, or `os.Stderr` into raw mode.
- Do not send terminal escape sequences, tmux control sequences, Ctrl+B
  sequences, Ctrl+L control paths, or resize/signal events to the inherited
  terminal.
- Do not send `SIGWINCH` to the process, process group, parent process, or
  inherited tty owner.
- Bare PTY/localSession usage in tests is prohibited unless the test explicitly
  creates a child PTY/TTY pair isolated from the runner terminal.
- TUI, attach, headless, host, and terminal regression tests must use `ptytest`
  or an equivalent reviewed harness that owns its sub-PTYs. Do not bypass this
  with direct `newLocalSession` tests for interactive behavior.
- Do not run real user-installed interactive programs such as `codex`, `tmux`,
  shells, or terminal apps in automated tests unless they are fully contained
  inside a dedicated test-owned sub-PTY with no inherited stdio access.
- Any PTY, raw mode, resize, or terminal-control test must allocate a dedicated
  isolated PTY/TTY owned by that test and must restore/close it in cleanup.
- If a test cannot prove that all terminal control is isolated from the caller,
  do not run it and do not merge it.

Agent-driven verification must also avoid surprise resource exhaustion:

- Verification must use `bash scripts/verify-contained.sh <command> [args...]`
  or a Make target that uses it. The entire verification stack shares a hard
  1 CPU / 3 GiB RAM ceiling with zero swap; do not bypass or raise these limits.

- Do not run full Android/emulator/instrumentation sweeps in parallel with other
  heavy suites.
- Do not leave emulators, adb servers, Gradle daemons, harnesses, or test
  servers running after verification unless the engineer explicitly asks for
  them to stay up.
- If a suite can monopolize CPU, RAM, GPU, or the desktop session, state that
  before starting it and run it serially.

## Command execution discipline

- Do not run shell commands that execute code, mutate state, start processes,
  run tests, run builds, run linters, start servers, start emulators, use adb,
  use Gradle, or perform other operational work in parallel.
- Test suites, build commands, linters, package managers, dev servers, emulator
  runs, Android instrumentation, database/service processes, and other
  long-running or resource-heavy commands must be strictly serialized: start
  one, wait for it to finish, inspect the result, then decide the next command.
- Parallel read-only file inspection is allowed. It is fine to read multiple
  files or run read-only discovery commands such as `rg`, `sed`, `ls`,
  `git show`, `nl`, `wc`, and similar commands in parallel when they cannot
  mutate state or start substantial background work.
- Parallel web searches are allowed when useful.
- If there is any doubt whether a command is read-only and lightweight, run it
  serially.

## Lingon engineering standards

- Optimize for Go-idiomatic design, separation of concerns, and developer
  experience.
- Keep package boundaries clean:
  - Public API packages expose stable user-facing surfaces.
  - `internal/...` packages hold non-exported implementation details.
- If there are two or more variants/adapters of an implementation, use an
  interface.
- Constructors for implementations should be named `New...`; when a constructor
  exists specifically to provide an implementation behind an interface, return
  the interface type.
- If cyclic imports occur, extract core application functionality into a `core`
  package or subpackage so both main/module code and subpackages can import it
  without cycles.

## Public API shape

- If a user-facing function or interface method takes more than 4 parameters
  total, including `ctx context.Context`, put the non-`ctx` inputs into a
  request/input struct.
- A user-facing function or interface method should return no more than two
  values: `(T, error)` or `(Response, error)`.
- If more than one non-error value needs to be returned, use a response/result
  struct for the first return value and `error` for the second.

## Documentation and generators

- Every package must have a `doc.go` with standard Go package comment
  documentation.
- If generators are not tightly bound to a single package, put `generate.go` at
  the top-level module folder.
- If a generator is tightly bound to a single package, put `generate.go` in that
  package folder and place any generator runner `main` packages underneath as
  appropriate.

## Quality gates

Run the project standard checks before declaring code changes complete:

- `go test ./...`
- `go vet ./...`
- `golint ./...`
- `golangci-lint run ./...`

All test failures must be addressed before the task is considered done, even if
a failure appears unrelated to the current change. If a failure cannot be
resolved safely in the current production envelope, stop and escalate with the
evidence.

For documentation, policy, configuration, or other non-executable changes, run
the strongest applicable verification: readback, syntax validation, rendered
output, config validation, or another check that can falsify mistakes.

## Android verification policy

Do not run the full connected Android instrumentation suite on every change by
default.

Use this tiered policy for Android-touching changes:

- Always run `./gradlew :app:testDebugUnitTest`.
- Always run `./gradlew :app:compileDebugAndroidTestKotlin` so
  `src/androidTest` stays buildable.
- Run targeted `:app:connectedDebugAndroidTest` cases for the specific
  Android-visible behavior that changed:
  - UI/layout/render/pan/zoom
  - notifications
  - lifecycle/reconnect/background behavior
  - relay/app/session interactions visible in the app
- Run the full Android integration sweep before release, and after broad
  terminal/render/session/relay changes that can affect multiple
  Android-visible behaviors at once.

An Android-visible bug is not considered fixed without a passing targeted
instrumentation or end-to-end verification for that exact behavior.

## Local CI and release lifecycle

- [RELEASE.md](RELEASE.md) defines the repository-owned Go lifecycle;
  `make help` is the command index. This is local CI, not GitHub Actions.
- `make prerelease` / `make test-all` run the full serial proof and artifact
  rehearsal. Root and Android-tool Go tests, race tests, PTY/browser integration,
  Android unit/compile gates and all instrumentation classes are required.
  Narrow checks remain available during ordinary development.
- Release targets are Linux, FreeBSD and Darwin, each on amd64 and arm64, plus
  the signed APK. CGO is disabled for every shipped Go binary. Linux/BSD must be
  static; Darwin may depend only on system libraries. Runtime test coverage is
  Linux/Android; other binaries are cross-built and inspected.
- Commit candidate code/docs and resolve review findings before release. A
  clean candidate must contain current `trunk`, which must match `origin/trunk`.
- `make release VERSION=X.Y.Z RELEASE_MESSAGE='type(scope): change'` runs the
  complete candidate gate before squashing to trunk, preserves the source
  branch, creates a lightweight `vX.Y.Z` tag, then cleans/retests/rebuilds and
  verifies final artifacts from that exact tagged commit. Select actual release
  versions explicitly; do not treat examples as a version decision.
- Release gates are stop-only. On failure, report the failing gate and local
  branch/tag/artifact state. Do not fix, amend, reset, retag, retry correctness
  gates or publish inside that release attempt; start a separate fix iteration.
- Final checksums are the upload manifest. Missing/extra artifacts, wrong
  identity/architecture/linking, private paths and APK signer/version failures
  block publication. Build scratch/logs belong under ignored `build/`; final
  artifacts belong under ignored `dist/`.
- `make publish`, when explicitly authorized, consumes verified full-proof
  artifacts, checks GitHub immutability, pushes trunk/tag atomically, verifies
  GitHub draft asset bytes, publishes, then pushes the versioned Podman manifest
  to `docker.io/pktsystems/lingon` as the last step.
- Assume existing GitHub/Docker Hub login. A final registry push failure leaves
  Git/GitHub published; `make push-container` retries only that step with the
  original verified image. Never rebuild or overwrite released bytes/tags.
  Once pushed, release identity is immutable; corrections roll forward.
- Podman is the sole engine. Local services use `podman kube play/down` and
  `devenv.yaml.in`, with all project-owned state under `build/devenv/` and
  unprivileged cleanup after teardown. Do not reintroduce containerd/Docker
  fallbacks, live home-directory mounts or surprise persistent test services.
- Android release version name/code are supplied as Gradle properties; release
  builds must not rewrite tracked metadata. Preserve intentional pre-existing
  `android/app/build.gradle.kts` edits rather than reverting them.

## Repo hygiene

If `.golangci.yml` does not exist in repo root, create and seed it with:

```yaml
version: "2"
linters:
  disable:
    - errcheck
  exclusions:
    rules:
      # staticcheck style nits we don't want to chase
      - linters: [staticcheck]
        text: "QF1003"
      - linters: [staticcheck]
        text: "S1017"
      - linters: [staticcheck]
        text: "QF1001"
      - linters: [staticcheck]
        text: "S1009"
```

## Non-negotiable UI invariant

- Tab switching in host TUI and attach TUI MUST NOT flicker.
- Specifically forbidden:
  - Full-screen clear/redraw on tab switch.
  - Row-1 base repaint followed by row-1 tab overlay repaint as separate
    visible phases.
- Regression tests enforcing this invariant are permanent and must not be
  removed or weakened.
- Do not remove or soften these tests. Ask the engineer three times before
  touching this.

## Regression discipline

- Every user-reported bug must be converted into a failing regression test
  before the fix is merged.
- For TUI/attach/headless flows, regressions must be real PTY integration tests
  (`ptytest`) that drive actual keypresses and assert observable screen/state
  outcomes.
- For Android app behavior, prefer instrumentation or end-to-end verification
  when the bug is visible at the UI, notification, lifecycle, or relay boundary.
- For timing-sensitive behavior, tests must use injected `clock.Clock` and
  explicitly advance mock time; do not rely on wall-clock sleeps or polling loops
  as proof of correctness.
- A bug is not marked resolved until:
  - the new regression test fails before the fix,
  - passes after the fix,
  - and relevant existing suites still pass.
- If a report contains multiple failure modes, add one explicit assertion per
  failure mode, or separate tests, so coverage is auditable.

## Bug tracking and post-fix verification

- Track active user-reported bugs in [BUG_TRACKER.md](BUG_TRACKER.md).
- Add or update the tracker entry before or during investigation; do not rely on
  thread memory alone.
- A bug fix is not considered trustworthy just because code was written.
- After implementing a fix, perform an explicit review of the changed code paths
  and confirm they actually enforce the intended behavior.
- Record verification evidence in the tracker entry:
  - concrete repro steps,
  - regression coverage added,
  - tests or end-to-end checks run,
  - any remaining gaps or blocked verification.
- Use these tracker statuses consistently:
  - `open`
  - `in_progress`
  - `needs_verification`
  - `resolved`
  - `blocked`
