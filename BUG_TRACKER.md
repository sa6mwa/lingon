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
  - `internal/session.TestResizeKeepsSessionResponsive`
  - `internal/attach.TestMultiAttachHeadlessResizePropagatesToPTY`
  - Android unit coverage ensuring app-side resize frames are not sent
  - Connected Android instrumentation coverage partially rerun
- Implementation notes:
  - Android app resize sending path removed/disabled
  - Non-headless host `publisher.OnResize` now ignores remote resize
  - Headless Lingon-owned PTYs keep resize enabled via explicit session capability and direct emulator snapshot propagation after resize
- Verification:
  - `go test ./...`
  - `go vet ./...`
  - `golint ./...`
  - `golangci-lint run ./...`
  - `make test-webui`
  - `./gradlew :app:testDebugUnitTest`
  - `go test -count=1 ./internal/session -run TestAttachControlResizeDoesNotResizeNonHeadlessHostPTY`
  - `go test -count=1 ./internal/session -run TestResizeKeepsSessionResponsive`
  - `go test -count=1 ./internal/attach -run 'TestMultiAttachHeadlessResizePropagatesToPTY|TestMultiAttachHeadlessInitialAttachSizePropagatesToPTY'`
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
  6. More recently reproduced on a real host PTY with this simpler sequence:
     - draw a fixed wide screen with a prompt row,
     - shrink,
     - expand,
     - press `Enter`,
     - observe preserved right-side content collapse or smear while the prompt/cursor redraw lands on top of stale content.
- Regression coverage:
  - `internal/session.TestHostResizePreservesWideContentInScrollbackAfterPostShrinkOutput`
  - `internal/session.TestHostSIGWINCHPreservesScrolledWideOutputWithoutInput`
  - `internal/session.TestHostSIGWINCHPreservesInteractiveWideOutputWithoutInput`
  - `internal/session.TestHostSIGWINCHPsAuxAdvancePreservesExpandedScreen`
  - `internal/session.TestHostSIGWINCHPromptAdvancePreservesExpandedMixedWidthScreen`
  - Surrounding preservation coverage reverified:
    - `TestHostResizePreservesWideContentAcrossShrinkAndExpand`
    - `TestHostResizePreservesWideContentInScrollbackWhileViewportIsNarrow`
    - `TestHostResizePreservesLowerViewportContentAcrossShrinkAndExpand`
    - `TestHostResizePreservesScrollbackHistory`
    - `TestHostScrollbackResizeRepaintsIndicatorWithoutInput`
- Investigation notes:
  - The previous tests only covered quiet shrink/expand cases and missed the destructive case where new post-shrink output pushed preserved rows into scrollback.
  - That still missed the real interactive SIGWINCH path. A separate helper-process regression with a real controlling PTY reproduced the exact user-visible failure: expand first restored the wide content, then a later render collapsed it back to a cropped prompt-at-bottom view.
  - The remaining strict failure was `shrink -> expand -> Enter` after a real `bash` + `ps aux` screen. The expanded host view looked correct until later partial redraw bytes arrived; then the bottom half recropped and cursor/prompt landed on stale content.
  - Raw PTY capture showed bash was sending partial cursor-positioned updates after `Enter`, assuming the expanded screen contents were already present. Lingon was visually restoring the preserved screen, but its emulator state still reflected the shrunken view, so those partial updates applied onto stale state and corrupted the host screen.
  - The fix rehydrates the emulator with the same restored viewport snapshot Lingon displays on non-headless host expand, so subsequent partial PTY redraws land on the correct framebuffer.
- Verification:
  - `go test -count=1 ./internal/session -run 'TestHostSIGWINCH(PreservesScrolledWideOutputWithoutInput|PreservesInteractiveWideOutputWithoutInput|PromptRedrawDoesNotCorruptPreservedWideScreen|PromptAdvanceDoesNotCorruptPreservedScrolledScreen|PsAuxAdvancePreservesExpandedScreen|TruncatedRedrawPreservesWideTails)'`
  - `go test -count=1 ./internal/session -run 'TestResizeKeepsSessionResponsive|TestHostSIGWINCHPreservesInteractiveWideOutputWithoutInput|TestHostSIGWINCHPreservesScrolledWideOutputWithoutInput'`
  - `go test -count=1 ./internal/attach -run TestMultiAttachHeadlessResizePropagatesToPTY`
  - `go test -count=20 ./internal/session -run 'TestHostSIGWINCH(PreservesInteractiveWideOutputWithoutInput|PreservesScrolledWideOutputWithoutInput|HelperProcess)'`
  - `go test -count=1 ./...`
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

### B-009 Android integration runner repeatedly cold-boots emulators and wastes time

- Status: `resolved`
- Area: `android`, `tests`, `tooling`
- Summary: Local Android integration runs should reuse a running emulator and reset app state between cases instead of paying repeated emulator boot cost.
- Report:
  The integration runner was cold-booting or forcing full environment restart cost too often, making Android verification much slower than necessary.
- Repro:
  1. Run `make integration-test` from `android/`.
  2. Let the script complete.
  3. Run it again.
  4. Observe that the previous emulator was killed at script exit, so the next run cold-boots again.
- Regression coverage:
  - Script-level verification remains live-run based; there is no shell test harness for the runner.
- Implementation notes:
  - `android/scripts/run-integration-tests.sh` now keeps a script-started emulator alive by default.
  - The runner resets `systems.pkt.lingon` and `systems.pkt.lingon.test` state between per-test instrumentation invocations and clears device-side test artifacts before each case.
  - `adb reverse --remove-all` is issued before re-establishing the harness port mapping so reuse does not accumulate stale reverse mappings.
- Verification:
  - `bash -n android/scripts/run-integration-tests.sh`
  - Code-path review confirming:
    - cleanup only kills the emulator when `LINGON_IT_KEEP_EMULATOR=0`
    - the per-test loop resets app state before each Gradle instrumentation invocation
    - harness restarts remain limited to tests that actually need different backend topology
  - Live emulator verification on `emulator-5554`:
    - `env LINGON_IT_ONLY=top_bar_menu_is_accessible ./scripts/run-integration-tests.sh`
    - the same command immediately after
    - both runs passed on the same running emulator
    - the second run did not print `Starting emulator ...`, confirming reuse
    - the runner printed `Resetting Android app state...` before the instrumentation case on both runs
- Remaining cost:
  - The runner still invokes `connectedDebugAndroidTest` once per test method, so Gradle/instrumentation startup overhead remains even though emulator reboot cost is now removed by default.

### B-010 Test runs leak real desktop notifications to the developer session

- Status: `resolved`
- Area: `tests`, `desktopnotify`, `session`, `attach`
- Summary: Test and end-to-end runs must never emit real desktop notifications to the developer’s desktop session.
- Report:
  During recent `make test-webui` runs, the developer still received real desktop notifications.
- Repro:
  1. Run `make test-webui`.
  2. Observe real desktop notifications during wall/inactivity-related tests.
- Investigation notes:
  - The recent `make test-webui` output strongly points at wall/inactivity tests in `internal/session` and `internal/attach`, not generic chromedp/browser automation itself.
  - `Runner.fireLocalWallNotification` still falls back to `desktopnotify.New()` whenever `Options.DesktopNotifier` is nil and `DisableDesktopNotifications` is false.
  - PTY harness defaults use a noop notifier, so the remaining leak is likely from test paths that instantiate `Runner` or attach views without an injected notifier or explicit disable flag.
- Regression coverage:
  - `internal/session.TestRunnerLocalWallNotificationUsesNotifierFactoryWhenUnset`
  - `internal/attach.TestClientHandleWallUsesNotifierFactoryWhenUnset`
  - package-wide reruns of `internal/session` and `internal/attach` wall/inactivity paths under forced noop notifier
- Implementation notes:
  - `internal/desktopnotify.New()` now goes through a swappable factory so test binaries can force a noop notifier.
  - `internal/session` and `internal/attach` install a package-wide noop notifier in `TestMain`, so any fallback notifier allocation in those test binaries stays local and silent.
  - Direct regression tests cover the previously unsafe fallback paths where session or attach code reached for `desktopnotify.New()` without an injected notifier.
  - `internal/attach.Client` now lazily resolves the notifier in the actual notification path, so the fallback is testable and consistent.
- Verification:
  - `go test -count=1 ./internal/session -run 'Test(RunnerLocalWallNotificationUsesNotifierFactoryWhenUnset|LocalWallInactivityShowsModalOnOtherLocalTabAndDesktopNotification|RelayBacked.*Wall.*|HostSIGWINCHPsAuxAdvancePreservesExpandedScreen|HostSIGWINCHPromptAdvancePreservesExpandedMixedWidthScreen)'`
  - `go test -count=1 ./internal/attach -run 'Test(ClientHandleWallUsesNotifierFactoryWhenUnset|AttachHonorsRetryAfter|AttachWallModalShowsWrappedLongMessage|MultiAttachHeadlessRoutedStatusStaysOnActiveSession)'`
  - `go test -count=1 ./internal/session`
  - `go test -count=1 ./internal/attach`
  - `make test-webui`
  - `go test -count=1 ./...`
  - `go vet ./...`
  - `golint ./...`
  - `golangci-lint run ./...`
