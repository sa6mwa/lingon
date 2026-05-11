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

### B-064 Android terminal viewport can show stale oversized content after initial/IME resize

- Status: `needs_verification`
- Area: `android`, `terminal`, `viewport`, `IME`, `render`
- Summary: When the IME is already visible before terminal content finishes loading, long session content can initially render as if the terminal viewport still extends below the visible app viewport. Hiding/showing the IME does not visibly correct the camera until the user pans.
- Report:
  The engineer reports that sufficiently long sessions consistently start with terminal content cropped below the bottom of the visible viewport. With the IME already up before session load, the terminal appears to load underneath the keyboard; toggling the keyboard does not move the viewport until a pan/touch forces another redraw.
- Repro steps:
  1. Start the Android app with enough terminal content to exceed the phone viewport.
  2. Let the IME appear before the terminal session finishes loading/reconnecting.
  3. Observe that bottom content is drawn/cropped below the visible viewport and keyboard.
  4. Hide/show the IME; observe the viewport does not visibly reanchor until the terminal is panned.
- Investigation:
  - `TerminalGridView.onSizeChanged()` recalculated layout and camera state for IME/Compose height changes but did not invalidate the view.
  - That matches the visible behavior: the state can be corrected internally while pixels remain from the old larger viewport until a later touch/pan triggers `invalidate()`.
  - Physical-phone verification showed the invalidation-only fix was insufficient, and delaying IME focus only moved the keyboard timing without fixing the terminal/content boundary.
  - The terminal panel, its weighted viewport box, and embedded `AndroidView` did not explicitly clip at the Compose viewport boundary, so any parent/AndroidView placement mismatch can let terminal content visibly bleed below the intended viewport until a pan/zoom forces a new interactive draw/layout path.
  - The existing Android keyboard tests focused the hidden `TerminalInput` node directly. That bypassed the production terminal tap path and could miss bugs where the real terminal surface and Android IME insets disagreed.
  - The emulator reproducer can force a real soft-keyboard inset with `show_ime_with_hard_keyboard=1`. On devices where the `AndroidView` remains physically behind the IME overlay, Compose bounds can look correct while the renderer still computes camera height from the full view height.
- Fix:
  - `TerminalGridView.onSizeChanged()` now invalidates immediately after recomputing layout and applying any pending restore.
  - The terminal panel, terminal viewport box, embedded Android terminal view, and quick-key bar are now clipped to their Compose bounds.
  - Keyboard integration helpers now focus `TerminalFocus`, matching the production tap path instead of clicking the hidden input node.
  - `TerminalGridView` now derives its effective camera viewport height from the actual visible portion of the view above the current IME inset. If Compose has already resized the view above the IME, this is a no-op; if Android leaves the view behind an overlay keyboard, camera layout, live-bottom anchoring, panning, clipping, restore, and debug visibility use the reduced visible height.
- Regression coverage:
  - Added `terminal_resize_invalidates_after_live_bottom_reanchor`, which asserts a terminal resize both reanchors the live viewport to the terminal bottom and records a resize invalidation before any pan/touch path can redraw it.
  - Tagged the quick-key container and strengthened keyboard/tall-session instrumentation coverage to assert the terminal viewport bottom never overlaps the quick-key top when the IME controls are visible.
  - Added `soft_keyboard_inset_keeps_terminal_viewport_above_keyboard`, which forces a real soft IME on the emulator, loads a 240-row session, focuses the real terminal surface, and asserts the effective terminal camera viewport is bounded by the visible area above the IME, stays live-bottom anchored, and does not move during the settle window.
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed after the clipping fix.
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed after the effective IME viewport fix.
  - `cd android && LINGON_IT_ONLY=soft_keyboard_inset_keeps_terminal_viewport_above_keyboard make integration-test` passed. Resource profile: `/home/mike/g/lingon/android/test-artifacts/resource-profile-20260512-015200-4064878/summary.txt`.
  - `cd android && LINGON_IT_ONLY=keyboard_hide_show_preserves_bottom_anchor_for_tall_sessions make integration-test` passed. Resource profile: `/home/mike/g/lingon/android/test-artifacts/resource-profile-20260512-015323-4070926/summary.txt`.
  - `cd android && LINGON_IT_ONLY=keyboard_visible_before_background_is_restored_after_resume make integration-test` passed. Resource profile: `/home/mike/g/lingon/android/test-artifacts/resource-profile-20260512-015506-4077816/summary.txt`.
  - Physical-phone confirmation is still pending.

### B-063 Android viewport cache leaks across logout and identity changes

- Status: `resolved`
- Area: `android`, `terminal`, `viewport`, `logout`, `identity`
- Summary: The top-level Android terminal viewport cache can survive logout or identity changes and restore a stale camera when another relay/user reuses the same session ID.
- Report:
  Review found that hoisting the viewport cache above `TerminalScreen` lets it outlive logout, endpoint changes, and login/cert screens while restore indexes only by `activeSessionId`.
- Repro:
  1. Attach as one endpoint/user with a session ID such as `host-1`.
  2. Move the terminal camera away from the default cursor-follow position.
  3. Logout so `TerminalScreen` is disposed.
  4. Attach as another endpoint/user that reuses `host-1`.
  5. Observe that the new terminal can restore the stale camera from the previous identity.
- Investigation notes:
  - The finding is real. The cache lives at top-level app composition and the key was only the session ID.
  - Clearing alone is fragile because `TerminalScreen` disposal can capture the old viewport during the transition. Restore lookup must also be scoped to the attached identity.
  - The fix scopes cache entries by endpoint/principal/session and clears the cache whenever the attached identity changes or disappears.
- Regression coverage:
  - Added instrumentation coverage that moves the camera for one identity, logs out, asserts the cache is empty, then logs in as another identity with the same session ID and asserts the previous camera is not restored.
  - Reran the app-lock viewport regression to prove same-identity app-lock restore still preserves the cached camera.
- Verification:
  - `LINGON_IT_ONLY=logout_clears_viewport_cache_and_reused_session_id_does_not_restore_stale_camera make integration-test` passed. Resource profile: `/home/mike/g/lingon/android/test-artifacts/resource-profile-20260511-091517-1858597/summary.txt`.
  - `LINGON_IT_ONLY=app_lock_unlock_preserves_terminal_camera_viewport make integration-test` passed. Resource profile: `/home/mike/g/lingon/android/test-artifacts/resource-profile-20260511-091645-1864750/summary.txt`.
  - `./gradlew :app:compileDebugAndroidTestKotlin` passed.
  - `./gradlew :app:testDebugUnitTest` passed.

### B-062 Android app lock unlock loses terminal camera viewport

- Status: `resolved`
- Area: `android`, `terminal`, `viewport`, `app-lock`, `lifecycle`
- Summary: Unlocking the Android app after app lock can recreate the terminal without the saved camera viewport, so the restored view pans to keep the cursor at the bottom-left instead of preserving the previous camera.
- Report:
  When restoring a session in the Android app after unlocking from the app-lock screen, the camera is not restored to the same position as before lock. It pans to keep the cursor bottom-left.
- Repro:
  1. Open a terminal session with enough content that the camera can be away from cursor-follow bottom.
  2. Position the camera at a saved viewport.
  3. Let app lock replace the terminal with the locked screen.
  4. Unlock the app.
  5. Observe the recreated terminal camera follows the cursor instead of restoring the previous camera.
- Investigation notes:
  - The app-lock branch removes `TerminalScreen` from composition while showing `LockedScreen`.
  - The terminal viewport cache lived inside `TerminalScreen`, so app-lock disposal destroyed the cache that reconnect/refocus restore paths rely on.
  - `TerminalPanel` also only captured viewport on lifecycle `ON_STOP`, not when it was disposed by an in-app lock state transition.
  - The first regression run reproduced the unlock failure: after unlocking, the viewport restored to row 38 instead of the saved row 10.
  - Hoisting the viewport cache kept the captured state alive across app-lock composition changes, but restore still needed ordering guards: a recreated `TerminalGridView` can be laid out before it has a snapshot/frame, and a no-cache restore attempt must not mark the view as restored.
- Regression coverage:
  - Added instrumentation coverage for the app-lock branch that disposes `TerminalScreen`, captures a top camera viewport, unlocks, and asserts the recreated terminal preserves the saved visible start row and camera offset instead of cursor-following to the bottom.
- Verification:
  - `LINGON_IT_ONLY=app_lock_unlock_preserves_terminal_camera_viewport make integration-test` passed. Resource profile: `/home/mike/g/lingon/android/test-artifacts/resource-profile-20260511-050339-1354744/summary.txt`.
  - `./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed.

### B-061 Tagged wall notification cleanup must cancel by tag and ID

- Status: `resolved`
- Area: `android`, `notifications`, `wall`, `e2e`
- Summary: Android wall notifications are posted with non-null tags, but the Android e2e cleanup path still cancelled by ID only, leaving stale wall notifications active across tests.
- Report:
  Review found that `clearWallNotifications()` calls `NotificationManager.cancel(id)` even for tagged `lingon_wall` notifications. Android requires `cancel(tag, id)` for tagged notifications, so stale wall notifications can survive cleanup and confuse later waits/assertions.
- Repro:
  1. Post a tagged wall notification.
  2. Call `NotificationManager.cancel(id)`.
  3. Observe the notification remains active.
  4. Call `NotificationManager.cancel(tag, id)`.
  5. Observe the notification is cleared.
- Investigation notes:
  - The review finding is real. The bug was introduced when wall posts moved from a single id-only notification to per-event tag/id notifications.
  - Background-wall service notifications still use id-only posts, so cleanup must support both tagged and untagged notifications.
- Regression coverage:
  - Added targeted instrumentation coverage proving id-only cancellation does not clear a tagged wall notification and tagged cancellation does.
  - Updated e2e wall-notification cleanup to cancel with `tag,id` when a tag exists and fall back to id-only cancellation otherwise.
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest` passed.
  - `cd android && ./gradlew :app:compileDebugAndroidTestKotlin` passed.
  - Targeted managed-device instrumentation passed with 3 tests: `systemd-run --user --scope -p CPUQuota=200% -p MemoryMax=7G -p MemorySwapMax=0 ./gradlew --no-daemon -Dkotlin.compiler.execution.strategy=in-process "-Dorg.gradle.jvmargs=-Xmx1536m -Dfile.encoding=UTF-8" :app:phoneApi35DebugAndroidTest --no-configuration-cache -Plingon.it.class=systems.pkt.lingon.AndroidWallNotifierInstrumentedTest`.
  - `cd android && make help` shows the corrected cgroup defaults.
  - `git diff --check` passed.

### B-060 Disabled Android wall notification channel can consume wall cursor

- Status: `resolved`
- Area: `android`, `notifications`, `wall`, `cursor`
- Summary: `AndroidWallNotifier` can return success when Android accepts a wall `notify()` call but suppresses the notification, allowing `WallDeliveryCoordinator` to advance the durable wall cursor for an event the user never saw.
- Report:
  Review found that if the existing `lingon_wall` notification channel is disabled by the user, `notify()` can return normally without surfacing an active notification. The current notifier returns `true` unconditionally after `notify()`, so suppressed wall posts are treated as delivered.
- Repro:
  1. Disable the app's `lingon_wall` notification channel while wall/background notifications are otherwise enabled.
  2. Receive a wall event while the app delivery path is posting system notifications.
  3. Android suppresses the notification but `notify()` returns normally.
  4. The durable wall cursor advances, so the missed event is not retried after the channel is re-enabled.
- Investigation notes:
  - The review finding is real. `MonotonicWallDeliveryCoordinator` advances the durable cursor only when `WallNotifier.notifyWall()` returns true, and `AndroidWallNotifier` had dropped the previous active-notification visibility check when wall posts moved from one fixed notification ID to per-event tag/id delivery.
  - Disabled notification channels are not fixed by `ensureChannel()` because an existing disabled channel is preserved. The notifier must reject non-postable channel state before posting and prove the expected tag/id/channel is active after posting.
  - Targeted managed-device verification exposed two more boundary details: `activeNotifications` is not always populated in the same instant that `notify()` returns, and Android can preserve disabled-channel state for a channel ID during an app install. The visibility proof must poll for a bounded interval, and instrumentation must use isolated test channel IDs instead of mutating the production `lingon_wall` channel.
  - The bounded visibility wait runs through the suspend coordinator on `Dispatchers.IO`, so websocket/background delivery does not block the main dispatcher while waiting for Android notification state.
- Regression coverage:
  - Added unit coverage for `IMPORTANCE_NONE` being non-postable and visible channel importances being postable.
  - Added unit coverage that active notification matching requires the exact wall channel, tag, and ID, including Java `String.hashCode()` notification-ID collision fixtures.
  - Added targeted instrumentation coverage that a `lingon_wall` channel created with `IMPORTANCE_NONE` makes `AndroidWallNotifier.notifyWall()` return false, and that an enabled channel reports success only after the expected wall notification is active.
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.notifications.AndroidWallNotifierTest --tests systems.pkt.lingon.notifications.WallDeliveryCoordinatorTest` passed.
  - `cd android && ./gradlew :app:compileDebugAndroidTestKotlin` passed.
  - Targeted managed-device instrumentation failed before the final fix because `activeNotifications` was not immediately visible after `notify()`, then passed with isolated test channel IDs and bounded visibility polling: `systemd-run --user --scope -p CPUQuota=200% -p MemoryMax=7G -p MemorySwapMax=0 ./gradlew --no-daemon -Dkotlin.compiler.execution.strategy=in-process "-Dorg.gradle.jvmargs=-Xmx1536m -Dfile.encoding=UTF-8" :app:phoneApi35DebugAndroidTest --no-configuration-cache -Plingon.it.class=systems.pkt.lingon.AndroidWallNotifierInstrumentedTest`.
  - `cd android && ./gradlew :app:testDebugUnitTest` passed.
  - `git diff --check` passed.

### B-059 Android IME visibility is not preserved across app refocus

- Status: `resolved`
- Area: `android`, `ime`, `keyboard`, `lifecycle`, `e2e`
- Summary: If the soft keyboard is visible when the Android app is backgrounded, it reappears briefly on refocus and then auto-hides about one second later.
- Report:
  On the latest branch build, backgrounding and refocusing the Android app always returns with the keyboard hidden. The visible/hidden IME state from the moment of backgrounding is not honored.
- Repro:
  1. Open the Android app and focus the terminal input so the IME quick keys are visible.
  2. Background the app.
  3. Refocus the app.
  4. Observe the keyboard/quick keys initially return, then disappear after roughly one second.
- Regression coverage:
  - Strengthened `keyboard_visible_before_background_is_restored_after_resume` to assert the quick-key row remains visible after the delayed post-resume hide window, not only immediately after resume.
  - Strengthened `keyboard_hidden_before_background_stays_hidden_after_resume` to assert that hiding the IME records a false lifecycle preference before backgrounding, and that refocus does not flip it back to visible.
- Investigation notes:
  - The original regression test only proved immediate visible restore. It did not cover the delayed post-resume hide window that reproduced the phone behavior.
  - Normal input readiness was sharing the same "restore in progress" suppression as lifecycle ON_START. That allowed a user-hidden IME transition to be ignored as if it were a transient lifecycle restore inset.
  - The hidden input view could be focused directly by tests and platform focus without going through the terminal tap path that records IME intent.
  - Refocus via `am start` can re-enter the activity path; `.MainActivity` now uses `singleTop` so foregrounding targets the existing activity instance instead of stacking a fresh one.
  - Reopened: the real phone still auto-hides after refocus. The missing invariant is that a platform-driven post-resume IME hide must not be interpreted as a user hide when the saved lifecycle preference is visible.
  - Follow-up fix: IME inset changes now go through `TerminalImeLifecyclePolicy`. A false inset while the saved lifecycle preference is visible re-requests focus instead of persisting hidden. User dismissal is explicit through pre-IME Back and ignores stale visible insets until Android reports the hidden inset.
  - Follow-up report: the IME now remains visible, but refocus shifts the terminal camera down by about one or two rows. The restore invariant is broader than IME visibility: the same terminal frame must keep the same viewport while Android settles IME height.
  - Follow-up fix: `TerminalGridView` now retains the restored viewport for the current terminal frame and re-applies it across same-frame height changes, so transient IME hidden/visible height bounces do not run the generic live-bottom snap path. New frames, user pan, zoom, or reset clear this restore guard.
- Verification:
  - `cd android && LINGON_IT_ONLY=keyboard_hidden_before_background_stays_hidden_after_resume make integration-test` failed before the final fix, then passed.
  - `cd android && LINGON_IT_ONLY=keyboard_visible_before_background_is_restored_after_resume make integration-test` failed before the delayed assertion fix, then passed.
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed.
  - `git diff --check` passed.
  - Added `TerminalImeLifecyclePolicyTest.platformHiddenInsetAfterVisibleLifecycleRestoreDoesNotPersistHidden`, which failed before the follow-up fix and passed after the policy change.
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed after the follow-up fix.
  - `cd android && LINGON_IT_ONLY=keyboard_visible_before_background_is_restored_after_resume make integration-test` passed as a targeted smoke test.
  - `cd android && LINGON_IT_ONLY=keyboard_hidden_before_background_stays_hidden_after_resume make integration-test` failed after the first follow-up fix because stale visible insets could undo explicit Back dismissal, then passed after adding dismissal-in-progress handling.
  - Added `lifecycle_viewport_restore_survives_ime_height_bounce_without_new_frame`, which failed before the viewport follow-up fix with the restored camera moving from `0` to `2680.8003px`, then passed after retaining the same-frame restored viewport.
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed after the viewport follow-up fix.
  - `cd android && LINGON_IT_ONLY=keyboard_visible_before_background_is_restored_after_resume make integration-test` passed as the final smoke test after the viewport follow-up fix.
  - Physical phone confirmation passed after the viewport follow-up fix; IME stays visible across refocus and the terminal camera no longer shifts down.

### B-058 Full Android integration sweep remains unsafe on developer workstation

- Status: `needs_verification`
- Area: `android`, `integration-test`, `emulator`, `performance`, `safety`
- Summary: The unqualified Android integration sweep can still freeze the workstation even after cgroup containment, so containment and the offending tests/phases must be investigated before another full run.
- Report:
  A contained full Android integration run froze the laptop hard enough to require a reboot. The engineer explicitly prohibited further full Android suite runs until the issue is resolved beyond doubt.
- Repro:
  1. Start the full Android integration sweep.
  2. During the long 30-test connected instrumentation batch, the host can become unresponsive and require reboot.
  3. Resource artifacts from the interrupted run show the emulator/Gradle/UTP path still held roughly 5.4-6.5 GiB in the test cgroup, with the host-GPU emulator as the dominant process.
- Investigation notes:
  - The interrupted run profile sampled qemu, GradleDaemon, UTP launcher, harness, and netsimd inside the integration cgroup, with peak cgroup CPU below the 600% quota and memory below the old 8G ceiling. That makes a simple managed-process CPU cgroup escape unlikely.
  - The dominant offender was qemu: every full-run profile shows `qemu-system-x86_64` as the top CPU process.
  - The integration path was launching a heavyweight emulator: `configure-avd.sh` forced 6 vCPUs and 4096 MB RAM, and the cgroup default on a 12-core machine allowed 600% CPU. That is too much sustained qemu CPU for the workstation.
  - A software-GPU probe was not the fix: `-gpu swiftshader -no-window` passed a single targeted test but increased average cgroup CPU to 4.62 cores and CPU pressure to 55%.
  - The Android Gradle daemon also used the project-wide `-Xmx4g` heap for instrumentation invocations, which unnecessarily inflated the integration cgroup memory envelope.
- Regression coverage:
  - Managed integration AVDs are reconfigured before launch to 2 vCPUs and 2048 MB RAM by default, with `LINGON_IT_AVD_CPU_CORES` and `LINGON_IT_AVD_RAM_MB` overrides.
  - The integration cgroup default CPU quota is now capped at 200%, still lower on small machines when half the online cores is less than two full cores.
  - The integration cgroup memory ceiling defaults to 7G with swap disabled, avoiding the observed 6G pressure while staying below the old 8G ceiling.
  - The integration Gradle invocation overrides the single-use daemon heap to `-Xmx1536m` instead of the project-wide `-Xmx4g`.
- Verification:
  - Existing interrupted-run artifacts were inspected and identified qemu as the dominant CPU process while the managed emulator/Gradle/UTP/harness processes were sampled in the test cgroup.
  - `LINGON_IT_ONLY=top_bar_menu_is_accessible EMULATOR_FLAGS='-gpu swiftshader -no-window -no-snapshot' make integration-test` passed, but proved software GPU made qemu CPU pressure worse.
  - `LINGON_IT_ONLY=top_bar_menu_is_accessible LINGON_IT_CPU_QUOTA_PERCENT=200 make integration-test` passed and proved the cgroup can cap qemu to roughly two cores.
  - `LINGON_IT_ONLY=top_bar_menu_is_accessible make integration-test` passed with the new defaults: CPUQuota=200%, 2-vCPU/2048 MB AVD, 1536m Gradle daemon, peak CPU 2.12 cores, peak memory about 6.01 GiB.
  - `LINGON_IT_ONLY=terminal_updates_live make integration-test` passed with the new defaults: peak CPU 2.11 cores, average CPU 1.59 cores, peak memory about 6.53 GiB.
  - Full Android integration verification is still deferred until after review because the original failure mode was host-level unresponsiveness; targeted runs now prove the qemu CPU envelope is bounded.

### B-057 Android integration suite needs per-test cost attribution and performance fixes

- Status: `in_progress`
- Area: `android`, `integration-test`, `performance`, `e2e`
- Summary: The Android integration suite is expensive enough that we need to identify the worst CPU/time offenders and improve the test flow instead of only containing it.
- Report:
  After adding cgroup containment, the next issue is to zero in on which Android e2e tests or phases are extremely CPU expensive, then optimize the expensive paths.
- Repro:
  1. Run the contained full Android integration suite.
  2. Attribute high CPU/wall time to tests or phases, not just the aggregate run.
  3. Reduce unnecessary overhead and keep the cost visible in run artifacts.
- Regression coverage:
  - Android integration resource profiles now copy connected-test JUnit XML into the run profile directory and write a sorted `test-times.txt` file so expensive instrumentation cases are attributable from artifacts.
  - Added Android tool unit coverage for sorting JUnit timings by descending duration.
  - Added Android instrumentation geometry coverage for small, production-sized, and large terminal dimensions without relying on the default harness size.
  - Added harness unit coverage for stopping only the requested temporary host fixture.
- Verification:
  - `bash -n android/scripts/run-integration-tests.sh` passed.
  - `go test ./cmd/lingon-android-harness` passed.
  - `cd android && go test ./cmd/lingon-android-tools` passed.
  - `cd android && ./gradlew :app:compileDebugAndroidTestKotlin` passed.
  - Full Android integration verification is intentionally incomplete because B-058 makes full-suite execution unsafe until containment and offending tests/phases are investigated.

### B-056 Android integration tests can saturate the workstation

- Status: `resolved`
- Area: `android`, `integration-test`, `harness`, `emulator`, `gradle`
- Summary: The Android integration harness can consume enough CPU and memory to freeze the laptop, making the full e2e suite unsafe to run normally.
- Report:
  Running Android integration tests can make the workstation effectively unresponsive. The full stack must be contained by default, including emulator, harness, Gradle, and test collection, with a hard CPU cap around half the online cores and a memory ceiling around 8 GB.
- Repro:
  1. Run `make integration-test` from `android/`.
  2. Observe emulator/harness/Gradle load can saturate the machine.
  3. The integration runner should instead enter a systemd user scope before starting the stack, apply dynamic CPU quota and memory limits, and record resource samples for diagnosis.
- Investigation notes:
  - The existing integration script owned emulator, harness, Gradle, artifact collection, and logcat, so the containment point belongs at the start of `run-integration-tests.sh`, before any of those processes start.
  - `systemd-run --user --scope` supports the needed hard limits in this environment. Default CPU quota is computed from online CPUs as `cores * 50%`, which is half the machine because systemd expresses `CPUQuota` as percent of one core.
  - The script now defaults to a fresh managed emulator so the emulator is created inside the cgroup. Reusing an already-running device/emulator is explicit via `LINGON_IT_REUSE_DEVICE=1`.
  - Gradle now runs with `--no-daemon` and in-process Kotlin compilation for integration tests, avoiding reuse of unconstrained Gradle/Kotlin daemons outside the cgroup.
  - Cgroups cap real memory, not virtual address space. The runner enforces `MemoryMax=8G` and `MemorySwapMax=0`, and logs peak per-process VSZ separately for diagnosis.
- Regression coverage:
  - `run-integration-tests.sh` now self-wraps normal invocations in a systemd user scope unless `LINGON_IT_CGROUP=0` is set deliberately.
  - Each run writes `samples.jsonl`, `summary.txt`, and `top-processes.txt` under `android/test-artifacts/resource-profile-*`.
  - `android/Makefile` documents CPU/memory knobs and the explicit device-reuse escape hatch.
- Verification:
  - `bash -n android/scripts/run-integration-tests.sh` passed.
  - `cd android && LINGON_IT_ONLY=top_bar_menu_is_accessible make integration-test` passed with the cgroup wrapper enabled.
  - Final smoke profile: `CPUQuota=600%` on 12 online CPUs, `MemoryMax=8G`, `MemorySwapMax=0`, `peak_cpu_cores=5.75`, `peak_memory_peak_bytes=5574295552`, `peak_process_vsz_kb=12293840`.

### B-054 Review follow-up: manual viewport restore drifts across height changes

- Status: `resolved`
- Area: `android`, `terminal`, `viewport`, `lifecycle`
- Summary: Restoring a non-live/manual cached viewport after an IME or orientation height change can preserve the old top camera offset instead of preserving the saved visible content bottom edge.
- Report:
  Review found that `TerminalViewportPolicy.restoreCameraOffsetY` returns the saved Y offset unchanged for non-bottom/manual restores. If a zoomed or scrollback viewport is captured at one height and restored at another, the visible rows shift by the height delta.
- Repro:
  1. Capture a manual viewport with camera Y 350 px, viewport height 200 px, 10 px cells, and 60 rows.
  2. Restore it into the same terminal content with viewport height 150 px.
  3. The restored camera should be 400 px so the same content bottom edge remains anchored; current behavior restores 350 px.
- Investigation notes:
  - The review finding was real. The policy-level repro and Android lifecycle restore repro both failed before the fix.
  - Live-bottom restore and manual restore need different anchors: live-bottom restores to the current terminal bottom, while manual/zoomed/scrollback restore preserves the saved visible content bottom.
  - `restoreCameraOffsetY` now maps manual saved content bottom through row-space, so it preserves the same content edge across viewport height and cell-height changes, then clamps to the current terminal bounds.
- Regression coverage:
  - Added `TerminalViewportPolicyTest.restore preserves manual bottom anchor when viewport shrinks`.
  - Added `TerminalViewportPolicyTest.restore preserves manual bottom anchor when viewport grows`.
  - Added `TerminalViewportPolicyTest.restore clamps manual bottom anchor to current terminal bounds`.
  - Added `TerminalViewportPolicyTest.restore preserves manual bottom content when cell height changes`.
  - Added `EndToEndTest.lifecycle_viewport_restore_preserves_manual_bottom_anchor_across_height_change`.
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.terminal.TerminalViewportPolicyTest` failed before the fix, then passed.
  - `cd android && LINGON_IT_ONLY=lifecycle_viewport_restore_preserves_manual_bottom_anchor_across_height_change make integration-test` failed before the fix, then passed.
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed.
  - `git diff --check` passed.

### B-053 Refactor: wall polling must not expose in-flight delivery as durable cursor

- Status: `resolved`
- Area: `android`, `notifications`, `wall`, `worker`, `service`
- Summary: The wall delivery surface was too primitive: workers/services could separately load and advance the same cursor that notification delivery used for in-flight claims, allowing failed notifications to be skipped by concurrent pollers.
- Report:
  Review found that persisting event 42 as a normal cursor before `notifyWall` succeeds lets another poller observe 42 as delivered, advance past it, and make rollback skip the unposted event.
- Repro:
  1. Start a poll for event 42 and block inside `notifyWall`.
  2. While blocked, read the durable cursor.
  3. The durable cursor must remain at the previous delivered value, not 42.
  4. If event 42 fails and event 43 is blank/later in the page, the cursor must not advance past 42.
- Investigation notes:
  - The review finding was a design issue, not a narrow exception-handling bug. `WallWorkStateStore.claimDelivery` persisted in-flight notification claims into the same cursor read by poll workers/services.
  - Refactor: removed persistent delivery claims and rollback state. `WallWorkStateStore` now only stores durable delivered cursors.
  - Refactor: `MonotonicWallDeliveryCoordinator.pollOnce` owns loading the cursor, fetching one page, processing events, posting notifications, and advancing the durable cursor under one process-wide mutex.
  - `WallPollWorker` and `BackgroundWallForegroundService` now call the shared `pollOnce` page processor instead of duplicating cursor/page loops.
  - Direct websocket delivery paths still use the same coordinator mutex and only advance the durable cursor after notification delivery succeeds or in-app consumption is accepted.
- Regression coverage:
  - Added `WallDeliveryCoordinatorTest.pollDoesNotExposeInFlightNotificationAsDeliveredCursor`.
  - Added `WallDeliveryCoordinatorTest.pollDoesNotSkipFailedNotificationWhenLaterBlankEventExists`.
  - Added `WallDeliveryCoordinatorTest.concurrentPollsSerializeAndSecondPollSeesCommittedCursor`.
  - Updated `WallWorkStateStoreTest` to remove claim/rollback behavior and assert only monotonic durable cursor semantics.
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.notifications.WallDeliveryCoordinatorTest --tests systems.pkt.lingon.data.WallWorkStateStoreTest --tests systems.pkt.lingon.work.BackgroundWallForegroundServiceTest` passed.
  - `cd android && LINGON_IT_ONLY=background_manual_wall_delivery_posts_system_notification make integration-test` passed.
  - `cd android && LINGON_IT_ONLY=background_distinct_wall_messages_post_distinct_system_notifications make integration-test` passed.
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed.

### B-052 Review follow-up: wall delivery exceptions and live height-change bottom alignment

- Status: `resolved`
- Area: `android`, `notifications`, `wall`, `terminal`, `viewport`, `keyboard`
- Summary: Review identified two untested failure modes: a throwing wall notifier can leave a claimed cursor persisted without a posted notification, and live/default-zoom viewport height changes can preserve an old camera when the cursor still fits instead of snapping to the terminal bottom.
- Report:
  1. If `WallNotifier.notifyWall(...)` throws after `claimDelivery()` advances the cursor, rollback is skipped and subsequent delivery attempts treat the event as already consumed.
  2. On IME/viewport height changes, live mode should align the terminal bottom to the new viewport edge. Cursor-follow can keep the old camera if the prompt row is visible above trailing blank rows, hiding the terminal bottom behind the keyboard.
- Repro:
  1. Claim wall event 42, make `notifyWall` throw, then retry event 42. The cursor must still be retryable and previous cursor state must be restored.
  2. Render 30 terminal rows in a tall viewport with cursor row 18, then shrink the viewport to 20 cell rows in live/default-zoom mode. The camera must move to row 10 so rows 10-29 remain visible.
- Investigation notes:
  - Both review comments were real. The new wall unit tests failed before the fix because notifier exceptions escaped after the cursor claim was persisted.
  - `MonotonicWallDeliveryCoordinator.deliver` now rolls back the claim when `notifyWall` returns false or throws. Coroutine cancellation is also rolled back and rethrown, so cancellation semantics are preserved.
  - The new height-change instrumentation assertion failed before the fix with the camera still at `0.0` after shrinking the viewport. In live/default-zoom mode, height changes now use bottom alignment directly instead of cursor-follow.
- Regression coverage:
  - Added `WallDeliveryCoordinatorTest.thrownNotificationDoesNotAdvanceCursorAndAllowsRetry`.
  - Added `WallDeliveryCoordinatorTest.thrownNotificationRestoresPreviousCursor`.
  - Added `WallDeliveryCoordinatorTest.cancelledNotificationRollsBackClaimAndPropagates`.
  - Updated `EndToEndTest.keyboard_height_change_bottom_aligns_live_view_when_cursor_still_fits` to assert live bottom alignment even when the cursor row remains visible above trailing blank rows.
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.notifications.WallDeliveryCoordinatorTest` failed before the fix, then passed.
  - `cd android && LINGON_IT_ONLY=keyboard_height_change_bottom_aligns_live_view_when_cursor_still_fits make integration-test` failed before the fix, then passed.
  - `cd android && LINGON_IT_ONLY=keyboard_hide_show_preserves_bottom_anchor_for_tall_sessions make integration-test` passed.
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed.

### B-051 Review follow-up: wall alert dedupe silences distinct messages and viewport restore loses horizontal preference

- Status: `resolved`
- Area: `android`, `notifications`, `wall`, `terminal`, `viewport`, `lifecycle`
- Summary: Review identified two regressions in the previous wall notification and viewport restore changes: notification alert dedupe was tied to one constant Android notification ID, and lifecycle restore could promote a temporary horizontal cursor-follow camera into the user's preferred horizontal camera.
- Report:
  1. Distinct wall events posted while an earlier wall notification remains visible should still be allowed to alert; only retries/updates for the same wall event should be update-only.
  2. A viewport captured after horizontal cursor-follow panned right for wide output should restore the current camera for the restore frame, but must keep the user's saved horizontal preference separate so later prompts that still fit can return to that preference.
- Repro:
  1. Enable background wall notifications, background the app, send two distinct wall messages, and observe they need distinct Android notification records/IDs while each still carries update-only retry semantics.
  2. Restore a `TerminalGridView` state with a temporary `cameraOffsetXPx` and a separate saved `preferredCameraOffsetXPx`; then update with a cursor that fits the saved preference and observe the camera should return to the preference instead of staying at the temporary offset.
- Investigation notes:
  - The notification review finding was real. `setOnlyAlertOnce(true)` is correct for retries of the same event, but using one constant `notificationId` made Android treat distinct wall events as updates of the same notification.
  - Device verification also showed that relying on a numeric ID alone was insufficient for the Android-visible behavior. Wall notifications now use a stable Android notification tag plus ID derived from the wall identity. Real relay event IDs are authoritative for retry dedupe; content-derived identity is only used when the event ID is missing.
  - Review found that using the hashed integer notification ID as the tag left fallback messages vulnerable to Java `String.hashCode()` collisions. The notification tag now uses a SHA-256 digest of the full stable key, so Android's `(package, tag, id)` replacement key remains distinct even if the integer ID collides.
  - The viewport review finding was real. Capturing only `cameraOffsetXPx` made restore conflate the transient camera with the user-preferred horizontal origin.
  - `TerminalViewportState` now captures `preferredCameraOffsetXPx` separately. Restore applies the saved current camera for the restore frame but keeps the saved preferred horizontal camera for later cursor-follow decisions.
- Regression coverage:
  - Added `AndroidWallNotifierTest` coverage for stable same-event notification tags/IDs, distinct event tags/IDs, endpoint scoping, missing-event fallback identity, trimmed fallback fields, same-event retry behavior when message text changes, fallback hash collisions such as `Aa`/`BB`, and long fallback messages.
  - Added `TerminalViewportPolicyTest` coverage for restoring the saved horizontal preference, preserving temporary camera when the cursor still fits there, panning right only when needed, and clamping stale preferred offsets.
  - Added `EndToEndTest.background_distinct_wall_messages_post_distinct_system_notifications`, including assertions for distinct Android notification IDs and tags.
  - Added `EndToEndTest.lifecycle_viewport_capture_keeps_horizontal_preference_separate_from_temporary_camera`.
  - Added `EndToEndTest.lifecycle_viewport_restore_keeps_horizontal_preference_separate_from_temporary_camera`.
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.notifications.AndroidWallNotifierTest --tests systems.pkt.lingon.terminal.TerminalViewportPolicyTest` passed.
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.notifications.AndroidWallNotifierTest` passed after adding the fallback collision coverage.
  - `cd android && LINGON_IT_ONLY=lifecycle_viewport_capture_keeps_horizontal_preference_separate_from_temporary_camera make integration-test` passed.
  - `cd android && LINGON_IT_ONLY=lifecycle_viewport_restore_keeps_horizontal_preference_separate_from_temporary_camera make integration-test` passed.
  - `cd android && LINGON_IT_ONLY=background_distinct_wall_messages_post_distinct_system_notifications make integration-test` failed with only the second notification visible before explicit notification tags, then passed after the fix.
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed.

### B-050 Android wall notifications can alert twice and wall banners dismiss too quickly

- Status: `resolved`
- Area: `android`, `notifications`, `wall`, `status`
- Summary: Background wall notifications can play duplicate sounds even when Android visually replaces/deduplicates the notification, and wall status banners disappear too quickly.
- Report:
  On a recent branch build, all wall notifications appear to be visually deduped by the phone but the notification sound plays twice. Background notifications are enabled. The in-app status box also disappears too quickly and should stay visible for a couple more seconds.
- Repro:
  1. Enable background wall notifications.
  2. Background the Android app.
  3. Send a wall message.
  4. Observe one visible wall notification but duplicate notification sound.
  5. Trigger a wall status banner and observe it disappears after the previous short timeout.
- Investigation notes:
  - Android visually dedupes wall notifications by replacing the same notification ID. Replacement can still alert again unless the notification is marked `onlyAlertOnce`.
  - `AndroidWallNotifier.notifyWall()` also posted the notification and then immediately checked `activeNotifications` to decide whether delivery succeeded. That check can lag the accepted post; a false negative rolls the wall cursor back and can retry the same event, causing another alert while the visible notification is replaced.
  - Fix: wall notifications now set `onlyAlertOnce`, and `notifyWall()` treats a successful `NotificationManagerCompat.notify(...)` call as accepted. Permission-disabled and `SecurityException` paths still return false before/around the post.
  - The transient status duration was 3 seconds. It is now 5 seconds.
- Regression coverage:
  - Strengthened `background_manual_wall_delivery_posts_system_notification` to assert posted wall notifications carry `Notification.FLAG_ONLY_ALERT_ONCE`.
  - Updated `wallInactivityBannerAutoDismisses` and `wallInactivityBannerReplacementRearmsDismissTimer` to require the banner to remain visible for 4,999 ms and dismiss at 5,000 ms under the coroutine test clock.
  - Kept the existing instrumentation test `wall_inactivity_banner_auto_dismisses_without_tab_switch` as an Android-visible smoke test without wall-clock timing assertions.
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.viewmodel.AppViewModelTest.wallInactivityBannerAutoDismisses --tests systems.pkt.lingon.viewmodel.AppViewModelTest.wallInactivityBannerReplacementRearmsDismissTimer` failed before the timeout change, then passed.
  - `cd android && LINGON_IT_ONLY=background_manual_wall_delivery_posts_system_notification make integration-test` passed with the `FLAG_ONLY_ALERT_ONCE` assertion.
  - `cd android && LINGON_IT_ONLY=wall_inactivity_banner_auto_dismisses_without_tab_switch make integration-test` passed.
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed.

### B-049 Review follow-up: live-bottom viewport restore loses rows added while stopped

- Status: `resolved`
- Area: `android`, `terminal`, `viewport`, `lifecycle`
- Summary: Restoring a viewport captured at live bottom could fail to remain at live bottom if terminal output added rows between capture and restore.
- Report:
  A cached viewport captured at live bottom for N rows was compared against the bottom offset for the current row count during restore. If rows advanced while the app was stopped or syncing, the saved camera looked like a manual non-bottom offset and live auto-follow was suppressed for the restore frame.
- Repro:
  1. Render a `TerminalGridView` with 60 rows and capture viewport state at live bottom.
  2. Update the snapshot to 61 rows before restoring the captured viewport state.
  3. Observe the camera restores to the old 60-row bottom instead of the current 61-row bottom.
- Investigation notes:
  - The review finding was real. Both the policy-level test and the Android view-level instrumentation test failed before the fix.
  - `TerminalViewportState` had viewport height and scaled cell height, but not the captured row count, so `TerminalViewportPolicy.restoreCameraOffsetY` could not distinguish saved live-bottom from manual offset after row-count changes.
  - Fix: store captured `totalRows` in `TerminalViewportState`; use saved row count to detect whether the saved camera represented live bottom, and use current row count to compute the restored live-bottom camera.
- Regression coverage:
  - Added `TerminalViewportPolicyTest.restore preserves live bottom when row count advanced after capture`.
  - Added `TerminalViewportPolicyTest.restore preserves manual camera when row count advanced after capture`.
  - Added `EndToEndTest.lifecycle_viewport_restore_preserves_live_bottom_when_rows_advance`.
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.terminal.TerminalViewportPolicyTest` failed before the fix, then passed.
  - `cd android && LINGON_IT_ONLY=lifecycle_viewport_restore_preserves_live_bottom_when_rows_advance make integration-test` failed before the fix, then passed.
  - `cd android && LINGON_IT_ONLY=lifecycle_viewport_restore_preserves_live_bottom_across_height_change make integration-test` passed.
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed.
  - `cd android && LINGON_IT_ONLY=keyboard_hide_show_preserves_bottom_anchor_for_tall_sessions make integration-test` passed.

### B-048 Review follow-up: viewport restore height drift and wall notification delivery race

- Status: `resolved`
- Area: `android`, `terminal`, `viewport`, `notifications`, `wall`
- Summary: Review identified two regressions: restoring a cached live-bottom viewport across IME-height changes could preserve the old camera exactly, and concurrent wall delivery coordinators could treat an in-flight failed notification as consumed.
- Report:
  1. A viewport captured at live bottom with one viewport height can be restored after the IME changes the viewport height. Restoring the old `cameraOffsetYPx` exactly leaves the terminal above the current live bottom.
  2. Two wall delivery paths can race. One path claims the cursor and blocks/fails in `notifyWall`; another path observes the cursor as already advanced and returns success, allowing workers to advance/skip without a posted notification.
- Repro:
  1. Create a `TerminalGridView` with overflowing live content, draw it at live bottom, capture viewport state, shrink the view height, restore the captured state, and assert the camera is at the new live bottom.
  2. Start delivery through one coordinator with a blocking failing notifier, start delivery through a second coordinator for the same event, and assert the second delivery does not complete as consumed while the first claim is in flight.
- Investigation notes:
  - Both review comments were relevant. The new tests failed before the fixes.
  - `TerminalViewportState` captured `viewportHeightPx` but not scaled cell height, so restore could not reliably identify that a saved camera represented live bottom after IME-related scale/height changes.
  - `MonotonicWallDeliveryCoordinator` used an instance mutex, so two coordinator instances could observe the same cursor claim concurrently through the shared store.
- Regression coverage:
  - Added `EndToEndTest.lifecycle_viewport_restore_preserves_live_bottom_across_height_change`.
  - Added `WallDeliveryCoordinatorTest.inFlightFailedDeliveryThroughSeparateCoordinatorIsNotConsumed`.
- Verification:
  - `cd android && LINGON_IT_ONLY=lifecycle_viewport_restore_preserves_live_bottom_across_height_change make integration-test` failed before the fix, then passed.
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.notifications.WallDeliveryCoordinatorTest.inFlightFailedDeliveryThroughSeparateCoordinatorIsNotConsumed` failed before the fix, then passed.
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed.
  - `cd android && LINGON_IT_ONLY=keyboard_hide_show_preserves_bottom_anchor_for_tall_sessions make integration-test` passed.

### B-047 Android IME toggle breaks live bottom alignment

- Status: `resolved`
- Area: `android`, `terminal`, `keyboard`, `viewport`
- Summary: Toggling the Android IME between hidden and visible can leave the live terminal camera misaligned from the bottom of the screen or keyboard.
- Report:
  When going from hidden IME keyboard to visible IME keyboard, the terminal bottom is no longer aligned correctly. With IME hidden, live output should align to the screen bottom; with IME visible, live output should align to the keyboard top. Existing screenshot-only coverage did not assert this.
- Repro:
  1. Load enough terminal output that the live terminal overflows the Android viewport.
  2. Hide the IME and assert the live viewport ends at the final terminal row.
  3. Show the IME and assert the live viewport still ends at the final terminal row above the keyboard.
  4. Observe stale viewport restore can override the height-change bottom anchor.
- Investigation notes:
  - Reproduced the missing coverage first: the existing visual hide/show and tab-switch tests only captured screenshots and did not assert the live camera offset against the bottom-aligned pixel offset.
  - A first tall-session attempt using a shell fixture was invalid because the harness registered the session but never produced a usable first frame. A second attempt using the default slow echo fixture was also invalid because the cursor still fit near the top, so bottom-follow was not the correct expectation.
  - Added a harness `initial_lines` fixture that creates real PTY output before the host echo loop, so the Android test can deterministically load a terminal where the live bottom is below the viewport.
  - The production bug was the viewport restore effect treating ordinary IME visibility changes as restore events. That let cached viewport state override the terminal view's height-change bottom anchor when toggling keyboard visibility.
  - While verifying adjacent lifecycle behavior, the IME focus path also needed cancellable delayed keyboard-show attempts so hiding the keyboard invalidates pending `showSoftInput` retries.
- Regression coverage:
  - Added pixel-level live-bottom assertions to `keyboard_hide_show_preserves_bottom_anchor_visual`.
  - Added pixel-level live-bottom assertions to each hop in `keyboard_tab_switch_preserves_bottom_anchor_visual`.
  - Added `keyboard_hide_show_preserves_bottom_anchor_for_tall_sessions`, which creates two tall host PTY sessions with deterministic initial output, toggles the IME hidden/visible per session, and verifies cached cross-session restores remain bottom-aligned.
  - Kept IME lifecycle coverage split into the existing hidden/visible background-resume tests, and tightened the camera refocus test so it asserts camera preservation independently from IME state.
- Verification:
  - `cd android && LINGON_IT_ONLY=keyboard_hide_show_preserves_bottom_anchor_for_tall_sessions make integration-test` passed.
  - `cd android && LINGON_IT_ONLY=keyboard_hide_show_preserves_bottom_anchor_visual make integration-test` passed.
  - `cd android && LINGON_IT_ONLY=keyboard_tab_switch_preserves_bottom_anchor_visual make integration-test` passed.
  - `cd android && LINGON_IT_ONLY=keyboard_hidden_before_background_stays_hidden_after_resume make integration-test` passed.
  - `cd android && LINGON_IT_ONLY=keyboard_visible_before_background_is_restored_after_resume make integration-test` passed.
  - `cd android && LINGON_IT_ONLY=focused_background_resume_preserves_live_camera_when_cursor_still_fits make integration-test` passed.
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed.
  - `go test ./cmd/lingon-android-harness` passed.

### B-046 Android IME state is not preserved across app refocus

- Status: `resolved`
- Area: `android`, `terminal`, `keyboard`, `lifecycle`
- Summary: Refocusing the Android app can automatically show the terminal IME even when the user hid it before backgrounding.
- Report:
  If the keyboard is toggled off, then Lingon is unfocused or closed and later refocused, the keyboard is automatically toggled back on without tapping the terminal. The IME visibility state should persist across app unfocus/refocus: hidden stays hidden, visible stays visible.
- Repro:
  1. Focus the terminal so the IME controls are visible.
  2. Hide the keyboard.
  3. Background/unfocus the app.
  4. Refocus the app.
  5. Observe the keyboard returns even though it was hidden before unfocus.
- Investigation notes:
  - `TerminalScreen` unconditionally requested terminal input focus on lifecycle `ON_START` and active-session changes.
  - That forced the IME visible after resume regardless of whether the user had hidden it before backgrounding.
  - Fix: automatic focus restore is now gated by captured foreground IME visibility. Initial startup and "IME was visible" still request focus; "IME was hidden" does not.
  - The first attempted fix still failed because the hidden input view kept focus and Android restored the IME on resume by itself. The final fix records foreground IME visibility into `AppViewModel` state and explicitly clears terminal input focus/hides IME when the saved state is hidden.
- Regression coverage:
  - `EndToEndTest.keyboard_hidden_before_background_stays_hidden_after_resume`
  - `EndToEndTest.keyboard_visible_before_background_is_restored_after_resume`
- Verification:
  - `cd android && LINGON_IT_ONLY=keyboard_hidden_before_background_stays_hidden_after_resume make integration-test` failed before the final fix, then passed.
  - `cd android && LINGON_IT_ONLY=keyboard_visible_before_background_is_restored_after_resume make integration-test`
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin`

### B-045 Android zoomed terminal keeps prompt cropped after wide output

- Status: `needs_verification`
- Area: `android`, `terminal`, `viewport`, `horizontal-pan`
- Summary: In a zoomed Android terminal, wide command output can pan the camera right and leave the next prompt cropped on the left even though the cursor would fit if the camera returned to the user's left-aligned position.
- Report:
  Running a wide command such as `ps aux` in a zoomed Android terminal can move the camera several characters to the right. When the shell returns to the prompt, the prompt text on the left remains cropped even though the cursor/prompt would fit with horizontal camera alignment restored.
- Repro:
  1. Open an Android terminal and zoom in so horizontal panning is possible.
  2. Start with the horizontal camera aligned all the way left.
  3. Produce wide output that moves the cursor/right edge far enough to pan the camera right.
  4. Return to a prompt near the left edge.
  5. Observe the prompt remains cropped because the horizontal follow policy keeps a nonzero left-margin offset instead of restoring camera X to zero.
- Investigation notes:
  - The horizontal cursor-follow policy returns `cursorLeft - margin` when the cursor is left of the current viewport.
  - For prompt positions near the left edge, that can be a positive camera offset even though the cursor would fit with camera X restored to zero.
  - Fix: TerminalGridView now tracks a preferred horizontal camera offset separately from temporary cursor-follow movement. Horizontal follow restores that preferred offset when the cursor fits there, so both left-aligned `x=0` and user-panned positions such as `x=10` are preserved.
- Regression coverage:
  - `TerminalViewportPolicyTest.horizontal cursor follow restores left edge when cursor fits from origin`
  - `TerminalViewportPolicyTest.horizontal cursor follow restores user panned edge when cursor fits there`
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.terminal.TerminalViewportPolicyTest`
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin`
  - Pending phone-visible verification with zoomed `ps aux` / wide-output prompt return.

### B-044 Android refocus/reconnect shifts terminal camera without new output

- Status: `needs_verification`
- Area: `android`, `terminal`, `viewport`, `lifecycle`
- Summary: Refocusing or reconnecting the Android app can move the terminal camera back to cursor-follow placement even when no new terminal content requires a follow adjustment.
- Report:
  After the keyboard typing case was fixed, starting/refocusing Lingon still rearranges the terminal camera to the same forced placement as before. Refocus/reconnect must preserve the saved camera view unless a real terminal delta/replay changes the cursor/content so the cursor no longer fits.
- Repro:
  1. Render a live terminal viewport and save its camera state.
  2. Simulate lifecycle restore/refocus with the same terminal frame and cursor state.
  3. Observe that the restore path can apply live cursor-follow behavior even though no new terminal output arrived.
- Investigation notes:
  - The lifecycle restore path reused the default live cursor-follow/bottom-align logic. That is correct for new terminal output, but not for restoring a previously captured camera after app refocus.
  - Restore must treat the captured camera as authoritative, then only future cursor movement from later frames can request cursor follow.
  - Fix: viewport restore now restores the saved camera directly, seeds the current cursor as already observed, and suppresses live auto-follow for the restored frame sequence only.
  - Follow-up: this did not resolve the phone-visible refocus bug. The original regression only exercised the lower-level view restore path, not the full focused Android lifecycle path.
  - Follow-up fix: TerminalScreen now re-applies the cached viewport once per lifecycle and IME visibility state. This covers the likely phone timing where restore ran before the focused/keyboard-visible layout settled and was not retried.
- Regression coverage:
  - `EndToEndTest.lifecycle_viewport_restore_preserves_saved_camera_without_new_frame`
  - `EndToEndTest.focused_background_resume_preserves_live_camera_when_cursor_still_fits`
- Verification:
  - `cd android && LINGON_IT_ONLY=lifecycle_viewport_restore_preserves_saved_camera_without_new_frame make integration-test` failed before the fix with `expected:<0.0> but was:<1266.3601>`, then passed after the fix.
  - `cd android && LINGON_IT_ONLY=keyboard_height_change_preserves_live_camera_when_cursor_still_fits make integration-test`
  - `cd android && ./gradlew :app:testDebugUnitTest`
  - `cd android && ./gradlew :app:compileDebugAndroidTestKotlin`
  - `cd android && LINGON_IT_ONLY=focused_background_resume_preserves_live_camera_when_cursor_still_fits make integration-test`
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin`
  - Pending phone-visible verification because the earlier fix did not resolve the physical device report.

### B-043 Android wall notifications can duplicate for one wall event

- Status: `resolved`
- Area: `android`, `notifications`, `wall`, `background`
- Summary: Android can show duplicate Lingon wall notifications for the same wall message even when the relay only sent one event.
- Report:
  The Android notification history showed two identical Lingon notifications with the same title, body, and timestamp. No duplicate wall message was sent, so the app must prevent duplicate system notification posts for the same wall event.
- Repro:
  1. Enable background wall notifications.
  2. Let separate Android background delivery paths observe the same wall event concurrently.
  3. Both paths can pass the cursor check before either records delivery.
  4. Observe duplicate system notification posts for the same message.
- Investigation notes:
  - Existing regression coverage only checked that one coordinator instance suppresses replay. Its per-instance mutex hid the production race.
  - Production can construct separate coordinators sharing the same persisted wall cursor. The old delivery path read `shouldDeliver`, posted the notification, and only then recorded the cursor, leaving a race window across coordinator instances.
  - Fix: delivery now claims the event atomically in the shared wall cursor store before posting the notification. If posting fails, the claim is rolled back unless a newer cursor has already advanced.
- Regression coverage:
  - `WallDeliveryCoordinatorTest.concurrentDeliveryThroughSeparateCoordinatorsPostsOnlyOnce`
  - `WallWorkStateStoreTest.deliveryClaimAtomicallySuppressesReplay`
  - `WallWorkStateStoreTest.deliveryClaimRollbackRestoresPreviousCursor`
  - `WallWorkStateStoreTest.deliveryClaimRollbackDoesNotMoveNewerCursorBackward`
  - `EndToEndTest.background_manual_wall_delivery_posts_system_notification` now asserts exactly one active notification for the wall message.
  - `EndToEndTest.background_manual_wall_delivery_does_not_repost_previous_message` now asserts exactly one active notification for the second wall message.
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.notifications.WallDeliveryCoordinatorTest --tests systems.pkt.lingon.data.WallWorkStateStoreTest`
  - `cd android && LINGON_IT_ONLY=background_manual_wall_delivery_posts_system_notification make integration-test`
  - `cd android && ./gradlew :app:testDebugUnitTest`
  - `cd android && ./gradlew :app:compileDebugAndroidTestKotlin`

### B-042 Android keyboard input snaps live camera even when cursor still fits

- Status: `resolved`
- Area: `android`, `terminal`, `viewport`, `keyboard`
- Summary: When the Android keyboard appears and the live cursor is still visible in the current camera viewport, the terminal must preserve the camera instead of bottom-aligning and cropping content unnecessarily.
- Report:
  With the terminal scrolled to the live bottom, starting to type opens the keyboard. Even though the cursor and input row still fit in the visible terminal viewport, the app shifts the camera downward so the cursor sits just above the new camera bottom, cropping content that should remain visible.
- Repro:
  1. Open an Android terminal session with enough visible rows that the prompt/cursor fits before keyboard input.
  2. Scroll/pan to the live bottom.
  3. Start typing so the keyboard appears.
  4. Observe the camera snaps downward even though the cursor row was already visible and did not need auto-follow.
- Investigation notes:
  - The Android terminal view bottom-aligns on viewport height changes whenever live auto-follow is eligible.
  - That is too aggressive for keyboard entry: live follow should move vertically only when the cursor row no longer fits in the current camera viewport.
  - Fix: vertical cursor follow now preserves the current camera when the cursor row is already fully visible, and scrolls only the minimum amount needed to reveal the cursor when it is above or below the viewport.
- Regression coverage:
  - `TerminalViewportPolicyTest.vertical cursor follow preserves camera when cursor already fits`
  - `EndToEndTest.keyboard_height_change_preserves_live_camera_when_cursor_still_fits`
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.terminal.TerminalViewportPolicyTest`
  - `cd android && LINGON_IT_ONLY=keyboard_height_change_preserves_live_camera_when_cursor_still_fits make integration-test`

### B-041 Codex v0.129.0 hangs on repeated start in host local PTY

- Status: `resolved`
- Area: `host`, `pty`, `terminal`, `escape-sequences`
- Summary: Codex starts in a Lingon host local PTY once, then hangs on a second start in the same session. Earlier workaround attempts also regressed CPR row reporting and Codex prompt styling.
- Report:
  Codex v0.129.0 must start reliably inside Lingon local PTY sessions. A prior single-start regression was insufficient: Codex can start the first time and then hang on the second start. CPR must continue to report the actual cursor row where Codex starts; forcing or implying row 1 is a regression. OSC color probing must also continue to work so Codex keeps its expected prompt styling, including the gray prompt background.
- Repro:
  1. Start a Lingon host local PTY session.
  2. Launch Codex v0.129.0 and wait for the UI to draw.
  3. Exit Codex back to the same shell.
  4. Launch Codex again in that same PTY.
  5. Observe the second start hangs. Also verify Codex startup receives the actual cursor row and OSC 10/11 responses.
- Investigation notes:
  - CPR/DSR, DA, and OSC color query support must not be suppressed globally or for Codex startup. These replies are part of the expected xterm-compatible terminal surface.
  - Lingon must decline unsupported kitty keyboard enhancement without falsely advertising support, but the fallback xterm probe surface still has to work.
  - Root cause: Lingon kept the PTY slave fd open in the host after starting the child. Codex v0.129.0 duplicates stdin for its startup cursor probe, sets that fd nonblocking, reads the CPR, then expects the next read to return `EAGAIN`. With Lingon's host-side slave fd still open, the probe could block before parsing the CPR it already received.
  - Fix: capture the slave-side startup state Lingon needs, then close the host copy of the PTY slave so child programs own the terminal side. Post-start termios operations use the PTY master fd instead of retaining the slave fd. Unsupported keyboard enhancement controls are ignored/declined without sending a kitty `CSI ? ... u` status response.
- Regression coverage:
  - `TestRespondToTerminalQueriesCodexStartupProbeUsesXtermFallback` drives the Codex-style startup control sequence directly against the query responder and asserts Lingon declines kitty keyboard enhancement while still returning CPR and DA xterm fallback replies.
  - `TestRespondToTerminalQueriesXtermStatusAndAttributes` asserts DSR 5, CPR 6, private DSR, DA1, and DA2 reply with the supported xterm-compatible responses.
  - `TestFilterOSCOutputRespondsToColorQueriesAndSuppressesQueryBytes` asserts OSC 10/11/12 color queries get replies and do not leak query bytes into rendered output.
  - `TestFilterOSCOutputUsesCapturedOSCDefaults` asserts captured terminal color defaults are used for OSC color query replies.
  - Existing `TestAttachCtrlDDoesNotExitHost` covers remote Ctrl-D/EOF behavior after the PTY slave fd is closed.
- Verification:
  - `go test -count=1 ./internal/session`
  - `go test ./...`
  - `go vet ./...`
  - `golint ./...`
  - `golangci-lint run ./...`

### B-040 Android ANSI blue is still too dark

- Status: `resolved`
- Area: `android`, `terminal`, `palette`
- Summary: Normal non-bold ANSI blue in the Android terminal remains hard to see against the dark terminal background.
- Report:
  After the earlier palette lightening, normal non-bold blue text in the Android app is still too dark and difficult to read.
- Repro:
  1. Render terminal text using normal ANSI blue, index 4.
  2. Compare legibility against the Android terminal's black/dark background.
  3. Observe that normal, non-bold blue is hard to see.
- Investigation notes:
  - Android's default ANSI palette mapped normal blue to `#6A9BFF`.
  - Bright blue still used raw `#0000FF`, which is also poor on the Android terminal background.
  - Follow-up candidates `#9AB6FF`/`#B4C8FF` and `#7CC7FF`/`#B8E0FF` still read too dark or not blue enough in terminal samples.
  - The palette now maps normal blue to `#78B4FF` and bright blue to `#A0CDFF`.
- Regression coverage:
  - `TerminalPaletteTest.defaultAnsiPaletteUsesBrighterReadableBaseColors`
  - `TerminalPaletteTest.resolveColorUsesReadableAnsiBlue`
  - `TerminalPaletteTest.defaultAnsiPaletteUsesReadableBrightBlue`
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.terminal.TerminalPaletteTest`
  - `cd android && ./gradlew :app:testDebugUnitTest`

### B-039 Wall modal repaint ignores scrollback viewport

- Status: `resolved`
- Area: `host`, `attach`, `tui`, `scrollback`, `wall`
- Summary: Showing a modal wall notification while browsing scrollback repaints the underlying terminal content from the live/end viewport instead of the current scrollback position.
- Report:
  When a modal wall notification appears while browsing the scrollbuffer in a Lingon host or attach session, the content behind the modal appears to jump to the end/live content. The scrollback cursor is still at the original position; after navigating again, the viewed position becomes apparent, so the bug is in the modal/background render rather than scrollback state.
- Repro:
  1. Open a Lingon host or attach TUI session with enough output to browse scrollback.
  2. Navigate into the scrollback buffer away from live output.
  3. Receive a modal wall notification.
  4. Observe the terminal content behind the modal displays live/end content instead of the scrollback viewport currently being viewed.
  5. Continue navigating scrollback and observe the cursor/position was not actually reset.
- Investigation notes:
  - Host wall display used `forceRedrawWithMode` directly after applying `WallAction`.
  - That path renders the active local session's live snapshot and bypasses `forceRedraw`, which is the scrollback-aware entry point.
  - The scrollback viewport state was not reset; only the wall modal repaint was composed over the wrong base snapshot, matching the reported pseudo-jump.
  - Attach already routed wall display through `RenderCurrent`, which rebuilds the scrollback view before composing overlays. A dedicated attach regression now asserts this behavior stays intact.
  - Host wall show and wall expiry redraw now use `forceRedrawRespectingScrollback`, which renders the current scrollback viewport when active and preserves the existing force-full behavior for live views.
- Regression coverage:
  - `TestRunnerShowWallPreservesScrollbackViewport` reproduces the host bug using an isolated test PTY: it first renders an active scrollback viewport, then shows a wall modal and asserts the repaint does not emit the live-only token.
  - `TestRunnerShowWallPreservesMixedScrollbackAndLiveViewport` covers the boundary case where history and live rows are visible together and asserts the modal repaint does not drift to deeper live/end content outside the viewed range.
  - `TestWallOverlayPreservesScrollbackViewport` covers the attach path so wall overlays remain composed over the active scrollback viewport there too.
  - `TestWallOverlayPreservesMixedScrollbackAndLiveViewport` applies the same mixed history/live viewport assertion to attach.
- Verification:
  - `go test -count=1 ./internal/session -run TestRunnerShowWallPreservesScrollbackViewport` failed before the fix with `LIVE-END-TOKEN` emitted during the wall repaint.
  - `go test -count=1 ./internal/session ./internal/attach -run 'TestRunnerShowWallPreservesScrollbackViewport|TestWallOverlayPreservesScrollbackViewport|TestWallAutoHideDoesNotForceFullRedraw'`
  - `go test -count=1 ./internal/session ./internal/attach -run 'TestRunnerShowWallPreservesScrollbackViewport|TestRunnerShowWallPreservesMixedScrollbackAndLiveViewport|TestWallOverlayPreservesScrollbackViewport|TestWallOverlayPreservesMixedScrollbackAndLiveViewport'`
  - `go test -count=1 ./internal/session ./internal/attach ./internal/mvu`

### B-038 Android scrollback pan jumps and switches to row stepping

- Status: `resolved`
- Area: `android`, `terminal`, `scrollback`, `gestures`
- Summary: Android terminal panning jumps when crossing between the live viewport and scrollback, and scrollback movement becomes line-by-line instead of pixel-continuous.
- Report:
  The Android app still jumps when panning back toward the live screen. Panning is smooth while moving around the live screen, but once the gesture hits scrollback it transitions to row-stepped movement, making the boundary visibly discontinuous.
- Repro:
  1. Open an Android terminal session with enough output to have scrollback.
  2. Zoom in so vertical pan is pixel-continuous in the live screen.
  3. Pan past the live screen into scrollback.
  4. Observe the view advances by whole rows instead of continuing pixel-by-pixel.
  5. Pan back toward the live screen.
  6. Observe a visible jump or loss of position at the scrollback/live handoff.
- Investigation notes:
  - The view already pans by pixels inside the currently rendered snapshot, but entering older scrollback was gated by a whole-cell overflow accumulator.
  - `buildScrollbackSnapshot` returned a fixed-height live-sized window, so even when the app fetched another scrollback row there was no extra rendered row available for fractional movement across the boundary.
  - The fix makes positive scrollback offsets render the requested history rows plus the full live snapshot, keeps fit-to-view scaling based on the live host row count, and requests the first scrollback row as soon as a fractional pan crosses the top boundary.
  - The viewport then applies the pending fractional overflow when the matching scrollback snapshot arrives, preserving pixel position instead of snapping to the next whole row.
  - A second reproduced failure mode happened when the user crossed the live/scrollback boundary, requested a scrollback row, then reversed direction before that scrollback snapshot arrived. The stale pending scrollback-entry amount was still applied to the late snapshot, which shifted the camera back to the earlier boundary position and produced the visible jump.
  - The view now reduces pending scrollback-entry pixels when a later drag moves back down inside the still-live snapshot. If the delayed scrollback snapshot then arrives, only the remaining pending entry amount is applied, so the current visual position is preserved.
  - A third reproduced failure mode required the actual keyboard-visible, manually zoomed Android UI path. When panning back from one loaded scrollback row into live output, the scrollback offset dropped from `1` to `0`; that render frame made the cursor look moved and re-enabled cursor auto-follow, snapping the zoomed camera to the live bottom.
  - The view now suppresses cursor auto-follow for the render frame that consumes pending live re-entry rows. Normal input-driven cursor follow remains intact, but scrollback row removal can no longer turn a pan-back gesture into a bottom snap.
- Regression coverage:
  - Added `zoomed_scrollback_entry_preserves_pixel_pan_before_row_boundary` to the Android integration suite. It drags less than one cell into scrollback and asserts a row is requested immediately, then verifies the camera lands at `cellHeight - partialPanPx` after the scrollback snapshot arrives.
  - Added `zoomed_scrollback_entry_reverse_before_snapshot_preserves_live_position`, which reproduces the reverse-before-snapshot jump. It failed before the fix with `expected:<117.8793> but was:<87.318>`, then passed after pending scrollback-entry cancellation was added.
  - Added `keyboard_visible_scrollback_entry_reverse_before_snapshot_preserves_live_position`, which runs the same reverse-before-snapshot scenario with `imeVisible = true` and width-fit scaling enabled so the keyboard-visible render path is covered explicitly.
  - Added `keyboard_visible_zoomed_scrollback_entry_without_width_fit_preserves_live_position`, which keeps `imeVisible = true`, `fitToViewWidth = false`, and manual zoom enabled to match the reported keyboard-up/manual-zoom case.
  - Added `keyboard_visible_zoomed_pan_across_scrollback_boundary_is_continuous`, which drives the real Android UI with keyboard visible, manual zoom enabled, enough output for scrollback, and pan pulses across the live/scrollback boundary. It failed before the fix with a jump from live row `-0.09` to `9.36`, then passed after suppressing cursor follow during live re-entry.
  - Updated `zoomed_scrollback_live_reentry_waits_for_matching_snapshot` to use a real scrollback-expanded snapshot so the live re-entry side remains covered.
  - Added `ScrollbackSnapshotTest` coverage proving scrollback snapshots prepend requested history while preserving the full live snapshot.
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.terminal.ScrollbackSnapshotTest --tests systems.pkt.lingon.terminal.TerminalViewportPolicyTest`
  - `cd android && ./gradlew :app:compileDebugAndroidTestKotlin`
  - `cd android && LINGON_IT_ONLY=zoomed_scrollback_entry_preserves_pixel_pan_before_row_boundary make integration-test`
  - `cd android && LINGON_IT_ONLY=zoomed_scrollback_live_reentry_waits_for_matching_snapshot make integration-test`
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin`
  - `cd android && LINGON_IT_ONLY=zoomed_scrollback_entry_reverse_before_snapshot_preserves_live_position make integration-test`
  - `cd android && LINGON_IT_ONLY=keyboard_visible_scrollback_entry_reverse_before_snapshot_preserves_live_position make integration-test`
  - `cd android && LINGON_IT_ONLY=keyboard_visible_zoomed_scrollback_entry_without_width_fit_preserves_live_position make integration-test`
  - `cd android && LINGON_IT_ONLY=zoomed_scrollback_entry_preserves_pixel_pan_before_row_boundary make integration-test`
  - `cd android && LINGON_IT_ONLY=zoomed_scrollback_live_reentry_waits_for_matching_snapshot make integration-test`
  - `cd android && LINGON_IT_ONLY=keyboard_visible_zoomed_pan_across_scrollback_boundary_is_continuous make integration-test` failed before the fix, then passed after the fix.

### B-037 Android integration harness temp roots leak after test runs

- Status: `resolved`
- Area: `android`, `integration-tests`, `harness`
- Summary: Android integration test runs left `/tmp/lingon-android-harness-*` directories behind after successful smoke runs.
- Report:
  After `make test-android`, temporary harness roots remained under `/tmp`. Manual cleanup removes the symptom, but the runner must clean up after itself.
- Repro:
  1. Run `make test-android`.
  2. Check `/tmp` for `lingon-android-harness-*` directories.
  3. No harness temp roots should remain after the script exits.
- Investigation notes:
  - The harness binary attempts to remove its own root from `h.stop()`, but the shell runner only signaled the harness and assumed the process cleanup completed.
  - The runner now derives each active harness root from the generated CA path in the harness config.
  - The runner removes the active root after each `stop_harness` call and also removes all remembered harness roots in the global `EXIT` cleanup.
  - Removal is constrained to directories named `lingon-android-harness-*` under `/tmp` or `/var/tmp`.
- Regression coverage:
  - `TestHarnessStopRemovesTempRoot` covers the binary cleanup path.
  - Shell syntax and end-to-end Android smoke verify the runner cleanup path.
- Verification:
  - `bash -n android/scripts/run-integration-tests.sh`
  - `make test-android`
  - `find /tmp -maxdepth 1 -type d -name 'lingon-android-harness-*' -print` returned no directories after the run.

### B-036 Attach reconnect replay cursor and live broadcast race

- Status: `resolved`
- Area: `attach`, `relay`, `reconnect`
- Summary: Reconnecting attach clients must never drop unsequenced readiness frames, replay too much terminal history, or receive duplicate live/replayed frames during the reconnect handshake.
- Report:
  Review found two replay/reconnect risks. First, cached reconnects use a nonzero replay cursor, so welcome/control readiness frames with `seq=0` must not be rejected by receive-side sequence filtering. Second, the relay registers a reconnecting websocket as a live broadcast recipient before replay has completed, allowing a host frame to be delivered live and then replayed for the same `last_seq`.
- Repro:
  1. Seed an attach client with a cached snapshot and nonzero `lastSeq`.
  2. Deliver an unsequenced control or welcome readiness frame.
  3. The client must accept the frame and become ready rather than treating it as stale replay data.
  4. Register a reconnecting relay client with `last_seq=N`, publish host frame `N+1` before replay completes, and process hello replay.
  5. The reconnecting client must receive frame `N+1` exactly once.
- Investigation notes:
  - `acceptSeq(0)` already accepted unsequenced readiness frames before consulting `lastSeq`; a regression test now locks that invariant so cached reconnects can keep a replay cursor without dropping welcome/control readiness.
  - The relay websocket handler now registers handshake clients as pending. Pending clients remain known for replacement/control bookkeeping, but live host/session/control broadcasts skip them until their hello handling completes.
  - `HandleClientFrame` activates pending clients under the hub lock after the replay decision is fenced and before live broadcasts can observe the client, so frames recorded before the decision are replayed and frames recorded after it are delivered live.
  - This keeps replay bounded to frames after `last_seq`; the fix does not widen replay or replay from zero.
- Regression coverage:
  - `TestUnsequencedReadinessFramesBypassReplaySequenceCursor`
  - `TestHubPendingReconnectDoesNotReceiveLiveFrameBeforeReplay`
- Verification:
  - `go test -count=1 ./internal/attach ./internal/relay`
  - `make test`
  - `make test-webui`
  - `go test -count=1 -tags integration ./integration/...`
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin`
  - `make test-android` (one expected notification-permission recovery skip; zero failures)
  - `go vet ./...`
  - `golangci-lint run ./...`
  - `golint ./...`

### B-035 WebUI integration suite fails under concurrent smoke load

- Status: `resolved`
- Area: `webui`, `integration-tests`
- Summary: `make test-webui` exited nonzero during release smoke when run concurrently with the full Go test suite, even though a standalone rerun passed.
- Report:
  The release smoke run reported a webui failure while `make test` and `make test-webui` were running in parallel. The visible tail showed only passing tests, so the failing test or package output was hidden by truncation.
- Repro:
  1. Run `make test` and `make test-webui` concurrently.
  2. Observe `make test-webui` may exit nonzero.
  3. Rerun `make test-webui` alone; it may pass, suggesting a test isolation or resource-contention issue.
- Investigation notes:
  - The exact failure from the release smoke was not recoverable from the truncated visible output.
  - `make test` and `make test-webui` both write to `TEST_LOG` by default, so concurrent manual smoke runs can obscure diagnostics in `test.log`.
  - Reproduced the concurrent load pattern with isolated `TEST_LOG` files across three runs; both the broad Go suite and webui suite passed each time.
  - Reproduced the concurrent load pattern with the default shared `test.log` across three runs; both suites passed each time.
  - Ran the webui integration package ten times in one process; all iterations passed.
  - Searched the current webui JSON/text logs for failing actions and `FAIL`; no failures were present.
- Regression coverage:
  - No code change was made because the failure was not reproducible and no broken webui test was found.
- Verification:
  - `make TEST_LOG=/tmp/lingon-go-parallel.json test` and `make TEST_LOG=/tmp/lingon-webui-parallel.json test-webui` concurrently.
  - Three concurrent isolated-log iterations of `make test` plus `make test-webui`.
  - Three concurrent default shared-log iterations of `make test` plus `make test-webui`.
  - `go test -count=10 -tags integration ./integration/webui`

### B-034 Android zoomed scrollback pan jumps during live reentry

- Status: `resolved`
- Area: `android`, `terminal`, `scrollback`
- Summary: Panning down from scrollback/history toward the live screen in the Android terminal can jump abruptly and lose the user's position.
- Report:
  The Android app regressed smooth panning between history and the current live screen. When dragging back down toward the live viewport, the pan position makes a large jump instead of moving continuously.
- Repro:
  1. Open an Android terminal session with enough output to populate scrollback.
  2. Zoom in so the terminal view pans inside the snapshot.
  3. Enter scrollback/history and drag down toward the live screen.
  4. Observe the visible position jumps during the handoff from scrollback rows back into the live snapshot.
- Investigation notes:
  - `TerminalGridView` asks the ViewModel to reduce `scrollbackOffsetRows` when zoomed panning crosses from history toward live output.
  - The current view also subtracts the consumed rows from its local camera immediately. That can draw the old snapshot with the new camera before Compose delivers the matching new scrollback snapshot, creating a visible discontinuity.
  - The fix records pending live-reentry rows when the gesture requests the ViewModel scrollback change, but leaves the current camera untouched until `update(...)` receives the matching lower `scrollbackOffsetRows`.
  - When the matching snapshot state arrives, the view applies the camera correction and clears the pending reentry rows, so old snapshot + old camera and new snapshot + new camera remain paired.
- Regression coverage:
  - `EndToEndTest.zoomed_scrollback_live_reentry_waits_for_matching_snapshot`
- Verification:
  - `./gradlew :app:compileDebugAndroidTestKotlin`
  - `./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.terminal.TerminalViewportPolicyTest`
  - `./gradlew :app:connectedDebugAndroidTest --no-configuration-cache -Plingon.it.class=systems.pkt.lingon.terminal.TerminalGridViewTest#liveReentryWaitsForScrollbackSnapshotBeforeMovingCamera` before moving the regression into `EndToEndTest`
  - `./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin`
  - `LINGON_IT_ONLY=zoomed_scrollback_live_reentry_waits_for_matching_snapshot make test-android`
  - `LINGON_IT_ONLY=zoomed_viewport_does_not_reset_after_frame_and_resume make test-android`

### B-033 Empty replay reconnect and TLS config-root defaults

- Status: `resolved`
- Area: `relay`, `cli`, `config`
- Summary: Reconnects with no replay frames must still make clients ready, and `--config-dir` must rebase TLS command defaults.
- Report:
  Review flagged two regressions:
  1. A reconnecting attach client with `last_seq` equal to the relay's current sequence hit the replay fast path with zero frames, so the relay sent neither replay frames nor a normal hello to the host.
  2. `lingon --config-dir <dir> tls new/export` still used the original home-derived `--dir` default because TLS commands use a flag named `--dir`, not `--tls-dir`.
- Repro:
  1. Register a host, record frames through relay history, then send client hello with `last_seq` equal to the current relay sequence.
  2. Observe the relay does not send a replay frame and previously did not forward hello to the host.
  3. Build a root command, apply a config root, and inspect TLS `new`, `new ca`, `new server`, and `export` `--dir` defaults.
- Investigation notes:
  - Replay remains a fast path only when there is at least one frame to send.
  - Empty replay now sends a lightweight control response to the client and only forwards resize to the host when needed. It does not replay cached history and does not request a fresh host snapshot.
  - Attach clients with a cached snapshot can mark reconnect ready from that control response; clients without cached snapshot still require a real snapshot.
  - The command sweep found the TLS `--dir` flag as the missed config-root-derived path. Other config-root-derived CLI flags already used `auth-file`, `log-file`, `data-dir`, `users-file`, `tls-dir`, or `tls-cache-dir` and were already rebased.
- Regression coverage:
  - `TestHubFallsBackToHelloWhenReplayIsEmptyAtCurrentSequence`
  - `TestControlFrameCanMarkCachedReconnectReady`
  - `TestRootConfigDirFlagRebasesDerivedDefaults`
- Verification:
  - `go test ./internal/relay -run 'TestHubReplaysMissingFramesToReconnectingClient|TestHubFallsBackToHelloWhenReplayHistoryIsTooOld|TestHubFallsBackToHelloWhenReplayIsEmptyAtCurrentSequence|TestHubClearsReplayHistoryWhenHostIsReplaced'`
  - `go test ./internal/attach -run 'TestControlFrameCanMarkCachedReconnectReady|TestControlFrameDoesNotMarkReconnectReadyWithoutSnapshot'`
  - `env LINGON_CONFIG_DIR=/tmp/lingon-review-env go test ./cmd/lingon -run 'TestRootConfigDirFlagRebasesDerivedDefaults|TestRootDefaultAuthFileIgnoresXDGConfigHome'`
  - `env LINGON_CONFIG_DIR=/tmp/lingon-review-env go test ./internal/config`
  - `env LINGON_CONFIG_DIR=/tmp/lingon-review-env go test ./...`

### B-032 Android harness leaves temp directories in `/tmp`

- Status: `resolved`
- Area: `android`, `integration-tests`, `harness`
- Summary: Android harness runs must remove their `lingon-android-harness-*` temp root after each run.
- Report:
  Repeated Android integration runs left many `/tmp/lingon-android-harness-*` directories behind.
- Repro:
  1. Run Android integration tests that start and stop the harness repeatedly.
  2. List `/tmp/lingon-android-harness-*`.
  3. Observe stale harness config/state directories left after completed runs.
- Investigation notes:
  - `startHarness` created a temp root with `os.MkdirTemp("", "lingon-android-harness-")`.
  - `harness.stop()` stopped sessions and the HTTP server but did not remove `h.baseDir`.
  - The main process also called `h.stop()` only after the normal wait path, so config write failures could skip cleanup.
  - The default host echo log now lives inside the harness temp root so the whole harness-owned tree is removed together.
- Regression coverage:
  - `TestHarnessStopRemovesTempRoot`
- Verification:
  - `go test ./cmd/lingon-android-harness`
  - `cd android && go test ./cmd/lingon-android-tools`
  - Direct smoke: start `lingon-android-harness -sessions 0`, terminate it with `SIGTERM`, and confirm no new `/tmp/lingon-android-harness-*` directory remains.

### B-030 Android wall notifications duplicate sender in title and body

- Status: `resolved`
- Area: `android`, `notifications`
- Summary: Android wall notifications must not repeat the sender/header in both notification title and body.
- Report:
  The Android app displays wall notification source text redundantly: the title/header is the sender, and the notification body starts with the same sender prefix, producing output like `alice@127.0.0.1` in both places.
- Repro:
  1. Enable Android background wall notifications.
  2. Send a wall message while the app is backgrounded.
  3. Observe the Android notification title contains the sender/header and `EXTRA_TEXT` repeats the same sender/header before the message body.
- Investigation notes:
  - `AndroidWallNotifier` sets `EXTRA_TITLE` to the formatted wall source, then calls `formatWallContent(...)`, which prefixes the message body with the same formatted source.
  - Android surfaces title and text together, so the prefix belongs only in the title.
  - The relay carries `sender` metadata and `message` separately; it does not prefix the message with `sender:`.
  - The formatter now keeps the source in `EXTRA_TITLE` and no longer adds a source prefix to `EXTRA_TEXT`/big text.
  - The manual harness wall delivery path initially exposed stale wall-inactivity state and foreground/background test idling races. Those were fixed in the Android integration harness so the manual background notification assertion now reaches the body-only formatter check.
- Regression coverage:
  - `AndroidWallNotifierTest.formatWallContentDoesNotRepeatSourceWhenMessagePresent`
  - `EndToEndTest.background_wall_delivery_posts_system_notification`
  - `EndToEndTest.background_manual_wall_delivery_posts_system_notification`
  - `EndToEndTest.background_wall_delivery_stops_without_notification_permission_and_recovers_after_grant`
- Verification:
  - `./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.notifications.AndroidWallNotifierTest`
  - `./gradlew :app:compileDebugAndroidTestKotlin`
  - `LINGON_IT_ONLY=background_wall_delivery_posts_system_notification make test-android`
  - `LINGON_IT_ONLY=background_manual_wall_delivery_posts_system_notification make test-android`
  - `./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin`
  - `make test-android` passed with one expected emulator capability skip for notification-permission gating

### B-031 Android full integration batch Compose idling race

- Status: `resolved`
- Area: `android`, `integration-tests`
- Summary: The full Android instrumentation sweep must not abort on a transient Compose measure/layout idle race while polling app state.
- Report:
  The full Android integration suite failed in a later shared instrumentation batch while `readLoginError()` called `composeRule.runOnIdle()`. Espresso reported `performMeasureAndLayout called during measure layout`, aborting the batch before product assertions could run.
- Repro:
  1. Run `make test-android`.
  2. Let the suite progress into the final shared instrumentation batch.
  3. Observe a transient Compose/Espresso idle exception from app-state polling, previously seen in different test methods depending on batch timing.
- Investigation notes:
  - The failing stack originated in common e2e polling helpers, not in the tested product path.
  - The guard is intentionally narrow: only the exact Compose `performMeasureAndLayout called during measure layout` race is treated as transient. Login errors, status errors, assertion failures, and all other runtime exceptions still fail the test.
- Regression coverage:
  - The common `waitUntil`/`waitUntilNoError` polling helpers now use the narrow idle guard.
  - `EndToEndTest.headless_detach_removes_session_without_reconnect_placeholder` was rerun because it was the latest batch victim.
- Verification:
  - `./gradlew :app:compileDebugAndroidTestKotlin`
  - `LINGON_IT_ONLY=headless_detach_removes_session_without_reconnect_placeholder make test-android`
  - `make test-android` passed with one expected emulator capability skip for notification-permission gating

### B-029 Resize integration smoke failures

- Status: `resolved`
- Area: `session`, `resize`, `tests`
- Summary: Release smoke exposed viewport resize failures around immediate typing after shrink and large-viewport clear comparisons.
- Report:
  During the full `go test -count=1 -tags integration ./integration/...` release smoke, `integration/pty/session` failed in `TestHostResizeImmediateTypingAfterShrinkKeepsPromptOnBottomRow` and `TestHostResizeLargeViewportClearAfterExpandMatchesControl`.
- Repro:
  1. Run `go test -count=1 -tags integration ./integration/pty/session`.
  2. Observe immediate post-resize input missing from the bottom prompt row in `TestHostResizeImmediateTypingAfterShrinkKeepsPromptOnBottomRow`.
  3. Observe large-viewport clear tests depending on real `ps aux` output, which can scroll the command line out of view on a busy host.
- Investigation notes:
  - Input could be consumed before the resize goroutine applied the already-changed TTY size, so keystrokes immediately after a resize were written against stale active-session geometry.
  - The large-viewport clear tests were not deterministic because host process-list length affected whether `ps aux` remained visible.
- Regression coverage:
  - `TestHostResizeImmediateTypingAfterShrinkKeepsPromptOnBottomRow`
  - `TestHostResizeLargeViewportClearAfterExpandMatchesControl`
  - `TestHostResizeLargeViewportCtrlLLClearAfterExpandMatchesControl`
- Verification:
  - `go test -count=1 -tags integration ./integration/pty/session -run 'TestHostResizeImmediateTypingAfterShrinkKeepsPromptOnBottomRow|TestHostResizeLargeViewportClearAfterExpandMatchesControl|TestHostResizeLargeViewportCtrlLLClearAfterExpandMatchesControl' -v`
  - `go test -count=1 -tags integration ./integration/...`
  - `go test -count=1 ./internal/session`

### B-028 Config default and Android foreground service review fixes

- Status: `resolved`
- Area: `config`, `android`, `notifications`
- Summary: The default config directory must remain `$HOME/.lingon` with no XDG cutover fallback, and Android must not start the wall foreground service from the background.
- Report:
  Review flagged that the Android background wall foreground service was started only after the app transitioned to background, which Android 12+ can reject. The config review also surfaced that the current default path logic incorrectly allowed `XDG_CONFIG_HOME` to move Lingon's default config directory to `$XDG_CONFIG_HOME/.lingon`.
- Repro:
  1. Set `XDG_CONFIG_HOME` and call `DefaultConfigDir()`.
  2. Observe the broken default points under XDG instead of `$HOME/.lingon`.
  3. Enable Android background wall notifications while foregrounded and background the app.
  4. Observe the service start request is scheduled from the background transition path.
- Investigation notes:
  - The config fix is a hard cutover back to `$HOME/.lingon`; no fallback search paths for `$XDG_CONFIG_HOME/lingon` or `$XDG_CONFIG_HOME/.lingon` are kept.
  - Lingon now has one explicit config root override, `--config-dir`/`-C` and `LINGON_CONFIG_DIR`. All default config/data paths derive from that root: `config.yaml`, `auth.json`, server data, `users.json`, TLS dir/cache, and local headless state.
  - Android should start or keep the foreground service while the app is still foregrounded, but the service's poll loop must skip polling while the app is foregrounded so wall notifications are not consumed under foreground suppression.
  - The broader integration sweep exposed a remaining test-harness regression: local headless attach subprocesses were isolated with XDG-only env vars. Since Lingon no longer uses XDG for the default config dir, those subprocesses now use `LINGON_CONFIG_DIR` so they see the same headless state without mutating `HOME`.
  - A live local PTY report still observed `$XDG_CONFIG_HOME/.lingon/auth.json.lock`. Runtime inspection showed active Lingon processes were stale installed binaries with `XDG_CONFIG_HOME` set and no explicit `LINGON_*` override. Those stale processes can keep creating the old lock path until restarted.
- Regression coverage:
  - `TestDefaultPathsIgnoreXDGConfigHome`
  - `TestDefaultPathsUseLingonConfigDirEnv`
  - `TestLoaderUsesLingonConfigDirEnvForDefaultConfigFile`
  - `TestRootConfigDirFlagRebasesDerivedDefaults`
  - `AppViewModelTest.shouldEnableBackgroundWallServiceWhenLoggedInUnlockedAndEnabled`
  - `AppViewModelTest.setBackgroundWallEnabledStartsBackgroundWallServiceWhileForegrounded`
  - `AppViewModelTest.onAppForegroundKeepsBackgroundWallServiceRunningAfterBackgroundEnable`
  - `BackgroundWallForegroundServiceTest.shouldPollBackgroundWallOnlyWhenAppIsBackgrounded`
  - `TestConfigDirForLoaderUsesLingonConfigDirEnv`
  - `TestRootDefaultAuthFileIgnoresXDGConfigHome`
- Verification:
  - `go test ./internal/config ./cmd/lingon-android-harness`
  - `./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin`
  - `LINGON_IT_ONLY=foreground_resume_suppresses_background_wall_notifications make test-android`
  - `LINGON_IT_ONLY=background_manual_wall_delivery_posts_system_notification make test-android`
  - `go test -count=1 ./...`
  - `go test -count=1 -tags integration ./integration/...`
  - `make test-webui`
  - `make test-android`
  - `go vet ./...`
  - `golint ./...`
  - `golangci-lint run ./...`
  - `git diff --check`

### B-027 Android foreground manual wall notification smoke failure

- Status: `resolved`
- Area: `android`, `notifications`, `integration-tests`
- Summary: Release smoke failed because the foreground/manual wall notification instrumentation test did not observe a `lingon_wall` system notification.
- Report:
  `make test-all` passed Go and web UI tests, then failed during the Android integration sweep in `manual_wall_delivery_posts_system_notification`. The app state at timeout was connected with active sessions and `lastFrameType=diff`, but no notification with the sent wall message appeared within 10 seconds.
- Repro:
  1. Run the Android release-smoke integration sweep.
  2. In the final notification batch, run `EndToEndTest.manual_wall_delivery_posts_system_notification`.
  3. Observe the test time out waiting for a foreground/manual `lingon_wall` notification.
- Investigation notes:
  - The current wall delivery coordinator applies a single foreground gate to all delivery callers. Both websocket wall frames and background wall polling/service delivery call the same `deliver(...)` method, so the foreground suppression fix for B-025 can also suppress the older foreground/manual notification behavior.
  - This was an obsolete smoke-test expectation rather than a production delivery regression: the current product invariant is that foreground wall traffic must not post Android system notifications, while `background_manual_wall_delivery_posts_system_notification` remains the positive proof for app-backgrounded notification delivery.
- Regression coverage:
  - `EndToEndTest.foreground_manual_wall_delivery_does_not_post_system_notification` verifies a foreground harness wall is created and does not produce a `lingon_wall` system notification while background wall notifications are disabled.
- Verification:
  - `make test-all` failed in `manual_wall_delivery_posts_system_notification` after Go and web UI suites passed.
  - `./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin`
  - `LINGON_IT_ONLY=foreground_manual_wall_delivery_does_not_post_system_notification make test-android`
  - `LINGON_IT_ONLY=background_manual_wall_delivery_posts_system_notification make test-android`
  - `make test-all`

### B-026 Local host PTY gpg output leaves underline enabled

- Status: `resolved`
- Area: `host`, `terminal`, `pty`
- Summary: Local host PTY rendering must not treat private CSI terminal mode controls as SGR underline.
- Report:
  Running an interactive signing/amend workflow from a Lingon host local PTY session left subsequent command output and prompts visually underlined. The same workflow did not leave underline enabled in a normal terminal session.
- Repro:
  1. Start a Lingon host local PTY session.
  2. Run an interactive full-screen editor/signing workflow that exits through xterm private mode restore sequences.
  3. Observe subsequent shell output remains underlined.
- Investigation notes:
  - The terminal parser treated `CSI 4:0 m` as the numeric parameter `40`, so the SGR underline-style reset did not clear `ModeUnderline`.
  - 2026-04-28: User retested the interactive signing/amend flow in a Lingon host local PTY and the underline still reproduced, so the first fix covered a related parser bug but not the reported path.
  - The remaining repro is full-screen-program shaped: the non-interactive path did not reproduce because it avoids the alternate-screen program before printing the summary.
  - Root cause: Lingon's `?1049` alternate-screen save/restore path restored only cursor coordinates, not the saved graphic rendition attributes. If a less-like full-screen program left underline active when switching back to the main screen, following normal-screen output inherited underline.
  - Added a closer reproduction with a fake full-screen program that emits editor/full-screen underline and italic attributes before printing a summary.
  - User clarified the repro happens on every interactive amend flow, with or without signing, and suspected terminal query/control leakage. Hardened delta rendering so every changed span starts with an explicit SGR reset, preventing a corrupted outer-terminal rendition state from carrying into later cursor-addressed summary spans.
  - 2026-04-28: User retested again and clarified there is no real underline in the full-screen editor, pager, prompts, or summary output; the visible underline is garbled/corrupted output, likely from leaked or misrouted terminal control/query traffic. The reproduction must avoid real editors and use deterministic control-sequence fixtures.
  - 2026-04-28: Reopened because the existing harness-level fixture did not reproduce the actual host/local PTY path from the screenshots. Replaced external-program-dependent coverage with deterministic control-sequence fixtures.
  - Additional leak reproduced: late OSC 10/11/12 outer-terminal color responses were forwarded into the active local PTY once the startup pending/grace window expired. The failing integration repro sent a late OSC response before `echo AFTER_LATE_OSC`; the shell received corrupted input and reported `/bin/sh: 1: echo: not found`. `filterOuterOSC` now consumes complete OSC 10/11/12 responses even outside the pending/grace window, while still passing incomplete/split fragments through immediately so ordinary keyboard input is not buffered.
  - Root cause reproduced for the screenshot underline: a full-screen terminal program can emit `CSI > 4 ; m` (`ESC[>4;m`) while restoring xterm modifyOtherKeys state. Lingon's emulator dispatched every final `m` as SGR even when the CSI private marker was `>`, so it interpreted the parameter `4` as SGR underline and marked subsequent summary cells underlined. Real terminals treat `CSI > ... m` as a private control sequence, not SGR.
  - Fix: SGR handling now only runs for non-private CSI `m`; private CSI `m` sequences are ignored as private/unhandled instead of mutating rendition attributes.
- Regression coverage:
  - `TestSGRColonUnderlineStyleResetClearsUnderline`
  - `TestSGRColonUnderlineStyleEnablesUnderline`
  - `TestPrivateCSIGreaterMDoesNotEnableUnderline`
  - `TestAltScreen1049RestoresSavedAttributes`
  - `TestHostLocalPTYColonUnderlineResetClearsFollowingOutput`
  - `TestHostLocalPTYAltScreenExitRestoresAttributesForLessLikePrograms`
  - `TestHostLocalPTYPrivateCSIGreaterMDoesNotRenderUnderlined`
  - `TestHostLocalPTYLateOuterOSCResponseDoesNotCorruptNextCommand`
  - `TestSnapshotViewportDeltaResetsAttributesBeforeEveryChangedSpan`
- Verification:
  - `go test -count=1 ./internal/terminal/emu ./internal/session -run 'TestPrivateCSIGreaterMDoesNotEnableUnderline|TestSGR|TestFilterOuterOSC|TestLocalSessionRespondsToDSR|TestLocalSessionOSCQueryDoesNotSelfSustainPublish|TestLocalSessionRepeatedOSCQueriesBoundedAfterProcessIdle'`
  - `go test -count=1 ./internal/session ./internal/terminal/emu ./internal/render`
  - `go test ./internal/terminal/emu`
  - `go test ./internal/terminal/... ./internal/session`
  - `go test -count=1 -tags integration ./integration/pty/session -run 'TestHostLocalPTY(AltScreenExitRestoresAttributesForLessLikePrograms|ColonUnderlineResetClearsFollowingOutput)'`
  - `go test -count=1 -tags integration ./integration/pty/session -run 'TestHostLocalPTY(PrivateCSIGreaterMDoesNotRenderUnderlined|LateOuterOSCResponseDoesNotCorruptNextCommand|AltScreenExitRestoresAttributesForLessLikePrograms|ColonUnderlineResetClearsFollowingOutput)'`
  - `go test ./internal/render`
  - `go test ./...`
  - `go vet ./...`
  - `golint ./...`
  - `golangci-lint run ./...`

### B-025 Android foreground app still posts wall notifications

- Status: `resolved`
- Area: `android`, `notifications`, `lifecycle`
- Summary: Wall events must not post Android system notifications while the app is foregrounded, even when background wall notifications are enabled.
- Report:
  When the Android app is brought back into focus, wall notifications still appear despite background notifications being enabled. Expected behavior is that system wall notifications are only for the app-not-in-focus case.
- Repro:
  1. Enable background wall notifications in the Android app.
  2. Background the app so the foreground service starts.
  3. Bring the app back to the foreground.
  4. Send or receive a wall message and observe a `lingon_wall` system notification while the app is in focus.
- Regression coverage:
  - `WallDeliveryCoordinatorTest.notificationSuppressionDoesNotConsumeEvent`
  - `WallDeliveryCoordinatorTest.inAppConsumptionAdvancesCursorAndSuppressesReplayWithoutPostingNotification`
  - `AppViewModelTest.foregroundLiveWallFrameShowsInAppBannerAdvancesCursorAndSuppressesReplay`
  - `EndToEndTest.foreground_manual_wall_delivery_does_not_post_system_notification`
  - `EndToEndTest.foreground_resume_suppresses_background_wall_notifications`
- Investigation notes:
  - The foreground suppression fix was too broad: the shared coordinator treated `shouldPostNotification == false` as delivered and advanced the wall cursor without any in-app surface.
  - Foreground WebSocket wall frames now use an in-app consumption path that records the cursor only after accepting the message for a visible status banner.
  - Background polling still uses the Android notification path; if notification posting is suppressed there, the event is not consumed.
- Verification:
  - `./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin`
  - `./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.notifications.WallDeliveryCoordinatorTest --tests systems.pkt.lingon.viewmodel.AppViewModelTest.foregroundLiveWallFrameShowsInAppBannerAdvancesCursorAndSuppressesReplay`
  - `LINGON_IT_ONLY=foreground_manual_wall_delivery_does_not_post_system_notification make integration-test`
  - `LINGON_IT_ONLY=foreground_resume_suppresses_background_wall_notifications make integration-test`
  - `LINGON_IT_ONLY=background_manual_wall_delivery_posts_system_notification make test-android`

### B-024 Android cursor-ahead recovery instrumentation race

- Status: `resolved`
- Area: `android`, `integration-tests`, `notifications`
- Summary: The cursor-ahead recovery instrumentation test must not send the post-reset wall before the background service has actually observed and repaired the ahead cursor.
- Report:
  Release smoke `make test-android` failed in `background_manual_wall_delivery_recovers_when_cursor_is_ahead_of_relay`. Logcat showed `lingon-wall-bg: poll cursor reset detected endpoint=... since=193 next=94`, but the test had already sent or was racing the post-reset wall against the service's 15s poll cadence, and the app timed out waiting for the target wall notification.
- Repro:
  1. Run the full Android integration sweep.
  2. In the final notification batch, advance the app wall cursor ahead of the relay and immediately send a wall.
  3. If the send races the service's cursor repair poll, the test can time out with `lastFrameType=diff` and no target `lingon_wall` notification.
- Regression coverage:
  - `EndToEndTest.background_manual_wall_delivery_recovers_when_cursor_is_ahead_of_relay` now waits for the foreground service to repair the ahead cursor before sending the post-reset wall message.
- Verification:
  - `./gradlew :app:compileDebugAndroidTestKotlin`
  - `LINGON_IT_ONLY=background_manual_wall_delivery_recovers_when_cursor_is_ahead_of_relay make test-android`
  - `make test-android`

### B-023 Android wall notifications replay previous message with next message

- Status: `resolved`
- Area: `android`, `notifications`
- Summary: Android wall notification dedupe must be monotonic and must serialize foreground/background delivery so an already-delivered or older wall event is not posted again with the next event.
- Report:
  The Android app repeats each wall message as a double notification; after some time, the previous notification is sent again with the next notification, so wall delivery is not deduped correctly.
- Repro:
  1. Deliver wall event `N`.
  2. Deliver wall event `N+1` or allow foreground websocket and background polling to process overlapping wall pages.
  3. Observe event `N` can be posted again because the state check treats any event id different from the current cursor as deliverable, and concurrent deliveries can both pass the read-side check before either records the cursor.
- Regression coverage:
  - `WallWorkStateStoreTest.deliveryChecksAndRecordsAreMonotonic`
  - `WallWorkStateStoreTest.shouldDeliverAndAdvanceSuppressesReplayForSameEndpoint`
  - `WallDeliveryCoordinatorTest.olderEventAfterNewerEventDoesNotReplayOrMoveCursorBackward`
  - `WallDeliveryCoordinatorTest.concurrentDeliveryOfSameEventPostsOnlyOnce`
  - `EndToEndTest.background_manual_wall_delivery_does_not_repost_previous_message`
- Verification:
  - `./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.notifications.WallDeliveryCoordinatorTest --tests systems.pkt.lingon.data.WallWorkStateStoreTest`
  - `./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin`
  - `LINGON_IT_ONLY=background_manual_wall_delivery_posts_system_notification make test-android`
  - `LINGON_IT_ONLY=background_manual_wall_delivery_does_not_repost_previous_message make test-android`
  - `git diff --check`

### B-021 Attach async rendering races with read-side render state

- Status: `resolved`
- Area: `attach`, `desktopnotify`, `windows`
- Summary: Async attach rendering must not race with websocket/read-side bookkeeping over shared render state, and the desktop notification package must remain buildable for Windows.
- Report:
  Reviewer flagged two issues: attach render requests now run on a render goroutine while websocket reads can still access `renderCache`/compositor state, and the Windows desktop notifier stub redeclares the common noop notifier.
- Repro:
  1. Inspect attach paths where `startRenderLoop` enables rendering outside the websocket read goroutine.
  2. Receive scrollback or other read-side frames while a render is pending; read-side helpers can read `renderCache` without `renderMu`.
  3. Run `GOOS=windows GOARCH=amd64 go test -c ./internal/desktopnotify`.
- Verification:
  - Added `internal/attach.TestAttachRenderCacheReadsUseSerializedHelpers` to prevent direct read-side `renderCache` reads from bypassing serialized helpers.
  - `GOOS=windows GOARCH=amd64 go test -c ./internal/desktopnotify`
  - `go test ./internal/desktopnotify`
  - `go test ./internal/attach -run 'TestAttachRenderCacheReadsUseSerializedHelpers|TestAttachRenderingDoesNotUseOverlayOnlyComposePaths'`
  - `go test ./internal/attach`
  - `go test -race ./internal/session -run 'TestAttachSendInputSharedConfig|TestAttachSendInputSeparateConfig'`
  - `go test ./...`
  - `go vet ./...`
  - `golint ./...`
  - `golangci-lint run ./...`
  - Remaining note: broader race runs still expose separate pre-existing race-detector failures in test buffers/session PTY lifecycle paths, outside the attach `renderCache`/Windows notifier fixes tracked here.

### B-022 Android wall notification delivery and integration isolation regressions

- Status: `resolved`
- Area: `android`, `notifications`, `integration-tests`
- Summary: Android wall notification delivery must not consume events that Android did not actually post, and Android integration tests must run in larger instrumentation batches without leaking app-op, activity, notification, or harness headless-session state between tests.
- Report:
  During the full Android integration sweep, `background_manual_wall_delivery_posts_system_notification` timed out after the app received a wall frame (`lastFrameType=wall`) but no `lingon_wall` system notification became visible. The old test runner also ran one Gradle instrumentation invocation per test, which made the suite slow and hid real state leaks behind per-test process/app resets.
- Repro:
  1. Run the full Android integration target after prior wall notification tests have toggled notification delivery.
  2. Observe the background manual wall notification test receive the relay wall frame but fail to observe the expected Android system notification.
  3. Batch Android tests into shared instrumentation invocations; stale headless sessions and notification/app-op state then leak into later visual/tab tests unless teardown is explicit.
- Investigation notes:
  - `NotificationManagerCompat.notify(...)` can return without throwing even when the app cannot later observe the posted notification; treating that call as delivery success advanced the wall cursor too early.
  - The permission-toggle test could leave notification app-op state behind for later tests because the old reset path depended on reinstall/clear behavior rather than an explicit test invariant.
  - Headless sessions created through the harness persisted at the relay/harness layer and polluted later tests once they shared an instrumentation process.
  - The integration runner now batches tests by harness mode: normal tests before the zero-session case, that special case, the next normal batch, the quiet-host case, and the final normal batch.
- Regression coverage:
  - `WallDeliveryCoordinatorTest.failedNotificationDoesNotAdvanceCursor`
  - `WallDeliveryCoordinatorTest.successfulNotificationAdvancesCursorAndSuppressesReplay`
  - Android instrumentation teardown now restores notification delivery, foregrounds/logs out the app, clears notifications, and detaches harness-created headless sessions.
  - Android integration runner now proves batched execution instead of one Gradle/instrumentation invocation per test.
- Verification:
  - `./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin`
  - `LINGON_IT_ONLY=background_manual_wall_delivery_posts_system_notification make test-android`
  - `make test-android` passed with 3-test, 17-test, and 19-test instrumentation batches plus two required single-test harness-mode cases. One pre-existing permission-toggle case skipped via its existing `assumeTrue` because the emulator did not support that runtime app-op toggle.

### B-020 Headless resize policy is inconsistent between attach and host remote multi-client

- Status: `resolved`
- Area: `attach`, `session`, `headless`
- Summary: Headless remote sessions must resize consistently in both `lingon attach` and host remote multi-client: initial connect, local WINCH, and control acquisition resize; non-headless remains camera-only. A manual headless-only forced resize shortcut should also exist in the TUI.
- Report:
  `lingon attach` against headless effectively resizes on connect/WINCH/control acquisition, but host remote multi-client was only getting similar behavior indirectly through reconnect/enable side effects. The result was inconsistent headless sizing after controller handoff. The user also requested a manual headless-only forced resize shortcut, but `Ctrl+L r` was already taken by respawn, so the agreed explicit action was `Ctrl+L 0` / `Ctrl+L Ctrl+0`.
- Repro:
  1. Start a relay-backed headless session.
  2. Connect to it from a host remote multi-client in a `40x10` viewport.
  3. Connect a second controller attach in a `52x14` viewport so the headless PTY is resized away from the host remote viewport.
  4. Disconnect the attach controller.
  5. In the host remote tab, observe the next interaction should restore the headless PTY to the host remote viewport consistently with attach semantics.
- Investigation notes:
  - `lingon attach` already had three explicit headless resize triggers:
    - `OnReady` initial connect resize in `internal/attach/multi.go`
    - local resize/WINCH path in `internal/attach/multi.go`
    - control acquisition resize in `internal/attach/client.go` via `controlCh`
  - Host remote multi-client only resized headless on `Show(...).OnReady` and on explicit `Runner.ResizeActive`, with controller-handoff behavior occurring only accidentally when input caused `Enable(...) -> OnReady`.
  - Fixed by:
    - adding an explicit host-remote headless resize callback on controller acquisition
    - enforcing headless-only gating inside `remoteManager.SendResize`
    - adding `Ctrl+L 0` / `Ctrl+L NUL` as a manual headless-only resize action in both attach and host TUI paths
- Regression coverage:
  - `integration/pty/session.TestHostRemoteHeadlessReacquiresControlAndResizesAfterAttachControllerDisconnects`
  - `internal/control.TestPrefixSessionActions`
- Verification:
  - `go test -count=1 ./internal/control ./internal/attach ./internal/session`
  - `go test -count=1 -tags integration ./integration/pty/session -run 'TestHostRemoteHeadless(InitialConnectAndWinchResizePTY|ExitRemovesSessionWithoutReconnectOverlay|ReacquiresControlAndResizesAfterAttachControllerDisconnects)'`
  - `go test -count=1 -tags integration ./integration/pty/attach -run 'TestRealCLIRelayHeadless(InitialConnectAndWinchResizePTY|ExitRemovesTerminatedSessionWithoutReconnectOverlay)'`
  - `go test -count=1 ./...`
  - `go vet ./...`
  - `golint ./...`
  - `golangci-lint run ./...`

### B-019 Test/config path regression breaks real user auth lookup

- Status: `resolved`
- Area: `config`, `tests`, `auth`
- Summary: Test-isolation work must not change Lingon's real config/auth lookup path, and tests must never fall back to the developer's real `HOME` or real `~/.lingon` / XDG config tree.
- Report:
  After recent test/config changes, Lingon started failing with:
  `auth file not found at $XDG_CONFIG_HOME/lingon/auth.json; run lingon login -e <endpoint>`
  The developer reported this as tests having broken the personal `.lingon` config/auth state.
- Repro:
  1. Run the code after the `test(config): isolate XDG-backed test state` tranche.
  2. In an environment with `XDG_CONFIG_HOME` set, run a normal Lingon CLI path that resolves default auth.
  3. Observe Lingon now looks for `$XDG_CONFIG_HOME/lingon/auth.json` instead of the existing hidden-dir path.
- Investigation notes:
  - The direct destructive test-delete smoking gun was not present in the CLI tests that were first suspected.
  - The actual root cause was a runtime path regression introduced in `internal/config/paths.go`:
    - under `XDG_CONFIG_HOME`, Lingon had been changed to use `XDG_CONFIG_HOME/lingon`
    - previous behavior, and the user's real stored state, used `XDG_CONFIG_HOME/.lingon`
  - That path change exactly matched the observed broken lookup.
  - The same tranche also rewrote many tests and shared test harnesses around the wrong `root/lingon` assumption, which masked the runtime regression and caused follow-on TLS/auth failures in unrelated tests.
  - Test isolation is now hardened further by using `testutil.SetLingonConfigEnv`/`LINGON_CONFIG_DIR`, so tests can route Lingon config without mutating the process `HOME`.
- Regression coverage:
  - `internal/config.TestDefaultPaths`
  - `internal/config.TestDefaultPathsIgnoreXDGConfigHome`
  - `internal/config.TestDefaultPathsUseLingonConfigDirEnv`
  - `internal/config.TestDefaultConfigUsesConstants`
  - `cmd/lingon` command tests revalidated against the restored hidden-dir path
  - `internal/ptytest.Harness` and affected webui/session tests updated to the restored hidden-dir config location
- Verification:
  - `go test -count=1 ./internal/config ./cmd/lingon ./internal/cliwall`
  - `go test -count=5 ./internal/relayhost -run TestHostHonorsRetryAfter -v`
  - `go test -count=1 ./...`
  - `go vet ./...`
  - `golint ./...`
  - `golangci-lint run ./...`

### B-017 Attach startup connected banner wipes row-1 prompt/body

- Status: `resolved`
- Area: `attach`, `mvu`, `render`
- Summary: On multi-attach startup, the green `connected to ...` banner must overlay only its own cells on row 1 and must not blank the prompt/body underneath the rest of the row.
- Report:
  `lingon attach` starts with only the cursor and the green connected banner visible; the prompt/body on row 1 is erased underneath the banner overlay.
- Repro:
  1. Start a normal non-headless host whose prompt is visible on row 1.
  2. Start `lingon attach` / multi-attach into that session.
  3. Observe the transient green `connected to ...` banner on row 1.
  4. The left side of row 1 should still show the prompt/body; instead it is blanked.
- Investigation notes:
  - Existing attach startup tests only checked that the tab bar hides when the cursor owns row 1.
  - Existing MVU/runtime tests cover banner composition in isolation, but not the real multi-attach startup path with a visible prompt underneath.
  - The new real PTY regression failed exactly as reported: row 1 contained only the right-aligned `connected to ...` banner and spaces elsewhere, with the underlying prompt blanked.
  - Root cause was in top-row overlay rendering: on attach, the renderer masked row 1 entirely and then cleared/drew the banner, so the startup snapshot never painted the underlying row-1 content first.
  - Fixed by rendering row 1 base content first, rendering rows 2..N through the existing mask-top-row path, and then drawing the top overlay without clearing the whole row.
- Regression coverage:
  - `internal/attach.TestMultiAttachStartupConnectedBannerOverlaysPromptInsteadOfBlankingRow`
  - `internal/attach.TestAttachFastReadyDoesNotLeaveLoadingBanner`
  - `internal/mvu.TestRenderAttachConnectionBannerOwnsTopRow`
  - `internal/mvu.TestRenderHostConnectionBannerOverwritesTopRowWithoutShiftingContent`
- Verification:
  - `go test -count=1 ./internal/attach -run 'TestMultiAttachStartupConnectedBannerOverlaysPromptInsteadOfBlankingRow|TestAttachFastReadyDoesNotLeaveLoadingBanner' -v`
  - `go test -count=1 ./internal/mvu -run 'TestRenderAttachConnectionBannerOwnsTopRow|TestRenderHostConnectionBannerOverwritesTopRowWithoutShiftingContent' -v`

### B-013 Android wall delivery missing

- Status: `resolved`
- Area: `android`, `notifications`, `relay`
- Summary: Relay wall events must surface in the Android app through the intended delivery path, including background notification delivery when enabled.
- Report:
  Lingon wall no longer works to the Android app.
- Repro:
  1. Trigger a relay wall event for the logged-in Android user.
  2. Observe that the Android app does not surface the wall as expected.
- Investigation notes:
  - Existing Android instrumentation only moved the activity to `CREATED`; it did not send the app to the real launcher/home background state, so it could pass while true background delivery was broken.
  - Replaced the fake background helper with a real HOME/background transition using shell `input keyevent KEYCODE_HOME`.
  - On the real background path, the app exposed two Android-side issues:
    - resuming after the background notification tests could trip foreground-service startup churn
    - wall notifications accumulated and Android auto-grouped them behind an empty `ranker_group` summary, making them effectively invisible on-device
  - Fixed by:
    - making the background wall service controller edge-triggered so it only issues start/stop on real state transitions
    - removing wall-notification grouping entirely
    - switching wall delivery to a single stable notification slot so the latest wall remains visible instead of accumulating into an auto-group summary
  - Confirmed the background path is not using the WebSocket; it uses `BackgroundWallForegroundService` polling `/wall/events`
  - Real physical-phone repro on 2026-04-24 exposed the remaining live bug:
    - the phone was polling `/wall/events`, but persisted `since=36`
    - the relay had restarted/reset wall IDs and was serving new real walls as IDs `6`, `7`, `8`
    - Android permanently suppressed those walls as duplicates
  - Root cause was a cursor contract mismatch:
    - relay `wall/events` echoed the caller cursor when the client cursor was ahead, so the app could not detect relay wall-ID reset
    - Android wall delivery treated any lower event ID as replay forever
- Regression coverage:
  - `android/app/src/androidTest/java/systems/pkt/lingon/EndToEndTest.kt::manual_wall_delivery_posts_system_notification`
  - `android/app/src/androidTest/java/systems/pkt/lingon/EndToEndTest.kt::background_manual_wall_delivery_posts_system_notification`
  - `android/app/src/androidTest/java/systems/pkt/lingon/EndToEndTest.kt::background_manual_wall_delivery_recovers_when_cursor_is_ahead_of_relay`
  - `android/app/src/androidTest/java/systems/pkt/lingon/EndToEndTest.kt::background_wall_delivery_posts_system_notification`
  - `android/app/src/test/java/systems/pkt/lingon/data/WallWorkStateStoreTest.kt`
  - `android/app/src/test/java/systems/pkt/lingon/work/BackgroundWallForegroundServiceTest.kt`
  - `internal/relay/wall_test.go::TestWallServiceListEventsReturnsCurrentHighWatermarkWhenCursorIsAhead`
- Verification:
  - `go test ./...`
  - `go vet ./...`
  - `golint ./...`
  - `golangci-lint run ./...`
  - `./gradlew :app:testDebugUnitTest`
  - `./gradlew :app:compileDebugAndroidTestKotlin`
  - Physical phone end-to-end over `adb` with the signed release installed:
    - background real `lingon wall ...` produced `lingon_wall` notification `id=1002`
    - foreground real `lingon wall ...` produced `lingon_wall` notification `id=1002`
    - verified via `dumpsys notification` and phone-side app logs
  - Note: targeted emulator instrumentation is currently awkward with a signed release installed on the connected physical phone because `connectedDebugAndroidTest` tries to install the debug app to every connected device. The exact phone-truth bug was reproduced and verified directly on the physical device instead.

### B-014 Wall inactivity banner leaks onto disconnected remote tab switch

- Status: `in_progress`
- Area: `attach`, `session`, `mvu`
- Summary: Switching to a disconnected remote session in multi-client must not show the green `wall inactivity off` top-bar banner unless the user explicitly toggled wall inactivity.
- Report:
  When switching to a disconnected remote session in multi-client (`lingon` host or `lingon attach`), the `wall inactivity off` green banner appears in the top bar.
- Repro:
  1. Open a multi-client host or attach with at least one disconnected remote session.
  2. Switch to that disconnected remote tab.
  3. Observe `wall inactivity off` shown in the top bar without a user toggle action.
- Investigation notes:
  - This banner should be event-driven from an explicit wall inactivity toggle/status update, not a generic tab-switch side effect.
  - The user reports two concrete failure modes:
    - switching to a never-opened/disconnected tab in multi-client shows `wall inactivity off`
    - reconnecting back into a tab can also surface the same stale green banner
  - One direct PTY regression now exists for the disconnected-tab switch path and passes repeatedly.
  - The broader live symptom is still not reproduced in harness coverage, so this bug stays open until the reconnect/never-opened path is trapped red as well.
- Regression coverage:
  - `internal/attach.TestMultiAttachSwitchToDisconnectedRelayTabDoesNotShowWallInactivityOff`
- Verification:
  - `go test -count=50 ./internal/attach -run TestMultiAttachSwitchToDisconnectedRelayTabDoesNotShowWallInactivityOff -v`
  - Remaining gap: the user-reported reconnect/never-opened banner leak still needs a red real PTY regression.

### B-015 Headless headless-exit semantics are inconsistent across attach and host-remote clients

- Status: `resolved`
- Area: `attach`, `headless`, `relay`
- Summary: When a headless session exits normally, relay-backed clients must treat it as a terminated session, not a lost connection with reconnect grace.
- Report:
  When `exit` is run in a headless terminal, the relay/client path treats it as connection-lost and shows reconnect grace instead of a clean terminated-session removal. This should match normal Lingon host session termination semantics.
- Repro:
  1. Start a headless session and connect through relay from `lingon attach` or a host remote multi-client.
  2. Run `exit` in the headless session.
  3. Observe a red `connection lost` banner and reconnect grace instead of clean session termination/removal.
- Investigation notes:
  - Previous work incorrectly treated headless as a special case and marked this resolved too early.
  - Headless sessions are just Lingon-owned local PTYs; they should not have bespoke grace semantics.
  - The remaining explicit-exit bug was lower in the stack than the attach/session clients:
    - after a host sent `session_closed`, the relay still tore the host down through the generic unregister path
    - that generic path broadcast `host disconnected` to clients
    - this raced the queued `session_closed` frame and intermittently left clients seeing reconnect semantics instead of clean termination
  - Fixes in this tranche:
    - relay now sends `session_closed` to clients immediately
    - relay marks explicit host close in session state and suppresses the generic `host disconnected` error broadcast on unregister
    - host remote manager now forces an immediate session refresh on unexpected remote-view close while the context is still live, so a missed explicit-close frame cannot leave a stale remote view around until the normal 60s poll
    - headless attach grace behavior remains normal for unexpected disappearance; only explicit `session_closed` removes immediately
- Regression coverage:
  - `internal/relay.TestHubSessionClosedDoesNotBroadcastHostDisconnected`
  - `internal/relay.TestHostSessionClosedFrameMarksSessionInactiveImmediately`
  - `internal/attach.TestRealCLIRelayHeadlessExitRemovesTerminatedSessionWithoutReconnectOverlay`
  - `internal/session.TestHostRemoteHeadlessExitRemovesSessionWithoutReconnectOverlay`
  - Existing grace regressions rechecked:
    - `internal/attach.TestRealCLIRelayHeadlessDeadActiveSessionTabIsRemovedAndRemainingSessionStaysUsable`
    - `internal/attach.TestRealCLILocalHeadlessDeadActiveSessionTabIsRemovedAndRemainingSessionStaysUsable`
- Verification:
  - `go test -count=1 ./internal/relay -run 'TestHubSessionClosedDoesNotBroadcastHostDisconnected|TestHostSessionClosedFrameMarksSessionInactiveImmediately'`
  - `go test -count=5 ./internal/attach -run 'TestRealCLI(RelayHeadless(InitialConnectAndWinchResizePTY|ExitRemovesTerminatedSessionWithoutReconnectOverlay|DeadActiveSessionTabIsRemovedAndRemainingSessionStaysUsable)|LocalHeadlessDeadActiveSessionTabIsRemovedAndRemainingSessionStaysUsable)' -v`
  - `go test -count=10 ./internal/session -run 'TestHostRemoteHeadless(InitialConnectAndWinchResizePTY|ExitRemovesSessionWithoutReconnectOverlay)' -v`
  - `go test -count=1 ./...`
  - `make test-webui`

### B-016 Headless initial-connect resize is missing in relay clients

- Status: `resolved`
- Area: `attach`, `headless`, `session`
- Summary: Relay-backed headless sessions must resize to the controlling client viewport on initial connect as well as on later WINCH changes.
- Report:
  `lingon attach` does resize a headless session on later WINCH, but not on initial connect, and host remote multi-client still does not resize relay headless sessions correctly.
- Repro:
  1. Start a headless session.
  2. Connect via `lingon attach` or host remote multi-client with a viewport different from the current headless PTY size.
  3. Observe the headless session does not immediately adopt the controller viewport.
  4. In `lingon attach`, later WINCH may resize correctly, proving the connect-time path is missing.
- Investigation notes:
  - This is a distinct bug from non-headless camera-only behavior; headless is supposed to allow resize propagation.
  - Existing coverage only proved later resize propagation and initial snapshot delivery on narrower paths.
  - Fixes in this tranche:
    - `lingon host` remote multi-client now uses the active remote-session resize path on WINCH
    - both attach and host-remote clients now send the controller viewport to relay headless sessions on initial connect as well as later WINCH
    - the host-side initial-connect regression helper was strengthened so it retries the actual tab-switch sequence long enough to prove the resize behavior rather than flaking on the mode switch itself
- Regression coverage:
  - `internal/attach.TestRealCLIRelayHeadlessInitialConnectAndWinchResizePTY`
  - `internal/session.TestHostRemoteHeadlessInitialConnectAndWinchResizePTY`
- Verification:
  - `go test -count=5 ./internal/attach -run 'TestRealCLI(RelayHeadless(InitialConnectAndWinchResizePTY|ExitRemovesTerminatedSessionWithoutReconnectOverlay|DeadActiveSessionTabIsRemovedAndRemainingSessionStaysUsable)|LocalHeadlessDeadActiveSessionTabIsRemovedAndRemainingSessionStaysUsable)' -v`
  - `go test -count=10 ./internal/session -run 'TestHostRemoteHeadless(InitialConnectAndWinchResizePTY|ExitRemovesSessionWithoutReconnectOverlay)' -v`
  - `go test -count=1 ./...`
  - `make test-webui`

### B-012 Multi-attach input lag/stall on real relay attach

- Status: `resolved`
- Area: `attach`, `session`, `render`
- Summary: Normal `lingon attach` against a real non-headless host must remain camera-only and must echo typed input promptly even when the host PTY is larger than the attach viewport.
- Report:
  Multi-attach currently becomes useless in a real terminal against a real Lingon host local PTY: input is not sent or arrives ultra-lagged, rendering is wrong, and proper testing would have caught it.
- Repro:
  1. Start a normal non-headless host with a local PTY larger than the attach window.
  2. Run authenticated `lingon attach` in a smaller local terminal and request control.
  3. Type commands into attach.
  4. Observe laggy or missing echo/input and broken rendering/wrapping.
- Investigation notes:
  - Existing multi-attach coverage was too weak for this report. It did not prove real CLI responsiveness through the relay path under smaller attach viewports, large host output, or resize churn.
  - A valid repro was added only on harness-owned PTYs and harness relay endpoints. No live endpoint or inherited tty state is used.
  - Root causes fixed across this tranche:
    - relay write ordering was not FIFO: control-priority writes could send higher-sequence frames ahead of earlier sequenced frames, causing attach gap/resync churn
    - `Sessions` frames were bypassing attach sequence accounting, which could also trigger resync churn
    - `attach.MultiClient` treated benign PTY-close stdin errors as fatal on teardown, making package-parallel runs flaky
    - real attach latency tests were measuring startup paint races instead of steady-state responsiveness
    - attach render coalescing could drop the first snapshot repaint if a status-banner render was already queued, leaving the view stuck on `wall inactivity off` while input was already flowing
  - The key user-visible lag symptom was reproduced in real external CLI PTY regressions:
    - host echoed typed bytes promptly
    - attach stayed stale for seconds after large host output, resize churn, or even at the first prompt under package load
    - once rendering was unstuck, measured input/echo latency stayed within the intended bounds
- Regression coverage:
  - `internal/attach.TestMultiAttachRealCLIControlDoesNotSendResizeAndEchoesPromptly`
  - `internal/attach.TestMultiAttachRealCLIControlWithMultipleSessionsKeepsViewportStable`
  - `internal/attach.TestMultiAttachRealCLIControlPsAuxAfterResizeMatchesHostCrop`
  - `internal/attach.TestMultiAttachRealCLIControlBurstEnterKeepsConsecutiveBashPromptNumbers`
  - `internal/attach.TestMultiAttachSignalResizeWithMultipleSessionsMatchesExplicitViewport`
  - `internal/attach.TestMultiAttachExternalCLIRepeatedInputStaysResponsiveRealClock`
  - `internal/attach.TestMultiAttachExternalCLIRepeatedSingleByteCommandsDoNotAccumulateLatencyRealClock`
  - `internal/attach.TestMultiAttachExternalCLIRepeatedSingleByteCommandsStayResponsiveWithBackgroundSessionOutput`
  - `internal/attach.TestMultiAttachExternalCLIRepeatedSingleByteCommandsStayResponsiveAfterLargeHostOutput`
  - `internal/attach.TestMultiAttachExternalCLIRepeatedSingleByteCommandsStayResponsiveAfterLargeHostOutputAndResizeChurn`
  - `internal/attach.TestMultiAttachExternalCLICommandExecutionStaysResponsiveAfterLargeHostOutput`
  - `internal/attach.TestMultiAttachRealCLIControlRepeatedSingleByteInputStaysResponsiveRealClock`
- Verification:
  - Focused contention slice passed:
    - `go test -p 5 -json -count=1 ./internal/attach ./internal/session ./internal/relayhost ./internal/relay ./internal/headlessd`
  - Full suite passed:
    - `go test -json -count=1 ./...`
  - Quality gates passed:
    - `go vet ./...`
    - `golint ./...`
    - `golangci-lint run ./...`
    - `make test-webui`

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
  - `integration/pty/attach.TestSingleAttachRelayViewportMatrixMatchesHostAcrossResizesAndLongOutput`
  - `integration/pty/attach.TestMultiAttachRelayViewportMatrixMatchesHostAcrossResizesAndLongOutput`
  - `integration/pty/attach.TestSingleAttachHeadlessResizeMatrixRendersExpectedViewport`
  - `integration/pty/attach.TestMultiAttachHeadlessResizeMatrixRendersExpectedViewport`
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
  - Focused matrix:
    - `go test -count=1 -tags integration ./integration/pty/attach -run 'Test(SingleAttachRelayViewportMatrixMatchesHostAcrossResizesAndLongOutput|MultiAttachRelayViewportMatrixMatchesHostAcrossResizesAndLongOutput|SingleAttachHeadlessResizeMatrixRendersExpectedViewport|MultiAttachHeadlessResizeMatrixRendersExpectedViewport)'`
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

- Status: `in_progress`
- Area: `tests`, `android`, `pty`
- Summary: Tests and harnesses must operate only on their own PTYs and must never mutate the inherited terminal running Lingon/tmux.
- Report:
  Previous test and harness runs resized or reconfigured the terminal session running the tests.
- Repro:
  1. Run resize-driving test coverage from the normal developer shell/tmux/Lingon session.
  2. Observe the outer tmux/Lingon terminal get resized, detached, or otherwise corrupted.
  3. The user reported this again after a recent test run, so the item is not trustworthy as resolved.
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
  - New hardening in this tranche:
    - `AGENTS.md` now has a non-negotiable terminal-isolation section forbidding inherited-tty mutation and process-level `SIGWINCH` stimulus in tests/helpers.
    - `internal/session/viewport_resize_signal_regression_integration_test.go` was removed so signal-driven resize regressions no longer exist in the test suite.
  - Remaining verification gap:
    - rerun the relevant narrow package checks after the signal-driven file removal and confirm no new inherited-tty path remains.

## Recently Resolved Or Reverified

### B-018 `make test-webui` reruns the wrong Web UI package set

- Status: `resolved`
- Area: `build`, `tests`, `webui`
- Summary: `make test-webui` must run the actual Web UI integration package set, not `go test -tags webui ./...`.
- Report:
  The original target took far too long because it reran the whole repository with the `webui` tag instead of only the package(s) that contained Web UI browser integration tests.
- Repro:
  1. Run `make test-webui`.
  2. Observe that the old target executed `go test -count=1 -tags webui -json ./...`.
  3. The run covered the full repo rather than the Web UI integration package set.
- Investigation notes:
  - Web UI browser integration tests now live under `./integration/webui` and use the `integration` build tag.
  - The Make target runs an explicit package list via `WEBUI_TEST_PKGS`, defaulting to `./integration/webui`.
- Regression coverage:
  - Makefile contract update only; no Go test added.
- Verification:
  - `make test-webui`
  - `rg -n --glob '*_test.go' '//go:build integration|\\+build integration' integration/webui`

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
- Summary: Local Android integration runs can opt into keeping a script-started emulator alive with `LINGON_IT_KEEP_EMULATOR=1`, and always reset app state between cases.
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
  - `android/scripts/run-integration-tests.sh` stops a script-started emulator by default to avoid leaving expensive processes behind; set `LINGON_IT_KEEP_EMULATOR=1` to preserve the previous reuse behavior.
  - The runner resets `systems.pkt.lingon` and `systems.pkt.lingon.test` state between per-test instrumentation invocations and clears device-side test artifacts before each case.
  - `adb reverse --remove-all` is issued before re-establishing the harness port mapping so reuse does not accumulate stale reverse mappings.
- Verification:
  - `bash -n android/scripts/run-integration-tests.sh`
  - Code-path review confirming:
    - cleanup kills a script-started emulator unless `LINGON_IT_KEEP_EMULATOR=1`
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

### B-021 `lingon -x detach` leaves stale headless sessions behind across UIs

- Status: `resolved`
- Area: `headless`, `relay`, `attach`, `host-remote`, `android`
- Summary: Force-detaching a local headless session must stop the PTY and remove the session cleanly from relay/UI state without reconnect grace or stale unreachable tabs.
- Report:
  `lingon -x detach` left the dead headless session behind in attach, host remote multi-client, and Android. The UI showed reconnect/lost-session behavior even though the session had been force-stopped and there was nothing to reconnect to.
- Repro:
  1. Start a relay-backed headless session.
  2. View it from `lingon attach`, host remote multi-client, or the Android app.
  3. Run `lingon -x detach`.
  4. Observe the dead session linger with reconnect/lost-session behavior instead of disappearing cleanly.
- Investigation notes:
  - `detachLocalHeadlessSession` was bypassing the daemon and directly deleting local headless store state before killing the daemon/socket.
  - That skipped the explicit `session_closed` path, so relay consumers treated the disappearance like a disconnect instead of a real close.
  - Android also needed explicit `session_closed` handling so closed sessions are removed immediately instead of entering missing-session grace.
- Regression coverage:
  - `integration/pty/attach.TestRealCLIRelayHeadlessDetachRemovesTerminatedSessionWithoutReconnectOverlay`
  - `integration/pty/attach.TestRealCLILocalHeadlessDetachRemovesTerminatedSessionWithoutReconnectOverlay`
  - `integration/pty/session.TestHostRemoteHeadlessDetachRemovesSessionWithoutReconnectOverlay`
  - Android instrumentation:
    - `headless_detach_removes_session_without_reconnect_placeholder`
- Implementation notes:
  - Added daemon-mediated `headless.DetachSession(...)` so detach requests go through the headless daemon first and emit a clean in-band close before shutdown.
  - Added `/internal/headless/detach` handling in `internal/headlessd.Daemon`.
  - Added `Runner.StopSession(...)` and `localSession.StopWithReason(...)` so detach can propagate a reasoned close through the existing session-close path.
  - `cmd/lingon/headless_local.go` now uses `headless.DetachSession(...)`.
  - Android `AppViewModel` now handles explicit `session_closed` frames and removes the closed session immediately instead of retaining it via missing-session grace.
- Verification:
  - `go test -count=1 -tags integration ./integration/pty/attach -run 'TestRealCLI(RelayHeadlessDetachRemovesTerminatedSessionWithoutReconnectOverlay|LocalHeadlessDetachRemovesTerminatedSessionWithoutReconnectOverlay)'`
  - `go test -count=1 -tags integration ./integration/pty/session -run 'TestHostRemoteHeadlessDetachRemovesSessionWithoutReconnectOverlay'`
  - Android targeted e2e:
    - `PATH=\"$HOME/Android/Sdk/platform-tools:$PATH\" LINGON_IT_ONLY=headless_detach_removes_session_without_reconnect_placeholder ./scripts/run-integration-tests.sh`
  - `go test -count=1 ./...`
  - `go vet ./...`
  - `golint ./...`
  - `golangci-lint run ./...`

### B-022 Android needs explicit headless-only resize and must not auto-resize on connect

- Status: `resolved`
- Area: `android`, `relay`, `headless`
- Summary: The Android app must never resize sessions implicitly. Only a headless session may be resized, and only through an explicit top-bar action. Non-headless sessions stay camera-only.
- Report:
  Android still auto-resized through the relay hello path, and there was no explicit headless-only resize action in the app.
- Repro:
  1. Connect the Android app to a headless session.
  2. Observe that connect-time viewport dimensions are sent in the websocket hello and can resize the headless PTY.
  3. Observe there is no explicit headless-only resize button in the UI.
- Regression coverage:
  - `AppViewModelTest.sendHeadlessResizeNow_sendsSingleResizeForActiveHeadlessSession`
  - `AppViewModelTest.connectActiveSession_doesNotAdvertiseViewportResizeInHello`
  - Android instrumentation:
    - `headless_resize_button_only_enables_for_headless_sessions`
    - `headless_resize_button_resizes_remote_headless_session`
- Implementation notes:
  - Relay session models and Android UI state now carry `headless` metadata.
  - Android websocket connect now uses `cols=0, rows=0`, so the app no longer auto-resizes through relay hello.
  - Added a top-bar `HeadlessResizeButton`:
    - visible whenever there is an active session
    - enabled only for headless sessions
    - disabled/dimmed for non-headless sessions
  - Pressing the button calls `sendHeadlessResizeNow()` and sends one explicit resize using the current viewport-derived terminal size.
  - The Android harness gained real headless control endpoints for e2e:
    - `start-headless`
    - `detach-headless`
    - `headless-size`
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin`
  - Android targeted e2e:
    - `PATH=\"$HOME/Android/Sdk/platform-tools:$PATH\" LINGON_IT_ONLY=headless_resize_button_only_enables_for_headless_sessions ./scripts/run-integration-tests.sh`
    - `PATH=\"$HOME/Android/Sdk/platform-tools:$PATH\" LINGON_IT_ONLY=headless_resize_button_resizes_remote_headless_session ./scripts/run-integration-tests.sh`
