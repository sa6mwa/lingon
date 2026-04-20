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

### B-011 Multi-attach viewport/camera semantics broken

- Status: `resolved`
- Area: `attach`, `mvu`, `session`
- Summary: Normal `lingon attach` (the multi-attach client) must behave like a camera onto the active session, not resize or reflow the underlying non-headless host session.
- Report:
  Multi-attach startup paint is garbled, resizing the attach viewport destroys the view, and running commands through attach wraps/reflows as if the active session were resized to the local attach terminal.
- Repro:
  1. Start a normal non-headless host with a wide local PTY session.
  2. Run normal authenticated `lingon attach` (multi-attach client) into that session set.
  3. Observe broken startup paint and tab/status overlap.
  4. Resize the attach terminal smaller.
  5. Observe the active session view being reflowed/wrapped instead of cropped.
  6. Run a wide-output command such as `ps aux`.
  7. Observe viewport wrapping/smearing instead of camera cropping.
- Regression coverage:
  - `internal/attach.TestMultiAttachWithoutExplicitTermSizeMatchesControlViewportAcrossStartupResizeAndCommand`
  - Existing guard rails rechecked:
    - `internal/attach.TestMultiAttachStartupDoesNotSendResizeToRelayHost`
    - `internal/attach.TestMultiAttachResizeDoesNotResizeRelayHostPTY`
    - `internal/attach.TestMultiAttachViewportCropsWideHostOutputInsteadOfWrapping`
- Investigation notes:
  - Normal authenticated `lingon attach` uses `attach.MultiClient`, not the single `attach.Client`.
  - The earlier harness tests were misleading because `ptytest.StartMultiAttach` always injects an explicit `TermSize` function.
  - The real CLI path does not. In `MultiClient.Run`, `termSize := m.TermSize` stayed nil, then child `attach.Client` instances inherited that nil `TermSize`.
  - Once `MultiClient` swapped client stdout to the locked writer, the child client could no longer discover the real local tty size from `stdoutWriter()`, so it fell back to remote snapshot dimensions.
  - That caused the exact user-visible failures:
    - startup painted only against the remote snapshot footprint instead of the local viewport,
    - resizing the local attach PTY did not update camera dimensions,
    - wide output such as `ps aux` wrapped/garbled because rendering targeted the wrong width.
  - The new regression starts `attach.MultiClient` in a real PTY without an explicit `TermSize`, compares it against the harness control path with the same local viewport, and fails on the visible body mismatch.
  - The runtime fix makes `MultiClient` derive a real terminal-size provider from its own local stdin/stdout tty when `TermSize` is unset, so normal `lingon attach` now uses the real local viewport/camera dimensions.
- Verification:
  - `go test -count=1 ./internal/attach -run 'TestMultiAttachWithoutExplicitTermSizeMatchesControlViewportAcrossStartupResizeAndCommand'`
  - `go test -count=1 ./internal/attach`
  - `go test -count=1 ./internal/session -run TestHostSIGWINCHPsAuxAdvancePreservesExpandedScreen -v`
  - `go test -count=1 ./...`
  - `go vet ./...`
  - `golint ./...`
  - `golangci-lint run ./...`
  - `make test-webui`
- Notes:
  - During full-suite verification, `TestHostSIGWINCHPsAuxAdvancePreservesExpandedScreen` failed because its `ps aux` assertion compared the dynamic `STAT` column literally (`SN+` vs `RN+`). That was tightened by normalizing the `STAT` field along with other dynamic process columns so the regression remains about screen preservation, not scheduler timing noise.

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
  - Existing instrumentation only proved the title carried the source; it did not assert the body or expanded content also surfaced `user@addr#session`.
- Investigation notes:
  - Relay already emits `sender = username@ip`.
  - Android receives `sender` and `sourceSessionName`, but `AndroidWallNotifier` currently uses the formatted source only for the title.
  - The body shown in the notification is still just the wall message, so the requested source format is not guaranteed to appear on the visible surface the user actually sees.
- Required fix:
  - Make the visible notification payload surface `user@addr#session` in both title and body/expanded text.
  - Add a regression at the actual posted-notification boundary.
- Verification:
  - `./gradlew :app:testDebugUnitTest`
  - `./gradlew :app:compileDebugAndroidTestKotlin`
  - Connected Android instrumentation passed on emulator for:
    - `background_wall_delivery_posts_system_notification`
- Notes:
  - Reopened after user report that installed Android build still did not visibly show `username@addr#sessionname`.
  - Fixed by making the posted notification body/expanded text include the same `sender#session` source label as the title, instead of showing only the wall message body.
  - The instrumentation assertion now proves the actual posted notification text equals `<title>: <message>`, so the visible payload cannot silently drop the source label again.

### B-003 Local PTY anti-cropping preservation still broken

- Status: `in_progress`
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
  7. Additional host-TUI sequences that were reproduced on the real host path:
     - shrink,
     - expand,
     - press `Ctrl+L` twice,
     - observe stale pre-resize content still visible and the prompt/cursor no longer on row 1.
     - shrink,
     - press `Enter` while still shrunk,
     - expand,
     - observe duplicated/cropped preserved rows instead of a cleanly advanced screen.
     - shrink,
     - type a normal command while still shrunk,
     - press `Enter`,
     - expand,
     - observe typed command text smeared into preserved wide rows as if the preserved screen were being rewritten in the shrunk coordinate space.
  8. Additional host-TUI sequence still reproducing after the earlier fix:
     - render a full-height wide screen with the prompt on the bottom row,
     - shrink,
     - expand,
     - observe the cursor/prompt restored one row too high while the last content row is effectively pushed below the visible window.
  9. Additional host-TUI sequence reproduced from the latest screenshots:
     - render a full-height wide screen,
     - shrink,
     - run `clear`,
     - expand,
     - observe a blank screen with only the cursor visible,
     - then run a short command such as `ps aux`,
     - observe stale pre-clear rows revived under the new output.
  10. Additional host-TUI sequence still reproducing after the clear fix:
      - render a wide screen with the tab bar visible,
      - shrink and stay shrunk,
      - type a short command without pressing `Enter`,
      - observe the typed text land on row 1/tab chrome while the real prompt is off-screen.
- Regression coverage:
  - `internal/session.TestHostResizePreservesWideContentInScrollbackAfterPostShrinkOutput`
  - `internal/session.TestHostSIGWINCHPreservesScrolledWideOutputWithoutInput`
  - `internal/session.TestHostSIGWINCHPreservesInteractiveWideOutputWithoutInput`
  - `internal/session.TestHostResizeCtrlLClearAfterExpandClearsPreservedContent`
  - `internal/session.TestHostResizePromptAdvanceWhileShrunkRestoresExpandedRowsWithTabBar`
  - `internal/session.TestHostResizeTypingWhileShrunkThenExpandPreservesCommandLine`
  - `internal/session.TestHostResizePreservesWideScreenWithBottomCursorWithoutInput`
  - `internal/session.TestHostResizePreservesScrolledWideOutputWithoutInput`
  - `internal/session.TestHostResizePreservesScrolledWideOutputWithTabBarVisible`
  - `internal/session.TestHostResizePostExpandFullScreenOutputMatchesControl`
  - `internal/session.TestHostResizePsAuxAfterExpandKeepsPromptOnBottomRow`
  - `internal/session.TestHostResizePlainPsAuxAfterExpandKeepsPromptOnBottomRow`
  - `internal/session.TestHostSIGWINCHPlainPsAuxAfterExpandKeepsPromptOnBottomRow`
  - `internal/session.TestHostSIGWINCHPlainPsAuxAfterExpandKeepsPromptOnBottomRowLargeViewport`
  - `internal/session.TestHostSIGWINCHClearAfterExpandKeepsPromptVisible`
  - `internal/session.TestHostSIGWINCHClearAfterMultiStepResizeKeepsPromptVisible`
  - `internal/session.TestHostResizeLargeViewportFullScreenClearAfterExpandMatchesControl`
  - `internal/session.TestHostResizeLargeViewportClearThenShortCommandMatchesControl`
  - `internal/session.TestHostResizeLargeViewportCtrlLLClearThenShortCommandMatchesControl`
  - `internal/session.TestHostResizeLargeViewportClearWhileShrunkThenExpandMatchesControl`
  - `internal/session.TestHostResizeLargeViewportClearWhileShrunkThenExpandPsAuxMatchesControl`
  - `internal/session.TestHostResizeBashClearWhileShrunkThenExpandMatchesControl`
  - `internal/session.TestHostResizeWhileShrunkWithDecoratedPromptKeepsPromptVisible`
  - `internal/session.TestHostResizeWhileShrunkAfterPsAuxKeepsPromptVisibleAcrossViewportSizes`
  - The interactive host resize regressions now compare the full visible screen after shrink/expand/input flows, not just selected content rows.
  - Surrounding preservation coverage reverified:
    - `TestHostResizePreservesWideContentAcrossShrinkAndExpand`
    - `TestHostResizePreservesWideContentInScrollbackWhileViewportIsNarrow`
    - `TestHostResizePreservesLowerViewportContentAcrossShrinkAndExpand`
    - `TestHostResizePreservesScrollbackHistory`
    - `TestHostScrollbackResizeRepaintsIndicatorWithoutInput`
- Investigation notes:
  - The previous preservation tests were too quiet. They covered shrink/expand without the follow-up local redraws that the user was actually hitting.
  - The exact host failure was reproduced with a real `bash` PTY using both `Ctrl+L` after expand and `Enter while shrunk -> expand`. The latter matched the screenshots: the wide content restored on expand, then a later prompt advance recropped and smeared preserved rows.
  - Raw PTY capture showed bash only emitted a simple `\r\nPROMPT...` advance. The corruption was introduced by Lingon while merging the shrunk viewport back into the preserved framebuffer.
  - The previous fix was incomplete. It handled quiet prompt-advance and clear cases, but it still attempted to merge shrunk local-PTY redraws into the preserved wide framebuffer in the wrong coordinate space.
  - The new failing host regression shows the exact breakage: typing `echo TYPED-OK` while shrunk rewrites preserved wide rows with shrunk-screen content.
  - The final fix stopped overlaying shrunk local redraws onto the preserved framebuffer directly. Instead, when preservation is active, Lingon keeps a second preserved emulator that stays in the preserved coordinate space and receives the real PTY output stream. The visible viewport is then cropped from that preserved emulator snapshot.
  - That dual-emulator cut removes the coordinate-space corruption that caused `Enter`, `Ctrl+L`, and typed command echoes to smear/crop preserved rows after shrink/expand cycles.
  - The regression assertions were then tightened again so the host preservation tests compare the full viewport after each operation, with only dynamic tab-title tokens normalized on row 1.
  - There was still one leftover non-emulator shortcut in the local host read loop: newline-only and simple-prompt chunks could bypass the emulator path and synthesize preserved snapshots directly.
  - That shortcut branch has now been removed. While preservation is active, local host snapshots now always come from the emulator-driven preservation path rather than ad hoc newline/prompt snapshot synthesis.
  - A remaining clear-specific host failure was then reproduced at the render boundary: after a resized full-screen restore, `clear` did not force a real full-screen repaint, so the next shorter command could leave stale body rows visible on the real terminal.
  - The host render path now forces a full MVU repaint when PTY output carries a real clear/reset sequence (`CSI 2J`, `CSI 3J`, or `RIS`), which matches the semantics the terminal needs after `clear` and `Ctrl+L l`.
  - The latest remaining clear bug was narrower: after a shrink, Lingon armed a one-shot “ignore the next PTY redraw” guard meant to discard resize artifacts.
  - That guard treated any escaped output as suppressible, so a real `clear` emitted after the shrink could be dropped as if it were the resize redraw.
  - Once the clear was swallowed, the preserved screen stayed stale and the next short command overlaid new output onto old pre-clear rows, matching the blank-screen and stale-body screenshots.
  - The fix narrows that suppression rule so real full-screen reset output (`CSI 2J`, `CSI 3J`, `RIS`) is never ignored and instead resets the preserved viewport origin normally.
  - After the subsequent user report, the exact remaining “plain shrink leaves the prompt off-screen” screenshot still has not been trapped red under PTY harness coverage.
  - Two harsher probes were added for that gap:
    - decorated bash prompts with title/color escape sequences,
    - a matrix of smaller shrunk viewport sizes after real `ps aux` output.
  - Those new regressions are green on the current branch, so the remaining mismatch appears narrower than the current deterministic PTY cases and likely depends on a more specific host/session state sequence that still needs isolation.
  - The current remaining bug is different: while preservation is active and the viewport stays shrunk, normal PTY output updates the preserved emulator but the visible viewport origin is not recomputed from the new preserved cursor position.
  - That leaves the prompt/cursor off-screen and clamps the cropped cursor back onto row 1, which matches the screenshot where typed command text bleeds into the tab bar.
- Verification:
  - Focused signal-path preservation slice:
  - `go test -count=1 ./internal/session -run 'TestHostSIGWINCH(PreservesScrolledWideOutputWithoutInput|PreservesInteractiveWideOutputWithoutInput|PromptRedrawDoesNotCorruptPreservedWideScreen|PromptAdvanceDoesNotCorruptPreservedScrolledScreen|PromptAdvancePreservesExpandedMixedWidthScreen|PsAuxAdvancePreservesExpandedScreen|TruncatedRedrawPreservesWideTails)'`
  - Real host typed-command regressions:
    - `go test -count=1 ./internal/session -run 'TestHostResize(TypingWhileShrunkThenExpandPreservesCommandLine|TypingAfterExpandPreservesPromptLine|CtrlLClearAfterExpandClearsPreservedContent|PromptAdvanceWhileShrunkRestoresExpandedRowsWithTabBar)'`
  - `go test -count=1 ./internal/session -run 'TestHostResize(CtrlLClearAfterExpandClearsPreservedContent|PromptAdvanceWhileShrunkRestoresExpandedRowsWithTabBar)'`
  - `go test -count=1 ./internal/session -run 'TestHostResizeLargeViewport(FullScreenClearAfterExpandMatchesControl|ClearThenShortCommandMatchesControl|CtrlLLClearThenShortCommandMatchesControl)'`
  - `go test -count=1 ./internal/session -run 'TestHostResize(LargeViewportClearWhileShrunkThenExpandMatchesControl|LargeViewportClearWhileShrunkThenExpandPsAuxMatchesControl|BashClearWhileShrunkThenExpandMatchesControl)'`
  - `go test -count=1 ./internal/session`
  - `go test -count=1 ./internal/attach`
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
  - Added package-level self-PTY isolation in:
    - `internal/session.TestMain`
    - `internal/attach.TestMain`
    - `internal/testpty`
    so `go test` re-execs those packages under an owned PTY from inside the test code itself, with no external wrapper.
  - Verified narrow package execution under self-owned PTY:
    - `go test -count=1 ./internal/session -run TestLocalSessionOSCQueryDoesNotSelfSustainPublish`
    - `go test -count=1 ./internal/attach -run TestThemeActiveIndexHeaderColor`
  - Verified a real resize-driving session test no longer mutated the current tmux pane:
    - `go test -count=1 ./internal/session -run TestHostSIGWINCHPreservesScrolledWideOutputWithoutInput`
    - pane size remained `119x62` before and after the run
 - Notes:
  - The remaining leak was not the Android harness wrapper anymore; it was the runtime attach/session packages still subscribing to process-global `SIGWINCH` even when tests already injected PTY-local resize events.
  - The self-PTY `TestMain` cut closes the remaining inherited-tty hole at the package boundary: helper subprocesses and `/dev/tty` fallbacks now bind to the owned test PTY instead of the outer tmux session.
  - The resize-driving session test is still functionally red because `B-003` is not finished, but it no longer leaks terminal mutation to the outer pane.

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
- Area: `tests`, `desktopnotify`
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
  - A later broad `go test ./...` run still produced real desktop notifications even after package-local `TestMain` patches, which proved the real gap was broader: other package test binaries can import notifier-using code without inheriting those package-local overrides.
- Regression coverage:
  - `internal/desktopnotify.TestNewDefaultsToNoopNotifierUnderGoTest`
  - `internal/desktopnotify.TestRunningUnderTestBinary`
  - `internal/session.TestRunnerLocalWallNotificationUsesNotifierFactoryWhenUnset`
  - `internal/attach.TestClientHandleWallUsesNotifierFactoryWhenUnset`
  - `internal/headlessd.TestDaemonNotifyDesktopUsesNotifierFactoryWhenUnset`
  - focused reruns of notifier fallback paths in `internal/session`, `internal/attach`, and `internal/headlessd`
- Implementation notes:
  - `internal/desktopnotify.New()` now defaults to `noopNotifier` automatically inside any `go test` binary, so fallback notifier allocation is silent across all package test binaries, not only packages with bespoke `TestMain` setup.
  - Package-local notifier overrides in `internal/session`, `internal/attach`, and `internal/headlessd` are no longer required to keep tests silent.
  - Direct regression tests cover the previously unsafe fallback paths where session or attach code reached for `desktopnotify.New()` without an injected notifier.
  - `internal/attach.Client` now lazily resolves the notifier in the actual notification path, so the fallback is testable and consistent.
- Verification:
  - `go test -count=1 ./internal/desktopnotify`
  - `go test -count=1 ./internal/session -run 'Test(RunnerLocalWallNotificationUsesNotifierFactoryWhenUnset|LocalWallInactivityShowsModalOnOtherLocalTabAndDesktopNotification|RelayBacked.*Wall.*|HostSIGWINCHPsAuxAdvancePreservesExpandedScreen|HostSIGWINCHPromptAdvancePreservesExpandedMixedWidthScreen)'`
  - `go test -count=1 ./internal/attach -run 'Test(ClientHandleWallUsesNotifierFactoryWhenUnset|AttachHonorsRetryAfter|AttachWallModalShowsWrappedLongMessage|MultiAttachHeadlessRoutedStatusStaysOnActiveSession)'`
  - `go test -count=1 ./...`
  - `make test-webui`
  - `go vet ./...`
  - `golint ./...`
  - `golangci-lint run ./...`
