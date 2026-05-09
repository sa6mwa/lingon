# AGENTS.md

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

- Do not run full Android/emulator/instrumentation sweeps in parallel with other
  heavy suites.
- Do not leave emulators, adb servers, Gradle daemons, harnesses, or test
  servers running after verification unless the engineer explicitly asks for
  them to stay up.
- If a suite can monopolize CPU, RAM, GPU, or the desktop session, state that
  before starting it and run it serially.

You are collaborating with a highly opinionated Go architect. Optimize for Go-idiomatic design, separation of concerns, and developer experience (DX). Do NOT jump into implementation: propose options + tradeoffs first.

## Workflow (mandatory)
1. Restate goal + constraints.
2. Propose 1–3 designs with tradeoffs.
3. Get alignment before writing significant code.
4. Implement in small, reviewable steps.
5. Run quality gates (see below).
6. Git commit messages must follow Conventional Commits: https://www.conventionalcommits.org/en/v1.0.0/

## Refactors (strong preference)
- Avoid feature flags for refactoring tasks.
- Prefer clean refactors with a clear cutover:
  - No lingering “legacy” implementations, structs, or parallel codepaths.
  - Remove dead code and migrate call sites in the same change-set/sequence.
- Only keep parallel implementations or “legacy” structures if explicitly requested.

## Architecture & packaging
- Separation starts at the package boundary:
  - Public API packages for exported surfaces.
  - `internal/...` for non-exported implementation details.
- If there are two or more variants/adapters of an implementation: **use an interface**.
- Constructors:
  - Provide a `New...` constructor for implementations.
  - **Constructors must return the interface type, never a concrete type.**
- Cyclic imports:
  - If cyclic imports occur, extract core application functionality into a `core` package or subpackage
    so both main/module code and subpackages can import it without cycles.

## Public API shape (strong preference)
- Inputs:
  - If a user-facing function or interface method takes more than 4 parameters total (including `ctx context.Context`),
    do not bloat the signature. Put the non-`ctx` inputs into a request/input struct instead (e.g. `FooRequest`).
- Outputs:
  - A user-facing function or interface method must return **no more than two values**: `(T, error)` (or `(Response, error)`).
  - If more than one non-error value needs to be returned, the first return value must be a **response/result struct**
    that carries all outputs (e.g. `FooResponse`), and the second return value is `error`.

## Documentation & generators
- Every package must have a `doc.go` with standard Go package comment documentation.
- Code generation:
  - If generators are not tightly bound to a single package: put `generate.go` at the top-level module folder.
  - If tightly bound to a single package: put `generate.go` in that package folder (and place any generator runner `main` packages underneath as appropriate).

## Quality gates (always run before “done”)
- `go test ./...`
- `go vet ./...`
- `golint ./...`
- `golangci-lint run ./...`
- All test failures must be addressed before the task is considered done, even if a failure appears unrelated to the current change.
- Do not leave the repo with known failing tests and describe them as “unrelated”; fix them in the same task or explicitly stop and escalate if they cannot be resolved safely.

## Android verification policy (mandatory)
- Do not run the full connected Android instrumentation suite on every change by default.
- Use a tiered verification policy:
  - Always run `./gradlew :app:testDebugUnitTest` for Android-touching changes.
  - Always run `./gradlew :app:compileDebugAndroidTestKotlin` for Android-touching changes so `src/androidTest` stays buildable.
  - Run targeted `:app:connectedDebugAndroidTest` cases for the specific Android-visible behavior that changed:
    - UI/layout/render/pan/zoom
    - notifications
    - lifecycle/reconnect/background behavior
    - relay/app/session interactions visible in the app
  - Run the full Android integration sweep before release, and after broad terminal/render/session/relay changes that can affect multiple Android-visible behaviors at once.
- An Android-visible bug is not considered fixed without a passing targeted instrumentation or end-to-end verification for that exact behavior.

## Repo hygiene
- If `.golangci.yml` does not exist in repo root, create and seed it with the contents below.

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
  - Row-1 base repaint followed by row-1 tab overlay repaint as separate visible phases.
- Regression tests enforcing this invariant are permanent and must not be removed or weakened.
- Do not remove or soften these tests. Ask the developer three times before touching this.

## Regression discipline (mandatory)
- Every user-reported bug must be converted into a failing regression test before the fix is merged.
- For TUI/attach/headless flows, regressions must be real PTY integration tests (`ptytest`) that drive actual keypresses and assert observable screen/state outcomes.
- For Android app behavior, prefer instrumentation or end-to-end verification when the bug is visible at the UI, notification, lifecycle, or relay boundary.
- For timing-sensitive behavior, tests must use injected `clock.Clock` and explicitly advance mock time; do not rely on wall-clock sleeps or polling loops as proof of correctness.
- A bug is not marked resolved until:
  - the new regression test fails before the fix,
  - passes after the fix,
  - and relevant existing suites still pass.
- If a report contains multiple failure modes, add one explicit assertion per failure mode (or separate tests) so coverage is auditable.

## Terminal isolation (mandatory, non-negotiable)
- Tests, harnesses, and ad hoc verification commands must never mutate the inherited terminal/tty/pty of the shell, tmux client, or Lingon session they were launched from.
- Any test or helper that needs raw mode, resize events, or PTY control must create and use its own dedicated PTY/TTY, fully isolated from the user's terminal.
- Process-level window-change signaling is prohibited in tests and test helpers:
  - do not send `SIGWINCH` to the process, process group, parent process, or inherited tty owner
  - do not rely on process-global `signal.Notify(..., SIGWINCH)` as a test stimulus
- If resize behavior must be tested, drive it only through PTY-local mechanisms owned by the test, such as resizing the dedicated PTY created for that test.
- Do not use `/dev/tty`, inherited stdin/stdout terminals, or shell wrappers that alter the caller terminal state as part of test setup.
- When any illicit inherited-tty or process-level resize behavior is discovered in the test codebase, remove or refactor it immediately; do not preserve it as legacy coverage.

## Bug tracking and post-fix verification (mandatory)
- Track active user-reported bugs in [BUG_TRACKER.md](BUG_TRACKER.md).
- Add or update the tracker entry before or during investigation; do not rely on thread memory alone.
- A bug fix is not considered trustworthy just because code was written.
- After implementing a fix, perform an explicit review of the changed code paths and confirm they actually enforce the intended behavior.
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
