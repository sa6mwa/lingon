# Bug Tracker

This file is the working ledger for user-reported bugs and feature regressions uncovered during iterative development and testing.

## Verification Rules

Do not treat a change as fixed just because code was written.

For every reported bug or requested behavior change:

1. Record the report here before or while investigating.
2. Capture a concrete repro path.
3. Add a regression test at the strongest reasonable layer.
   For Android app behavior, prefer instrumentation or end-to-end coverage when practical.
   For TUI, attach, host, and PTY behavior, prefer PTY integration tests.
4. Implement the fix.
5. Review the actual changed code paths after implementation instead of trusting intent.
6. Run the relevant verification:
   Unit tests, integration tests, lint, and end-to-end tests as appropriate.
7. Mark the item resolved only after the repro is no longer reproducible and the regression passes.

Required status values:

- `open`: reported or reproduced, not fixed
- `in_progress`: actively being investigated or implemented
- `needs_verification`: code changed, but proof is incomplete
- `resolved`: repro closed and verification recorded
- `blocked`: cannot yet verify because of an external dependency or missing environment

## Active Items

### B-001 Android non-headless host resize leak

- Status: `resolved`
- Area: `android`, `relay`, `session`
- Summary: Android must not be able to resize a normal host session at all. Only headless Lingon-owned PTYs may accept remote resize.
- Report:
  Android session switching or viewport changes were still resizing a real local host session and propagating into the tmux terminal running Lingon, even with `resize host` disabled.
- Repro:
  1. Run a normal non-headless Lingon host in a local terminal.
  2. Connect from Android with control.
  3. Switch sessions or otherwise trigger Android-side size updates.
  4. Observe local host PTY and outer terminal resizing.
- Regression coverage:
  - `internal/session.TestAttachControlResizeDoesNotResizeNonHeadlessHostPTY`
  - Android unit coverage ensuring app-side resize frames are not sent
  - Connected Android instrumentation coverage partially rerun
- Implementation notes:
  - Android app resize sending path removed/disabled
  - Non-headless host `publisher.OnResize` now ignores remote resize
- Verification:
  - `go test ./...`
  - `go vet ./...`
  - `golint ./...`
  - `golangci-lint run ./...`
  - `make test-webui`
  - `./gradlew :app:testDebugUnitTest`
  - Connected Android instrumentation passed on emulator for:
    - `resize_setting_default_off_does_not_resize_host`
    - `resize_menu_absent_and_input_remains_blocked_without_control`
    - `resize_setting_override_still_does_not_resize_host_when_in_control`
    - `resize_setting_disabled_for_view_only_share_token`
- Notes:
  - One stale instrumentation assertion originally expected input to remain enabled after forcing `hasControl = false`; that test was corrected to reflect the real control model before final verification.

### B-002 Android wall notification source/title format missing

- Status: `resolved`
- Area: `android`, `notifications`
- Summary: Visible Android wall notifications should show the requested source format `alice@10.0.0.1#sessionname`.
- Report:
  Installed Android app notification does not show the expected source label format even though this was previously claimed fixed.
- Repro:
  1. Trigger a wall/background wall notification in the Android app.
  2. Inspect the posted system notification.
  3. Observe that the visible notification does not show `user@endpoint#session`.
- Regression coverage:
  - Existing helper-only unit test is insufficient because it validates formatter helpers, not the posted notification payload.
- Investigation notes:
  - `AndroidWallNotifier` still uses the generic title `"Broadcast"`.
  - The formatter helper already produces `sender#session`, but that value is only placed in notification content text.
- Required fix:
  - Add a regression at the actual notification payload boundary.
  - Make the visible notification surface the source label.
- Verification:
  - `./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.notifications.AndroidWallNotifierTest`
  - Connected Android instrumentation passed on emulator for:
    - `background_wall_delivery_posts_system_notification`
- Notes:
  - The bug was in the posted notification payload, not the formatter helper.
  - `AndroidWallNotifier` now uses the formatted source as the visible title and the wall message as the body.

### B-003 Local PTY anti-cropping preservation still broken

- Status: `resolved`
- Area: `session`, `scrollback`, `render`
- Summary: Shrinking the host viewport must not destroy right-side content or garble preserved history.
- Report:
  After shrinking a terminal hosting a local PTY, right-side content is still destroyed. Scrollback can show garbled post-crop output.
- Repro:
  1. Produce wide content in a local host PTY.
  2. Shrink the viewport.
  3. Emit additional narrow-width output after the shrink so the old wide row scrolls into history.
  4. Re-expand or inspect scrollback.
  5. Observe lost right-side content and/or garbled history.
- Regression coverage:
  - `internal/session.TestHostResizePreservesWideContentInScrollbackAfterPostShrinkOutput`
  - Surrounding preservation coverage reverified:
    - `TestHostResizePreservesWideContentAcrossShrinkAndExpand`
    - `TestHostResizePreservesWideContentInScrollbackWhileViewportIsNarrow`
    - `TestHostResizePreservesLowerViewportContentAcrossShrinkAndExpand`
    - `TestHostResizePreservesScrollbackHistory`
    - `TestHostScrollbackResizeRepaintsIndicatorWithoutInput`
- Investigation notes:
  - The previous tests only covered quiet shrink/expand cases and missed the destructive case where new post-shrink output pushed preserved rows into scrollback.
  - The fix uses a shrink-time preserved-row queue for local sessions, so post-shrink scrollback drains substitute the pre-shrink row content only when needed, while normal scrollback remains emulator-native.
- Verification:
  - `go test -count=1 ./internal/session -run 'TestHostResizePreservesWideContentInScrollbackAfterPostShrinkOutput'`
  - `go test -count=1 ./internal/session -run 'TestHostResizePreservesWideContentAcrossShrinkAndExpand|TestHostResizePreservesWideContentInScrollbackWhileViewportIsNarrow|TestHostResizePreservesWideContentInScrollbackAfterPostShrinkOutput|TestHostResizePreservesLowerViewportContentAcrossShrinkAndExpand|TestHostResizePreservesScrollbackHistory|TestHostScrollbackResizeRepaintsIndicatorWithoutInput'`
  - `go test ./...`
  - `go vet ./...`
  - `golint ./...`
  - `golangci-lint run ./...`
  - `make test-webui`

### B-004 Test and harness terminal isolation

- Status: `resolved`
- Area: `tests`, `android`, `pty`
- Summary: Tests and harnesses must operate only on their own PTYs and must never mutate the inherited terminal running Lingon/tmux.
- Report:
  Previous test and harness runs resized or reconfigured the terminal session running the tests.
- Repro:
  Historical; not yet pinned to one remaining deterministic path.
- Regression coverage:
  - `cmd/lingon-android-harness.TestWriteHostScriptDoesNotTouchCallerTTY`
  - `internal/attach.TestSubscribeResizeSignalsDisabled`
  - `internal/attach.TestSubscribeResizeSignalsEnabled`
  - `internal/session.TestSubscribeResizeSignalsDisabled`
  - `internal/session.TestSubscribeResizeSignalsEnabled`
- Verification:
  - Reviewed the remaining resize-driving paths and removed process-global `SIGWINCH` handling from PTY-harnessed attach, multi-attach, and host session runs by wiring explicit `DisableSignalResize` boundaries through:
    - `internal/attach/client.go`
    - `internal/attach/multi.go`
    - `internal/session/session.go`
    - `internal/ptytest/harness.go`
  - Audited direct test-owned attach clients in:
    - `internal/session/*`
    - `internal/headlessd/*`
    and disabled signal-based resize there as well.
  - `go test -count=1 ./internal/attach ./internal/session`
  - `go test -count=1 ./internal/headlessd`
  - `go test ./...`
 - Notes:
  - The remaining leak was not the Android harness wrapper anymore; it was the runtime attach/session packages still subscribing to process-global `SIGWINCH` even when tests already injected PTY-local resize events.
  - The full-suite red state reproduced only when `internal/attach` and `internal/session` ran concurrently, which closed after the host-side isolation patch landed.

## Recently Resolved Or Reverified

### B-005 Android reconnect syncing indicator missing while reconnect is pending

- Status: `resolved`
- Area: `android`, `viewmodel`
- Summary: App should continue showing syncing while reconnect is still pending.
- Verification:
  - Android unit tests
  - full Android/unit/webui/go quality gates during prior fix pass

### B-006 Burst Enter prompt loss in local and attach PTYs

- Status: `resolved`
- Area: `session`, `attach`, `pty`
- Summary: Repeated `Enter` should not collapse or skip shell prompts.
- Regression coverage:
  - `TestHostBurstEnterKeepsConsecutiveBashPromptNumbers`
  - `TestAttachBurstEnterKeepsConsecutiveBashPromptNumbers`
- Investigation notes:
  - The attach-controlled regression resurfaced after the terminal-isolation work.
  - Raw child-PTY capture showed the bug was not a render-only issue: the shell was emitting prompts back-to-back because remote-controlled newline input was reaching the child PTY too aggressively.
  - Local host input remained stable, so the root cause was isolated to the remote-control input delivery path.
  - A host-side serialized remote-input worker now processes remote line bytes one at a time and waits for child PTY output before admitting the next line byte.
- Code-path review:
  - `Runner.handleRemoteInput` now only filters and enqueues remote bytes; it no longer writes directly to the PTY.
  - `localSession.processRemoteInput` owns the pacing boundary for remote line-only input.
  - PTY reader publish flow calls `notifyOutput()` after snapshot/publish, which is what releases the next queued remote line byte.
- Verification:
  - `go test -count=20 ./internal/session -run TestAttachBurstEnterKeepsConsecutiveBashPromptNumbers`
  - `go test -count=20 ./internal/session -run 'TestHostBurstEnterKeepsConsecutiveBashPromptNumbers|TestAttachCtrlDDoesNotExitHost|TestAttachBurstEnterKeepsConsecutiveBashPromptNumbers'`
  - `go test ./...`
  - `go vet ./...`
  - `golint ./...`
  - `golangci-lint run ./...`
  - `make test-webui`

### B-007 Android viewport still fits full host width and hides usable output

- Status: `in_progress`
- Area: `android`, `terminal`, `render`
- Summary: Android must treat the terminal as a camera viewport, not shrink the entire host width into the phone screen.
- Report:
  The Android app is visibly unusable: prompts and content are misplaced or microscopic, and the current integration suite still passes.
- Repro:
  1. Start a host with a wide terminal size such as the current Android harness default `120x24`.
  2. Open the Android app and connect to the host.
  3. Observe the terminal rendered as a tiny block in the top-left because the full host width is being fit into the phone viewport.
- Regression coverage:
  - `systems.pkt.lingon.EndToEndTest#host_width_is_authoritative`
  - `systems.pkt.lingon.EndToEndTest#share_token_width_is_authoritative`
- Investigation notes:
  - A live emulator screenshot during the integration run showed the terminal shrunk into a tiny top-left block while the suite continued to pass.
  - The previous assertions only checked `activeSnapshot` and that `viewCols/viewRows` were positive, which does not prove the visible output is usable.
  - The Android viewport was still configured with `fitToViewWidth = true`, which forced the host width into the phone width instead of behaving like a camera.
- Verification in progress:
  - `./gradlew :app:testDebugUnitTest`
  - targeted connected Android instrumentation for:
    - `host_width_is_authoritative`
    - `share_token_width_is_authoritative`

### B-008 Preserved shrink buffer leaks into live host and Android rendering

- Status: `in_progress`
- Area: `session`, `render`, `android`
- Summary: Hidden preserved rows and columns must remain hidden while the local PTY is shrunk; they may only reappear after the PTY expands or through scrollback.
- Report:
  Host and Android both show the current cursor/prompt above stale old content after the local PTY shrinks under a larger Lingon viewport.
- Repro:
  1. Render content across the full local PTY height.
  2. Shrink the local PTY while leaving the Lingon host viewport larger.
  3. Force a redraw.
  4. Observe preserved lower rows still painted as live content below the current cursor.
- Regression coverage:
  - `internal/session.TestHostShrinkHidesPreservedRowsUntilLocalPTYExpands`
- Investigation notes:
  - The anti-cropping work preserved old cells by merging snapshots, but the merged preserved snapshot was also being published and rendered as the live snapshot.
  - That conflated hidden restore data with current live geometry, so both host and Android painted preserved rows as if they were active screen rows.
- Required fix:
  - Keep the preserved merged snapshot internal to `localSession`.
  - Publish and render only the live cropped snapshot while the PTY is shrunk.
  - Rehydrate from the preserved buffer when the PTY expands again.
