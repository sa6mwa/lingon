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

### B-169 Podman Kube development service binds an unreachable loopback listener

- Status: `resolved`
- Real repro: The verified rehearsal image passes the matrix, but `make dev-up` reaches its readiness deadline. It creates owned TLS state, then automatically removes its pod (`build/lifecycle/dev-up-1755081943.log`). An isolated repeat captures `serve.http.start.init` listening on `127.0.0.1:12843` inside the container; the published host port cannot reach that loopback listener (`build/lifecycle-dev-diagnostic.log`).
- Plan: Bind the service to its container network interface while retaining the host's loopback-only published port. Capture pod diagnostics before bounded owned teardown, including cancellation. `TestDevCancellationKeepsDiagnosticsAndOwnsTeardown` checks that a canceled readiness context cannot suppress logs or leave the owned pod running.
- Verification: The cancellation fixture fails before (`build/lifecycle-dev-cancel-before.log`) and the complete dev-focused race cases pass three runs after (`build/lifecycle-dev-unit-after.log`). Real rootless Kube up now passes CA-verified readiness, and a second explicit HTTPS health request returns `{"status":"ok"}` (`build/lifecycle-dev-up-after.log`, `build/lifecycle-dev-health-after.log`). Owned state and CA are UID/GID 1000, mode 700/600. Ordinary-user dev-down/reset pass, the pod is absent and `build/devenv` is removed (`build/lifecycle-dev-{down,reset}-after.log`). Root tests, vet, golint and golangci-lint pass (`build/lifecycle-dev-{go-test,vet,golint,golangci}.log`, zero lint issues). No privileged cleanup, home mounts or lingering service was used.

### B-168 Broader integration exposes publisher and raw-mode descriptor races

- Status: `resolved`
- Report: The complete post-B-166 integration rerun (`build/lifecycle-integration-full-after.log`) passes every browser case but detects unsynchronized publisher control ownership, HELLO dimensions racing with resize, and raw-mode descriptor access racing with owned teardown. It also exposes a transient `ps aux` command-echo observation in the multi-viewport fixture.
- Plan: Capture publisher mutable fields under its existing mutex, borrow descriptors for terminal startup/size operations, and retain the viewport fixture's 500 ms response assertion with a test-owned output gate so fast process output cannot erase its echo before observation. Preserve permanent flicker assertions and isolate every terminal control.
- Regression/fix: Concurrent ownership and owned raw-mode close tests fail before (`build/lifecycle-publisher-raw-before.log`) and pass ten runs after. `helloFrame` captures name/dimensions under the same mutex used by Resize; TakeControl captures ownership in its existing critical section. Terminal detection, raw setup and viewport reads use descriptor-retaining helpers; nil/pipe/live/closed owned-PTY behavior passes ten runs (`build/lifecycle-publisher-terminal-unit-final.log`). The test-owned FIFO preserves the 500 ms echo assertion and adds explicit real-output completion coverage.
- Additional matrix findings: Protect credential reads/writes with the client mutex; capture session/token/theme state and claim connection setup under the manager mutex. Publish fully initialized clients and make each worker capture its own client. Consume render-baseline flags and capture client pointers under the manager mutex, with rendering outside it. Intermediate repeated matrices exposed these races (`build/lifecycle-{publisher-raw-pty-after,remote-matrix-after,remote-matrix-final}.log`); the complete seven-scenario host/attach matrix then passes three race runs (`build/lifecycle-remote-render-matrix-after.log`). No permanent assertion changed.
- Final verification: Complete publisher, terminal, attach, session, lifecycle, PTY-harness and observer race suites pass (`build/lifecycle-remediation-full-race.log`); permanent no-flicker/row-overlay and targeted input/prune/viewport PTY cases pass (`build/lifecycle-remediation-permanent-pty.log`). Root tests/vet/golint/golangci and JavaScript syntax pass (`build/lifecycle-remediation-{go-test,vet,golint,golangci,js-syntax}.log`, zero lint issues). No input/frame lock is added for ownership capture; new resize and connection-state synchronization has behavioral/race proof, without an invented throughput claim. A complete clean-tree release rehearsal remains required for the lifecycle task.

### B-167 Missing pager silently reduces release integration coverage

- Status: `resolved`
- Finding: Developer-focused Linux PTY cases skip if `/bin/bash` or `less` is missing. A successful Go exit alone could therefore violate the lifecycle's missing-tools blocker contract.
- Regression: `TestMissingPagerCannotSilentlySkipReleaseCoverage` injects a missing pager and requires proof to stop before expensive tests. It fails before the fix (`build/lifecycle-pager-before.log`).
- Fix: Check the exact required shell and pager noninteractively before the expensive proof graph; document the optional explicit-baseline startup comparison separately from required benchmarks.
- Verification: Complete lifecycle race suite and standard checks pass in `build/lifecycle-remediation-{full-race,go-test,vet,golint,golangci}.log`. `/bin/bash` and `/usr/bin/less` are present for the real full rehearsal. The missing-pager fixture stops before any Go test gate, without weakening developer test assertions.

### B-166 Full integration release gate exposes teardown, observer and input failures

- Status: `resolved`
- Report: The fourth clean rehearsal (`7d3569b`) passes all root/Android-tool Go tests and races, then stops in the full integration gate. Thirteen cases fail across attach, host and browser packages; evidence is retained in `build/lifecycle/prerelease-1467734321.log`.
- Findings under investigation: Unsynchronized captured clients/PTY output buffers; harness reader startup and host terminal detection racing with owned descriptor close; dead active-session reconnect overlay; stray mouse reports forwarded to a headless shell; wheel/input completion mismatch; browser management auth recovery.
- Constraint: Preserve all behavioral and permanent flicker assertions. Reproduce and repair each failure, then rerun the complete integration gate before a new clean full rehearsal. No Android/artifact/publication gates ran after the failure.
- Fixes: Atomic captured-client observers and synchronized PTY-output snapshots retain existing initial-frame/prompt assertions. Cancel and join the owned screen reader before closing its PTY. Stop/join the client's owned render worker before terminal cleanup and ignore late render notifications. Refresh browser management availability on WebSocket open/close. The broader run passes every browser case (`build/lifecycle-integration-full-after.log`); its remaining failures are tracked as B-168, not treated as a clean full gate.
- Input regression: Consume SGR mouse reports incrementally across read boundaries, retaining only an ambiguous ESC/CSI prefix. A standalone ambiguous prefix flushes after 50 ms of injected clock time; ordinary text and complete non-mouse keys have no added wait. Preserve scrollback's wheel handling. Exhaustive byte/read-boundary tests, both Escape-key PTY cases, wheel behavior and actual headless-shell mouse filtering pass; six targeted cases pass three race runs (`build/lifecycle-integration-input-prune-after.log`).
- Grace regression: `TestMissingSessionGraceExpiresWhileNetworkGateIsBlocked` drives real owned-PTY tab keys and a discovery receiver event, explicitly advances mock time through five-second grace, then asserts surviving-view input. It fails before (`build/lifecycle-missing-prune-before.log`) and passes after. Expire established absence locally, serialized with fresh discovery, instead of waiting for a blocked network refresh; retain network backoff policy.
- Evidence: Immediate owned-reader cancellation fails before and passes ten runs after (`build/lifecycle-integration-harness-before.log`, `build/lifecycle-integration-core-after.log`); browser recovery/control pass five runs (`build/lifecycle-webui-recovery-after.log`). The final complete affected race suites, permanent PTY cases and standard checks pass as recorded in B-168. Three ordinary 1,920-byte input-filter samples measure 37.71/37.60/36.03 ns with zero bytes/allocations (`build/lifecycle-mouse-filter-bench.log`); this is focused filter cost, not overall transport throughput or an Escape-latency claim.

### B-165 Terminal-attribute ioctl races with owned PTY shutdown

- Status: `resolved`
- Report: The third full rehearsal at `5b3b45c` stops in the root race gate: relay-host retry cleanup closes the owned PTY while a publisher ownership callback calls `setVEOF`. Evidence: `build/lifecycle/prerelease-2897003153.log`.
- Regression: `TestTermiosConcurrentCloseUsesOwnedDescriptor` creates fresh isolated PTY pairs with no child or inherited stdio, checks successful live EOF operations, then joins concurrent close and terminal-attribute calls. Three runs fail against the original code, detecting races in EOF, input-filter and echo-control helpers (`build/lifecycle-termios-before.log`).
- Fix: Use `SyscallConn.Control` to retain the descriptor through each termios ioctl, including read/modify/write and echo restoration. A concurrent close returns a normal file error instead of exposing a destroyed/reused descriptor. No session/render locks or terminal semantics changed.
- Verification: Complete session and relay-host suites pass three race runs (`build/lifecycle-termios-after.log`); the final descriptor wrapper passes ten close-regression race runs (`build/lifecycle-termios-final-race.log`). Root Go tests, vet, golint and golangci-lint pass (`build/lifecycle-termios-final-{go-test,vet,golint,golangci}.log`). A temporary benchmark-source backup initially made Go discover a spurious build package; rename it to a non-Go suffix before the passing full rerun. No gate was bypassed.
- Performance: Three owned-descriptor read samples measured 210.8/213.4/225.4 ns versus original 336.3/333.3/334.2 ns, retaining zero bytes and allocations per operation (`build/lifecycle-termios-bench-{before,final}.log`). An intermediate wrapper allocated 48 bytes and was replaced before acceptance. These are narrow unpaired observations, not an application-wide speed claim. A fresh complete release rehearsal is still required.

### B-164 Publisher ping cancellation races with a queued timer tick

- Status: `resolved`
- Report: The second clean `make prerelease` rehearsal (`badc054`) stops at the root race gate on `TestPublisherPingLoopSkipsWhileWriteBusy`; the earlier B-163 failures are absent. `build/lifecycle/prerelease-3203614489.log` records `failed to ping: use of closed network connection, want nil after cancellation`.
- Finding: A selected ticker event can reach Ping after the parent context is canceled; the busy-writer fixture also uses a 150 ms wall-clock wait as timing proof. Preserve the full gate, convert timing proof to explicitly advanced mock time, and distinguish intentional cancellation from an actual ping failure.
- Regression: `TestPublisherCanceledQueuedPingReturnsWithoutReconnect` queues a real mock ticker event before entering the canceled loop and asserts a normal return without reconnect/failure recovery. Three race runs fail before the production fix (`build/lifecycle-ping-cancel-before.log`); ten runs pass after (`build/lifecycle-ping-cancel-after.log`). The busy-writer fixture now waits for ticker creation, advances mock time and observes the idle check; its wall-clock delay is removed.
- Fix: Check parent cancellation after selecting a tick and after the serialized Ping completes. Return normally for deliberate cancellation before clearing connection state or triggering failure recovery; genuine non-cancellation ping errors retain their existing handling. Changes affect the periodic ping path, not input/frame serialization or allocations; no relevant ping performance gate exists and no latency improvement is claimed.
- Verification: Complete publisher race suite passes (`build/lifecycle-publisher-race.log`), followed by root tests/vet/golint/golangci-lint (`build/lifecycle-ping-{go-test,vet,golint,golangci}.log`, zero lint issues). Owned transport fixtures close their sockets/servers; no inherited terminal is used. A fresh complete release rehearsal is still required after the remediation commit.

### B-163 Full local CI race gate exposes unjoined fixture callbacks and remote diagnostics

- Status: `resolved`
- Report: The first committed-tree `make prerelease` rehearsal of the Go lifecycle (`14ec962`) correctly stops at the full root race gate. No release branch, tag or publication mutation occurred.
- Repro: `setsid --wait env JAVA_HOME=/usr/lib/jvm/java-21-openjdk-amd64 GOMAXPROCS=2 GOFLAGS=-p=1 make prerelease </dev/null`; command log `build/lifecycle/prerelease-2032895291.log`, outer log `build/lifecycle-prerelease.log`.
- Findings: Attach expiry and MVU scheduler fixtures inspect mock-clock callbacks before completion; three headless fixtures share unsynchronized output buffers; the forwarded-wall fixture shares callback fields without synchronization; the relay persistence/ping fixture returns before asynchronous save cleanup finishes; remote-view diagnostic logging reads mutable view fields without its manager lock.
- Failing cases: `TestPrepareForCtrlLClearPreservesStateExpiryWithoutRemoteSnapshot`, `TestDaemonAttachAndSendViaUnixSocket`, `TestDaemonReplaysPublishStatusToNewAttachClient`, `TestDaemonInactivityWallFiresOnceUntilNewActivity`, `TestDaemonForwardedInternalWallPreservesKindForAttachClients`, `TestEffectSchedulerScheduleReplaceStopAll`, `TestHostPingAnsweredWhileRegistrationPersists`, `TestHostTabBarToggleDoesNotStallInput`.
- Verification plan: Keep the race and release gates intact. Isolate supporting fixes, prove before/after behavior, commit, then start a new full rehearsal. Permanent no-flicker assertions remain unchanged.
- Additional findings: The same owned-PTY tab-input case exposed an unlocked host baseline reset and an observer reading the renderer's private `needsFullRender` flag. Lock baseline resets with the existing stdout/render mutex and retain the actual remote echo as the readiness/routing assertion. The headless inactivity fixture's command text contained its own output nonce; use formatted output whose completed nonce is absent from command echo (ten race repetitions pass). The host ping/persistence test joined its handler, but that handler did not join its session-list stream; all three WebSocket role paths now cancel and join their stream before returning.
- Fix proof: A new `TestRemoteViewDiagnosticsConcurrentClose` drives 1000 concurrent diagnostic/close operations without terminals or network. The corrected retained-view fixture fails three race runs against the old production logger (`build/lifecycle-diagnostics-before.log`) and passes after capturing only diagnostic fields under the existing manager mutex; logger I/O remains outside it. A shared synchronous test clock joins callbacks after explicit mock advancement; its Stop/Reset/deadline contract has unit coverage. Headless output observers take immutable mutex-protected snapshots, and forwarded wall results use a channel rather than shared fields.
- Passing verification: All original failing cases plus new regressions pass three focused race runs (`build/lifecycle-expanded-race-after.log`), followed by complete affected attach/headless/MVU/session/testclock/relay race suites (`build/lifecycle-full-affected-race.log`). Actual owned-PTY create/offline-create and permanent host/attach no-flicker gates pass (`build/lifecycle-support-pty.log`); assertions are unchanged. Root Go tests, vet, golint and golangci-lint pass (`build/lifecycle-support-{go-test,vet,golint,golangci}.log`, zero lint issues). Source restoration for baseline builds was checked byte-for-byte.
- Android boundary proof: Three targeted owned-device cases pass with zero errors/failures/skips: terminal creation/rename, headless resize and owner typing after a real handoff (`build/lifecycle-support-connected.log`; per-batch XML under `android/test-artifacts/resource-profile-20261006-025531-2271122`). The serial capped runner cleaned up its owned resources. This is targeted verification; the fresh full local CI rehearsal remains required.
- Performance: Three same-location, alternating 20-pair startup comparisons before/after the supporting production changes measured medians 30.199/30.894, 30.769/31.827, 31.827/31.688 ms (+2.3%, +3.4%, -0.4%; `build/lifecycle-fix-startup-paired.log`). No consistent startup slowdown was isolated and no speedup is claimed. A focused no-op diagnostics benchmark measures median 105.9 -> 109.7 ns/op, +3.8 ns for the synchronized snapshot; 408 B and 4 allocations remain unchanged (`build/lifecycle-diagnostics-bench-{before,after}.log`). This is a diagnostic microbenchmark, not an input-latency SLA.

### B-162 Expanded session race verification exposes startup and fixture races

- Status: `resolved`
- Report: The full `internal/session` race sweep for B-161 fails (`review-management-geometry-session-race.log`), although the new geometry regression and real PTY gates pass. Failures cover attach callbacks published before assignment, redundant startup clock writes, test reads of unready session/client state, PTY resize versus close, and asynchronous notification/TTY fixture cleanup. Notification tests also observe callback state before completion.
- Scope: Establish baseline evidence, then fix confirmed synchronization/publication defects and make the existing fixtures join their asynchronous work. Preserve every observable assertion and permanent no-flicker regression. All exposed failures must be addressed before goal completion; the geometry fix alone does not establish a clean race suite.
- Regression evidence: The existing failing tests and exact conflicting stack locations are retained in the log. Individual failure modes and remediation evidence will be recorded after triage.
- Baseline evidence: With the three geometry production files restored to `5021249`, the selected existing startup/notification/tab cases still fail under the race detector (`review-expanded-session-race-baseline.log`). The trap restores the exact geometry fix afterward. This demonstrates pre-existing failures, not a passing baseline race suite. Those failures blocked overall completion until the remediation below; B-161 is independently proven in its own entry.
- Confirmed failure modes and fixes:
  - Attach readers receive sessions/status before `applySessions`, refresh and missing-session timer callbacks are assigned. Start the first view only after all those callbacks are initialized. Existing `TestAttachSharedConfigRoot` and auth-restart cases fail with conflicting startup/callback stack traces before; they pass after. No callback lock or per-frame work was added.
  - Runner startup redundantly writes the clock already initialized by `New`, and initializes local maps outside the mutex used by observers. Remove the redundant clock write/defaulting and publish both maps under `localMu`. Existing Ctrl+L creation, respawn, offline, muted-tab and quit cases fail before and pass after.
  - PTY resize retrieves `File.Fd` while Close destroys or changes the descriptor/poll state. Borrow the descriptor with `SyscallConn.Control` through the window-size ioctl; retain its blocking mode and release it before returning. `TestResizeConcurrentCloseUsesOwnedDescriptor` owns fresh isolated PTY pairs, starts no child and sends no signal; it fails with actual FD races before and passes after. It verifies successful resize before Close begins and permits the platform's closed-file error once Close starts. The first post-fix fixture incorrectly assumed the error must match `os.ErrClosed`/EBADF (`review-expanded-session-notification-after.log`); that assertion was corrected, then the corrected test again failed against the original code (`review-expanded-session-pty-corrected-before.log`). This fixture correction is not claimed as a production fix.
  - Concurrent inactivity callbacks initialize and mutate `opts.DesktopNotifier` repeatedly. Keep options immutable, initialize the private notifier once, and publish it through `sync.Once`. New `TestRunnerConcurrentInactivityInitializesNotifierOnce` gates the real factory, joins eight concurrent callbacks and asserts one initialization plus all eight deliveries. Before, the factory ran two to eight times and races were reported (`review-expanded-session-extra-before.log`); after, every delivery occurs through the single notifier.
  - Notification fixtures read request slices, restore the factory and inspect a lazily created runtime while mock timer callbacks are still running. A test-specific injected clock dispatches expired timer-channel callbacks synchronously after explicit mock advancement. It forwards Stop/Reset to the underlying mock timer; assertions run after the notification and modal work finishes. Production clocks and notification timing were not changed. Existing factory, per-session, activity-rearm, disabled/cancelled and sibling-modal assertions are preserved and pass.
  - The owned TTY fixture reads/resets a bytes.Buffer concurrently with its reader and treats a ten-millisecond sleep as output completion. Protect the buffer and append a private barrier to that fixture's slave; readback waits for the reader to consume it, then snapshots under the mutex. Its three-second transport deadline only bounds a broken reader, and cleanup joins the reader. No sleep, inherited terminal, raw mode or caller signal is used for this proof. Existing scrollback and mixed viewport assertions are unchanged.
  - The tab-input fixture reads a client ID and view map during startup mutation. Wait for the actual relay's single B viewer registration instead; that test's topology has exactly one viewer of B. Input routing assertions are preserved, and `TestHostTabBarToggleDoesNotStallInput` now passes without racing private startup fields.
- Passing verification: Focused existing failures and both new regressions pass three race runs (`review-expanded-session-focused-after.log`). The complete `internal/attach`, `internal/pty` and `internal/session` race suites pass (`review-expanded-session-full-race-after.log`). Final notification fixture assertions pass another three race runs after a style-only promoted-method selector cleanup (`review-expanded-session-clock-final.log`). Real PTY creation/offline creation, resize matrix/bursts, twenty reader-shutdown cycles, reconnect row-overlay and permanent host/attach no-flicker gates pass (`review-expanded-session-pty-after.log`). No permanent assertion was removed or weakened.
- Platform evidence: `internal/pty` and its new test compile for Darwin arm64 with CGO disabled (`review-expanded-session-darwin-compile.log`). Linux runtime tests prove the actual close/resize behavior; no macOS runtime verification is claimed.
- Performance: A binary built at `32ac308` is the baseline for the controlled same-location startup comparison. Three 20-pair baseline/fixed medians were 25.978/29.164, 23.261/26.056, 25.356/25.157 ms (+12.3%, +12.0%, -0.8%). Because the first two were slower, repeat rather than dismiss them: three further runs measured 26.495/26.875, 24.012/23.932, 26.009/26.218 ms (+1.4%, -0.3%, +0.8%). P95 pairs for those repeats were 33.783/32.931, 31.169/35.077, 33.229/33.569 ms. The earlier slowdown did not repeat; no speedup or absolute startup SLA is claimed. All 120 measured pairs alternate executable order and detach each child before the next. Callback publication adds no input/frame lock; the notifier's once guard runs only on notification delivery. No relevant resize throughput gate exists; descriptor-race, real resize matrix/burst and input/no-flicker checks provide its focused behavioral proof.
- Android evidence: Unit tests and instrumentation compilation pass (`review-expanded-session-android-unit.log`). Three targeted device cases pass, zero failures/errors/skips: actual creation/rename, explicit headless resize and authorized first typing after a real control handoff (`review-expanded-session-connected.log`; XML under `android/test-artifacts/resource-profile-20261004-165544-1495593/android-test-results`). This rechecks the requested transparent control behavior without an extra button against the final backend paths. The capped serial runner cleaned up its owned emulator/adb/harness resources; no full Android release sweep ran.
- Standard checks: Root tests, vet and golint pass (`review-expanded-session-{go-test,vet,golint}.log`). Golangci-lint initially reported QF1008 on the test clock's explicit embedded-field selector; use the equivalent promoted method, rerun its race tests and obtain zero lint issues (`review-expanded-session-golangci-final.log`). Formatting, full worktree/source readback and diff checks pass. Only the tracker and seven documented attach/PTY/session paths belong to this batch; no unrelated/concurrent edits or staged changes were found.
- Validation command ledger (root, strictly serialized unless the working directory is stated):
  - Prefix `setsid --wait env GOMAXPROCS=2 GOFLAGS=-p=1`, suffix `</dev/null`, retained root logs for Go commands below.
  - The broad/failing baseline commands are recorded in B-161. `go test -race -p 1 ./internal/pty ./internal/session -run '^Test(ResizeConcurrentCloseUsesOwnedDescriptor|RunnerConcurrentInactivityInitializesNotifierOnce)$' -count=10 -timeout=60s` failed both new regressions before.
  - `go build -o review-expanded-session-baseline-binary.log ./cmd/lingon` built the `32ac308` performance baseline before production edits.
  - `go test -race -p 1 ./internal/pty ./internal/session -run '^Test(ResizeConcurrentCloseUsesOwnedDescriptor|RunnerConcurrentInactivityInitializesNotifierOnce|Runner.*Wall.*)$' -count=10 -timeout=90s` passed notification tests but failed the closed-file error assumption described above. Temporarily restore `internal/pty/pty.go` from HEAD and run the corrected `go test -race -p 1 ./internal/pty -run '^TestResizeConcurrentCloseUsesOwnedDescriptor$' -count=10 -timeout=30s`; it fails with FD races. A trap and `cmp` restore the exact fixed source.
  - `go test -race -p 1 ./internal/pty ./internal/session -run '^Test(ResizeConcurrentCloseUsesOwnedDescriptor|RunnerConcurrentInactivityInitializesNotifierOnce|Runner.*Wall.*|AttachSharedConfigRoot|No401OnExpiredAuthAfterRelayRestart|CtrlL.*|HostTabBarToggleDoesNotStallInput)$' -count=3 -timeout=180s` passed.
  - `go test -race -p 1 ./internal/attach ./internal/pty ./internal/session -count=1 -timeout=180s` passed the complete affected race suites.
  - With `LINGON_TEST_STARTUP_BASELINE_BINARY=/home/mike/g/lingon/review-expanded-session-baseline-binary.log`, `go test -p 1 -tags integration ./integration/pty/attach -run '^TestSessionManagementStartupBaselineComparison$' -count=3 -v -timeout=120s` passed twice (`review-expanded-session-startup-paired{,-repeat}.log`), retaining all measured values.
  - With `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0`, `go test -c -o review-expanded-session-darwin-pty-test-binary.log ./internal/pty` passed compilation.
  - `go test -race -p 1 -tags integration ./integration/pty/attach ./integration/pty/session -run '^Test(SessionManagement(LocalCLI|RelayAndHeadfulCLI|OfflineCredentialsStayOfflineUntilExplicitToggle)|RealCLILocalHeadlessResizeMatrixRendersExpectedViewport|AttachLargeResizeBurst|AttachNoFullRedrawAfterTabSwitchWithLocalPTYs|AttachNoFullRedrawOnOnlineWrapAroundTabSwitch|AttachReconnectDoesNotRepaintBaseTopRowBeforeOverlay|HostSIGWINCHResizesLocalPTY|HostLocalOnlyNoFullRedrawOnWrapAroundTabSwitch|HostNoFullRedrawOn(Offline|Online)WrapAroundTabSwitch|HostReconnectDoesNotRepaintBaseTopRowBeforeOverlay|HostResizeDoesNotClearScreen|LocalSessionCancelWaitsForPTYReaderBeforeClose)$' -count=1 -timeout=180s` passed.
  - `go test -p 1 ./...`, `go vet ./...`, `golint ./...` passed. `golangci-lint run ./...` failed the selector style issue, then passed after correction. `go test -race -p 1 ./internal/session -run '^TestRunner.*Wall.*$' -count=3 -timeout=60s` passed the final helper after that correction. Formatting, readback and `git diff --check` passed.
  - From `android`, `setsid --wait env JAVA_HOME=/usr/lib/jvm/java-21-openjdk-amd64 ./gradlew --no-daemon --max-workers=2 -Dorg.gradle.parallel=false -Dkotlin.compiler.execution.strategy=in-process '-Dorg.gradle.jvmargs=-Xmx1536m -Dfile.encoding=UTF-8' :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin </dev/null` passed.
  - From root, `setsid --wait env JAVA_HOME=/usr/lib/jvm/java-21-openjdk-amd64 LINGON_IT_CPU_QUOTA_PERCENT=200 LINGON_IT_MEMORY_MAX=7G LINGON_IT_ONLY=settings_create_terminal_and_rename_session,headless_resize_button_resizes_remote_headless_session,owner_typing_reclaims_control_after_real_handoff ./android/scripts/run-integration-tests.sh </dev/null` passed.
- Remaining gap: A clean prescribed normal review is still required after the remediation commit.

### B-161 Detached creation races with terminal geometry updates

- Status: `resolved`
- Report: Normal review `codex-review.normal-trunk.yRQJ5w.log` at `5021249` found detached creation reading `r.opts.Cols/Rows` while daemon resize writes them through `Runner.ResizeActive`. Management serialization does not cover resize; the child can receive a mixed pair.
- Scope: Protect the runner's width/height pair with a private geometry lock and use coherent snapshots for detached creation and existing dimension consumers. Preserve resize behavior and keep terminal/network I/O outside this lock.
- Regression: `TestDetachedCreationCapturesCoherentGeometryDuringResize` calls real management and detached launch while dimensions alternate. A noninteractive test executable captures the actual `--cols/--rows` through its owned readiness pipe and intentionally reports failure before starting any terminal. Each pair must be 80x24 or 120x40. The inert parent identity owns no PTY; no Run/newLocalSession/raw mode/inherited stdio is used, and the resize worker is joined.
- Failing evidence: The new regression fails against `5021249`: both race reports and actual mixed arguments (`120x24` and `80x40`) are captured in `review-management-geometry-before.log`. No setup or compilation failure is substituted for defect evidence.
- Fix: A private geometry RWMutex protects the existing width/height pair. Detached creation, local/publisher construction, rendering fallbacks, control acquisition and resize-if-changed consume one coherent snapshot. Startup resolves partial/default dimensions into local variables and publishes the pair once; resize publishes both dimensions together. Source inspection confirms all production access to `r.opts.Cols/Rows` now occurs inside the two synchronized helpers. File/terminal/process/render/network work remains outside this lock.
- Passing evidence: Ten race runs capture 200 real detached-launch argument pairs with no mixed dimensions (`review-management-geometry-after.log`). Existing PTY local/relay/headful/offline creation, local headless resize matrix, host resize and permanent attach/host no-clear/no-flicker gates pass (`review-management-geometry-pty.log`), unchanged assertions. Required root tests, vet, golint and golangci-lint pass (`review-management-geometry-{go-test,vet,golint,golangci}.log`, zero lint issues). The broader optional session race sweep fails on pre-existing cases and is explicitly tracked in B-162; it is not claimed as passing.
- Android evidence: Unit tests and instrumentation compilation pass (`review-management-geometry-android-unit.log`). Actual app creation/rename and explicit headless resize pass together, two tests with zero failures/errors/skips (`review-management-geometry-connected.log`; results under `android/test-artifacts/resource-profile-20261004-161412-1455512/android-test-results`). The capped serial runner cleaned up its owned emulator/adb/harness resources. No full Android release sweep was run.
- Performance: Existing five-alternating-launch gate measured direct/routed 22.113/30.511 ms before and 23.632/37.930 ms after. Six further paired invocations, with run order reversed for the last three, measured routed baseline 38.780, 35.883, 32.043, 35.783, 34.371, 30.888 ms versus fixed 35.668, 30.209, 31.453, 37.161, 30.414, 30.712 ms. Direct baseline 22.587, 27.406, 27.933, 27.161, 31.082, 20.054 ms versus fixed 30.421, 32.730, 28.912, 29.459, 25.158, 25.787 ms required closer investigation rather than a no-regression claim.
- Startup measurement control: Added optional `TestSessionManagementStartupBaselineComparison`, using the same harness, alternating binary order across 20 pairs and detaching each child before the next. Its first run and three repeats used different executable locations and measured +6.2%, +5.0%, +7.2%, +11.1%; those values are not accepted as an isolated source-change effect. Matching directory, filesystem, filename length and permissions produced three 20-pair baseline/fixed medians: 25.179/26.287, 27.069/24.729, 27.247/26.732 ms (+4.4%, -8.7%, -1.9%). P95 pairs were 31.133/38.259, 31.303/30.599, 33.819/30.688 ms. No repeatable slowdown was isolated in that controlled gate; no speedup or absolute startup SLA is claimed. The baseline binary was built from the committed production source with the same toolchain/flags, and all temporary source switches were restored byte-for-byte before verification.
- Validation command ledger (serialized, root unless noted):
  - Prefix `setsid --wait env GOMAXPROCS=2 GOFLAGS=-p=1`, suffix `</dev/null`, retained root logs for the Go commands below.
  - `go test -race -p 1 ./internal/session -run '^TestDetachedCreationCapturesCoherentGeometryDuringResize$' -count=1 -timeout=60s` failed before; `-count=10 -timeout=90s` passed after.
  - `go test -p 1 -tags integration ./integration/pty/attach -run '^TestSessionManagementStartupPerformance$' -count=1 -v -timeout=90s` ran before/after and twelve further alternating invocations (`review-management-geometry-startup-{before,after,baseline-1..6,fixed-1..6}.log`). Baseline invocations used the three production files from `git show HEAD:<path>`; trap restoration and `cmp` checks passed.
  - `go build -o review-management-geometry-baseline-binary.log ./cmd/lingon` built the committed baseline before restoring the three fixed sources.
  - With `LINGON_TEST_STARTUP_BASELINE_BINARY=/home/mike/g/lingon/review-management-geometry-baseline-binary.log`, `go test -p 1 -tags integration ./integration/pty/attach -run '^TestSessionManagementStartupBaselineComparison$' -count=1 -v -timeout=120s`, then `-count=3`, ran the initial location-confounded gate (`review-management-geometry-startup-paired{,-repeat}.log`). The corrected same-location gate passed `-count=3` (`review-management-geometry-startup-paired-same-location.log`).
  - `go test -race -p 1 -tags integration ./integration/pty/attach ./integration/pty/session -run '^Test(SessionManagement(LocalCLI|RelayAndHeadfulCLI|OfflineCredentialsStayOfflineUntilExplicitToggle)|RealCLILocalHeadlessResizeMatrixRendersExpectedViewport|AttachNoFullRedrawAfterTabSwitchWithLocalPTYs|AttachNoFullRedrawOnOnlineWrapAroundTabSwitch|HostSIGWINCHResizesLocalPTY|HostLocalOnlyNoFullRedrawOnWrapAroundTabSwitch|HostNoFullRedrawOn(Offline|Online)WrapAroundTabSwitch|HostResizeDoesNotClearScreen)$' -count=1 -timeout=180s` passed.
  - `go test -race -p 1 ./internal/session -count=1 -timeout=180s` failed on B-162. With the three geometry production files temporarily restored to HEAD, `go test -race -p 1 ./internal/session -run '^Test(AttachSharedConfigRoot|No401OnExpiredAuthAfterRelayRestart|CtrlL.*|Runner.*Wall.*|HostTabBarToggleDoesNotStallInput)$' -count=1 -timeout=120s` confirmed baseline failures (`review-expanded-session-race-baseline.log`); the fixed source was restored on exit.
  - `go test -p 1 ./...`, `go vet ./...`, `golint ./...`, `golangci-lint run ./...`, formatting, source-access audit, tracker readback and `git diff --check` passed after restoration.
  - From `android`, `setsid --wait env JAVA_HOME=/usr/lib/jvm/java-21-openjdk-amd64 ./gradlew --no-daemon --max-workers=2 -Dorg.gradle.parallel=false -Dkotlin.compiler.execution.strategy=in-process '-Dorg.gradle.jvmargs=-Xmx1536m -Dfile.encoding=UTF-8' :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin </dev/null` passed.
  - From root, `setsid --wait env JAVA_HOME=/usr/lib/jvm/java-21-openjdk-amd64 LINGON_IT_CPU_QUOTA_PERCENT=200 LINGON_IT_MEMORY_MAX=7G LINGON_IT_ONLY=settings_create_terminal_and_rename_session,headless_resize_button_resizes_remote_headless_session ./android/scripts/run-integration-tests.sh </dev/null` passed.
  - Completed normal review `codex-review.normal-trunk.yRQJ5w.log` passed `setsid --wait env GOMAXPROCS=2 go test -count=1 -p 1 ./internal/relay ./internal/sessionmanage ./internal/backoff ./internal/headless ./internal/control ./internal/terminal/emu </dev/null`, the Android unit/compile command above and diff checks. Review scope: attached `androidurl` at `5021249` against pinned local `trunk`, prescribed model/reasoning. These earlier checks did not cover the new geometry concurrency case.
- Remaining gap: Resolve B-162 and obtain a clean normal review before completing the overall goal. No unrelated/concurrent dirty or staged changes were found; this batch owns the tracker, five session paths and the startup integration test file.

### B-160 Session-list WebSockets survive relay shutdown

- Status: `resolved`
- Report: Normal review `codex-review.normal-trunk.EFvdO5.log` at `1923ce5` found that account session-list sockets are registered outside the hub and excluded from `HTTPServer.Close`. HTTP shutdown does not close hijacked WebSockets, leaving readers, writers and ping workers alive.
- Repro: Authenticate an owner session-list WebSocket without a session ID, receive the initial list, then call relay shutdown. The peer and handler must terminate without waiting for their transport deadline. Also register multiple account sockets and attempt a late authenticated registration after shutdown.
- Scope: Close the session-list registry and fence late registrations at the existing registry lock. Keep socket I/O outside that lock; preserve credential-revocation isolation and idempotent cleanup. Restart creates a new HTTPServer, as verified in Serve and the PTY harness.
- Fix: Shutdown marks the list registry closed and drains it under its existing mutex, before hub shutdown. Actual socket closes happen outside the mutex. Late handshakes return an error and use the existing handler close path. Repeated shutdown, old unregister callbacks and credential revocation can safely operate on the drained map.
- Failing evidence: The multi-user regression finds both registered sockets still open and accepts a late authenticated handshake; the real WebSocket regression reaches its transport deadline instead of observing a disconnect (`review-session-list-shutdown-before.log`). A concurrent regression finds surviving registrations in all 100 iterations against the original source (`review-session-list-shutdown-concurrent-before.log`). A trap restores both exact fixed source files after the baseline run, verified byte-for-byte. Subsequent cleanup explicitly joins unregister as well as shutdown.
- Passing evidence: All session-list regressions including revocation pass ten race runs (`review-session-list-shutdown-after.log`). The full relay and root SDK race suites pass (`review-session-list-shutdown-relay-race.log`). Final shutdown and concurrent tests pass ten race runs, covering 1,000 concurrent registration/shutdown attempts (`review-session-list-shutdown-final-focused.log`). The real socket sends the Android list-only hello, receives its initial sessions frame, then observes actual closure and handler return before the transport deadline. Transport deadlines only bound a failed test; channel completion and the peer's disconnect prove shutdown, with no sleep or polling used as timing evidence. No inherited terminal is used.
- Code-path review: The registry mutex orders registration, credential revocation and shutdown. A connection admitted before shutdown enters the drain; a later admission sees `closed`. `closeNow` closes the existing done channel and transport, waking the writer, session subscription and read loop; the returning handler cancels its ping and stream contexts. I/O cannot block registry cleanup because it runs after unlocking. Serve and the restart harness each construct a new HTTPServer for the next run. No client UI, input, replay or frame-delivery implementation changes.
- Performance: No relevant shutdown timing/allocation gate exists. The new registration check is constant work under a lock already held only at handshake; shutdown visits each registered socket once. The actual transport and repeated concurrent race regressions are the strongest applicable resource-lifecycle proof. No performance improvement or Android UI latency claim is made; earlier parser, scanner and real-child startup measurements remain recorded in their respective entries.
- Standard checks: Root Go tests, vet, golint and golangci-lint pass serially (`review-session-list-shutdown-{go-test,vet,golint,golangci}.log`, zero lint issues). Formatting, tracker readback and diff checks pass. Only this tracker and three relay files belong to this batch; no unrelated/concurrent edits or staged changes were found. Existing Android unit, instrumentation compilation and targeted device proofs are recorded above; this server-resource fix is proven at its actual list-only WebSocket boundary. No full Android release sweep was run.
- Validation command ledger (root, strictly serialized, detached stdin):
  - Prefix `setsid --wait env GOMAXPROCS=2 GOFLAGS=-p=1`, suffix `</dev/null`, retained root logs for each command below.
  - `go test -p 1 ./internal/relay -run '^TestSessionListShutdown' -count=1 -timeout=30s` failed both before.
  - `go test -p 1 ./internal/relay -run '^TestSessionListRegistrationConcurrentWithShutdownCannotSurvive$' -count=1 -timeout=30s` failed before with both fixed sources temporarily replaced by `git show HEAD:<path>` and restored on exit; `cmp` confirmed restoration.
  - `go test -race -p 1 ./internal/relay -run '^TestSessionList' -count=10 -timeout=90s` passed after.
  - `go test -race -p 1 ./internal/relay . -count=1 -timeout=180s` passed.
  - `go test -race -p 1 ./internal/relay -run '^TestSessionList(Shutdown|RegistrationConcurrent)' -count=10 -timeout=60s` passed final.
  - `go test -p 1 ./...`, `go vet ./...`, `golint ./...`, `golangci-lint run ./...`, Go formatting, tracker readback and `git diff --check` passed.
  - Completed normal review `codex-review.normal-trunk.EFvdO5.log` ran `GOMAXPROCS=2 go test -p 1 ./internal/relay ./internal/sessionmanage ./internal/control ./internal/backoff` successfully, then demonstrated this defect with an anonymous-memory overlay test (`go test -p 1 -overlay <memory-overlay> ./internal/relay -run '^TestReviewShutdownClosesSessionListConnections$' -count=1`, expected failure). The full review log retains the exact overlay source and command. Review scope: attached `androidurl` at `1923ce5` against pinned local `trunk`, prescribed model and reasoning.
- Remaining gap: A clean normal branch review remains required after this commit.

### B-158 Superseded reconnect session refresh mutates a new context

- Status: `resolved`
- Report: `codex-review.normal-trunk.95rhBP.log` found that a reconnect clears its tracked job before awaiting session discovery; endpoint change/logout cannot cancel the running request. The refresh lacks the generation fence already used by bootstrap/management and can accept old sessions or apply old authentication errors to a new account.
- Scope: Fence reconnect success, failure and subsequent reconnect/poll actions with the existing session-context generation and identity. Prevent an obsolete 401 from starting recovery for a new context. Preserve normal retry behavior.
- Fix: Capture the existing session-context generation and identity before recovery starts. Reject obsolete session results before update, recheck after active-session preference resolution, reject obsolete API/network errors before publishing them, and fence the final connect/poll work. The shared discovery helper also checks context before refreshing authentication for a 401 and again before a second session request. Existing current-context recovery and cancellation behavior remains intact; no extra job tracker, public API or persistence change was added.
- Failing unit evidence: Four corrected gated regressions fail before: old sessions return after endpoint change/logout, an old 401 clears a synthetic newer account, and an old network error replaces the newer account's status (`review-reconnect-context-corrected-before-unit.log`; full failure XML copied to `review-reconnect-context-before-unit-results.log`). The first fixture never established a connected socket and failed only its setup assertion (`review-reconnect-context-before-unit.log`); it is excluded from defect evidence. The corrected fixture explicitly selects and opens the old socket, closes it, advances the test scheduler by two seconds to enter the reconnect request, changes context while that request is gated, then releases it. It verifies sessions, selection, login/username/status, no extra auth refresh or socket, and advances another ten mock seconds to reject delayed effects.
- Failing device evidence: The actual app's manual recovery uses the same `refreshSessionsAndReconnect` path. Gate its real owner session response, change endpoint or log out and authenticate the configured second user, then release and join the old recovery job. Both cases fail before: old `host-1`/`host-2` sessions return at the new endpoint, or the new `host-3` account's sessions become empty after the old 401 (`review-reconnect-context-before-connected.log`, two failures, zero skips). This verifies the recovery/UI/authentication boundary; timed backoff itself is driven deterministically by the unit test scheduler.
- Passing evidence: Full Android unit tests and instrumentation compilation pass (`review-reconnect-context-after-unit.log`), including all 83 AppViewModel tests. Four targeted device cases pass, zero failures/errors/skips: the two new recovery regressions and existing bootstrap/management context isolation (`review-reconnect-context-after-connected.log`; XML under `android/test-artifacts/resource-profile-20261004-150825-1411513/android-test-results`). The second account remains connected and receives further live frames after the old failure finishes. No wall-clock sleep is used to prove the race; gates/job joins order the responses.
- Code-path review: Generation and endpoint/username/share identity checks run on the view-model dispatcher before any obsolete result mutates state or invokes current-account unauthorized handling. The post-update check covers preference suspension already fenced inside active-session selection. The helper cannot start a current-account auth refresh from an obsolete 401 or issue a second session request after context changes during auth recovery. Current responses retain their normal update, poll, status and connect behavior. Input, frame decoding and lease transfer are untouched.
- Performance: No relevant recovery-generation timing/allocation gate exists. Checks add constant work only around session-discovery suspensions and results, and the regression proves no obsolete-result refresh/extra socket. No input/frame hot path or ordinary request was added; no Android latency claim is made. Standard checks and command ledger follow B-159.

### B-159 Detached creation drops an authenticated host's offline startup default

- Status: `resolved`
- Report: The same review found that credential forwarding and `--offline` are mutually exclusive in detached creation, even though configured publishing remains enabled for an offline host so it can toggle online later.
- Scope: Forward the configured offline startup default independently of credential forwarding, matching in-process creation. Prove local creation through actual Ctrl+L c starts offline and can later publish through Ctrl+L o with inherited credentials.
- Fix: Append `--offline` when the configured startup default is offline or publishing is unavailable, independently of forwarding endpoint/auth-file/private-pipe credentials. Configured online startup remains online. This matches in-process `createLocalSession`, which uses `r.opts.Offline` as the new terminal's default, rather than the parent's current toggle state.
- Failing evidence: `TestSessionManagementOfflineCredentialsStayOfflineUntilExplicitToggle` launches a real authenticated `-x --offline` parent, attaches through an owned PTY, sends Ctrl+L c, and observes the child lose its offline record flag and publish to the relay before any toggle (`review-offline-default-before-pty.log`). All interactive stdio is contained in owned PTYs; the harness uses an injected mock clock for observations.
- Passing evidence: The same test passes three race runs (`review-offline-default-after-pty.log`). It asserts both records are initially offline and absent from the relay, then sends Ctrl+L o and proves only the child becomes online and joins the authenticated relay. The broader real local CLI, relay/headful creation and auth-file/private-pipe credential-isolation integration gate passes (`review-offline-default-creation-final.log`). No socket, name, metadata or credential-transport format changed.
- Performance: Existing five-alternating-launch startup gate initially measured routed/direct 33.245/28.873 ms before and 36.590/29.414 ms after. To resolve that variable pair, collect three further alternating baseline/fixed invocations: routed 41.855, 35.171, 36.559 ms before versus 37.790, 31.044, 28.862 ms fixed; direct 28.024, 27.336, 31.119 ms before versus 28.926, 28.349, 20.778 ms fixed. Median-of-medians routed 36.559 → 31.044 ms, direct 28.024 → 28.349 ms, with no observed stable startup regression and no speedup claim under host variation. Each invocation starts five real direct and five real routed children. A trap restores the exact fixed source after baseline switching, followed by a byte-for-byte comparison.

#### Shared verification for B-158–B-159

- Required root `go test -p 1 ./...`, `go vet ./...`, `golint ./...` and `golangci-lint run ./...` pass strictly serially after source restoration (`review-reconnect-offline-go-test.log`, `review-reconnect-offline-vet.log`, `review-reconnect-offline-golint.log`, `review-reconnect-offline-golangci.log`, zero lint issues). Every running session was joined before the next operational command. Capped Android runners cleaned up owned emulator/adb/harness resources; no full release sweep ran. Only the six documented task paths are dirty; no unrelated/concurrent changes were found.
- Validation command ledger:
  - From `android`, `setsid --wait env JAVA_HOME=/usr/lib/jvm/java-21-openjdk-amd64 ./gradlew --no-daemon --max-workers=2 -Dorg.gradle.parallel=false -Dkotlin.compiler.execution.strategy=in-process '-Dorg.gradle.jvmargs=-Xmx1536m -Dfile.encoding=UTF-8' :app:testDebugUnitTest --tests '*AppViewModelTest.reconnectSessionFetchIsIgnored*' --tests '*AppViewModelTest.staleReconnect*' </dev/null` failed the initial setup fixture and then all four corrected regressions. The same options with `:app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed after.
  - From root, `setsid --wait env JAVA_HOME=/usr/lib/jvm/java-21-openjdk-amd64 LINGON_IT_CPU_QUOTA_PERCENT=200 LINGON_IT_MEMORY_MAX=7G LINGON_IT_ONLY=reconnect_session_fetch_does_not_restore_old_endpoint_sessions,stale_reconnect_authentication_failure_preserves_new_account_frames ./android/scripts/run-integration-tests.sh </dev/null` failed both before. It passed after with those two cases plus `management_session_fetch_does_not_restore_old_endpoint_sessions,bootstrap_session_fetch_does_not_restore_old_endpoint_sessions` selected. Both runs compiled instrumentation and used real harness accounts/sockets with gated response ordering.
  - Go commands below used `setsid --wait env GOMAXPROCS=2 GOFLAGS=-p=1`, `/dev/null` stdin and retained root logs. `go test -p 1 -tags integration ./integration/pty/attach -run '^TestSessionManagementOfflineCredentialsStayOfflineUntilExplicitToggle$' -count=1 -timeout=90s` failed before; the same test with `-race -count=3 -timeout=120s` passed after.
  - `go test -race -p 1 -tags integration ./integration/pty/attach -run '^TestSessionManagement(LocalCLI|RelayAndHeadfulCLI|ChildCredentialsStayOutOfProcessMetadata|OfflineCredentialsStayOfflineUntilExplicitToggle)$' -count=1 -timeout=180s` passed.
  - `go test -p 1 -tags integration ./integration/pty/attach -run '^TestSessionManagementStartupPerformance$' -count=1 -v -timeout=90s` passed before/after and six alternating baseline/fixed invocations (`review-offline-default-startup-{baseline,fixed}-{1,2,3}.log`).
  - `go test -p 1 ./...`, `go vet ./...`, `golint ./...`, `golangci-lint run ./...`, Go formatting, documentation readback, `git diff --check` and byte-for-byte source restoration checks passed after final implementation.
  - Completed review `codex-review.normal-trunk.95rhBP.log` ran `GOMAXPROCS=2 go test -p 1 ./internal/relay ./internal/sessionmanage ./internal/backoff ./internal/terminal/emu`, `GOMAXPROCS=2 go test -p 1 -count=1 ./internal/headless ./internal/sessionmanage ./internal/control ./internal/relay`, the Android unit/compile Gradle command above and `git diff --check 87515484b07eaf19586fed6579019209961cd715`; all passed. Those checks did not cover the new findings until the regressions above were added. Scope was normal `androidurl` at `5803b31` against pinned local `trunk`, with prescribed model/reasoning configuration.
- Remaining gap: None for these findings. A final clean normal branch review remains required after committing this batch.

### B-157 Rename dialog remains visible above the app lock screen

- Status: `resolved`
- Report: `codex-review.normal-trunk.zgqkXX.log` found that opening Rename session and returning after the app-lock timeout can leave the dialog above `LockedScreen` after unlock cancellation, exposing the session name and draft.
- Scope: Gate dialog rendering on the unlocked state and discard the local rename target/draft when locking. Preserve explicit reopening after unlock and normal rename behavior.
- Fix: Key the local rename target to `requiresAppUnlock`, resetting it synchronously when lock state changes. Also require the unlocked state before composing the dialog. Disposing the dialog discards its remembered draft; unlocking does not reopen it. Explicit reopening retains normal current-name initialization. No new button, request or view-model state was added.
- Failing regression: `rename_dialog_lock_hides_and_discards_private_draft` authenticates against the real harness, opens the actual settings/dialog UI, verifies the current name and enters a private draft. It then explicitly supplies the locked/cancelled state. Before the fix, the device still has one `rename_session_input` node where zero is required (`review-rename-lock-before-connected.log`). The test does not sleep through a timeout or change device credentials.
- Passing evidence: The same device test now asserts zero rename input/save/draft nodes while locked, the visible App locked screen, no automatic dialog return after unlocking, and the current original name on explicit reopen. It passes alongside `settings_create_terminal_and_rename_session` and `app_lock_unlock_preserves_terminal_camera_viewport`: three tests, zero failures/errors/skips (`review-rename-lock-after-connected.log`; XML retained under `android/test-artifacts/resource-profile-20261004-144420-1395157/android-test-results`). This verifies the actual Compose UI at the lock/cancellation state boundary; it does not claim a new test of Android's system credential prompt or wall-clock timeout scheduling. Those mechanisms are unchanged.
- Code-path review: The keyed remembered state is reset within composition, rather than waiting for a launched cleanup effect. The explicit gate prevents rendering under the locked state, and removal disposes `RenameSessionDialog`'s own remembered text. Settings and normal save/dismiss behavior remain intact. No terminal frame/input, transport, auth, persistence or lifecycle-timer implementation changed.
- Performance: No relevant rename-dialog or lock-transition timing/allocation gate exists. The change adds a boolean remember key and render guard only at composition; no terminal parser/input/frame or network work was added. Targeted correctness gates are the evidence for this change, with no Android latency or speedup claim. Earlier measured startup/parser gates remain recorded above.
- Validation commands (strictly serialized):
  - From root, `setsid --wait env JAVA_HOME=/usr/lib/jvm/java-21-openjdk-amd64 LINGON_IT_CPU_QUOTA_PERCENT=200 LINGON_IT_MEMORY_MAX=7G LINGON_IT_ONLY=rename_dialog_lock_hides_and_discards_private_draft ./android/scripts/run-integration-tests.sh </dev/null` failed before at the locked-dialog assertion. The same command with `LINGON_IT_ONLY=rename_dialog_lock_hides_and_discards_private_draft,settings_create_terminal_and_rename_session,app_lock_unlock_preserves_terminal_camera_viewport` passed after. Both runs used the isolated capped runner and cleaned up their owned emulator, adb server and harness. No full release sweep ran.
  - From `android`, `setsid --wait env JAVA_HOME=/usr/lib/jvm/java-21-openjdk-amd64 ./gradlew --no-daemon --max-workers=2 -Dorg.gradle.parallel=false -Dkotlin.compiler.execution.strategy=in-process '-Dorg.gradle.jvmargs=-Xmx1536m -Dfile.encoding=UTF-8' :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin </dev/null` passed (`review-rename-lock-after-unit.log`).
  - From root, `setsid --wait env GOMAXPROCS=2 GOFLAGS=-p=1 go test -p 1 ./... </dev/null`, then the same wrapper with `go vet ./...`, `golint ./...` and `golangci-lint run ./...`, passed (`review-rename-lock-go-test.log`, `review-rename-lock-vet.log`, `review-rename-lock-golint.log`, `review-rename-lock-golangci.log`, zero lint issues). Documentation readback and `git diff --check` passed.
  - The completed review also passed `GOMAXPROCS=2 go test -p 1 ./internal/sessionmanage ./internal/relay ./internal/publisher ./internal/headless ./internal/control ./internal/backoff` and `git diff --check 87515484b07eaf19586fed6579019209961cd715` (`codex-review.normal-trunk.zgqkXX.log`). Review scope: normal `androidurl` at `62c3473` against pinned local `trunk`, prescribed model and reasoning configuration. No reviewer Android verification was claimed.
- Remaining gap: None for the UI exposure. A clean normal branch review remains required after this commit.

### B-154 Offline keyboard regression lacks deterministic command and snapshot setup

- Status: `resolved`
- Report: The full Go suite failed `TestMultiAttachRespondsToKeysWhileOffline` while checking session B; help still showed session A (`review-verification-go-test.log`). The fixture sends Ctrl+L h on every observation retry and advances fixed delays before sending the tab command.
- Scope: Emit each help/dismiss/tab command once and wait for observable help dismissal. Prime both tabs online with distinct snapshots before testing cached offline rendering. Preserve existing assertions and add an explicit cached-B assertion. Initial repeated runs exposed the production defects tracked below, rather than establishing a fixture-only explanation.
- Fix: Send each command once, wait for help dismissal, and visit both tabs online with distinct output before stopping the relay. Assert B's own cached output after switching offline, in addition to the existing help identity and cached-A checks. No existing permitted fallback or assertion was removed.
- Verification: The initial command-only revision still failed seven of ten runs (`review-verification-offline-keys-after.log`); it is not accepted as a fix. Priming both tabs made the production failures below reproducible (`review-verification-offline-keys-final.log`). The final fixture and production fixes pass ten race runs, all existing no-flicker gates and the full Go suite. Shared command ledger follows B-152.

### B-155 Offline tab reconnect overwrites active help identity and reuses a hidden framebuffer baseline

- Status: `resolved`
- Report: The primed, single-command owned-PTY regression fails with help identifying session B while A is active, or A's cached content remaining after switching to B (`review-verification-offline-keys-final.log`, ten repeated race runs). Detached hidden clients initialize the shared compositor's session identity, and an offline tab reconnect seeds its previously hidden delta baseline while the physical display still contains the other tab.
- Scope: Shared compositor context belongs to its owner; only client-owned runtimes initialize client context. Invalidate the cached delta baseline before revealing a different tab, including the reconnect branch, without a screen clear or phased tab overlay repaint.
- Fix: Initialize client context only when creating a client-owned compositor; `SetCompositor` preserves a supplied owner's context, and detached startup no longer reapplies its own identity. Both shared-runtime adapters (`MultiClient` and host `remoteManager`) already supply their owner context. Preserve the standalone local endpoint label. Before revealing a different cached view, invalidate its delta baseline before the reconnect can seed it. Compose the next frame without a terminal clear. Clarify the existing `RenderCurrentFull` comment to match its no-clear behavior.
- Failing coverage: `TestClientSetCompositorPreservesOwnerContext` fails against the original code (`review-verification-shared-context-before.log`). The real primed offline PTY regression fails with the wrong help identity and stale/partially overlaid cached content (`review-verification-offline-keys-final.log`). The test drives actual Ctrl+L h/n/p and q in owned PTYs using the injected mock clock.
- Verification: The context unit tests and offline PTY regression pass ten race runs (`review-verification-offline-production-after.log`). Permanent host/attach no-flicker and 20-cycle shutdown checks pass unchanged (`review-verification-offline-lifecycle-final.log`). Startup and standard checks pass; see shared ledger below. No Android source, relay contract or storage format changed in this batch.

### B-156 Concurrent attach reconnect reads refreshed credentials without synchronization

- Status: `resolved`
- Report: The same owned-PTY race run reports `connectView` reading `MultiClient.AccessToken` concurrently with another reconnect's refresh callback write (`review-verification-offline-keys-final.log`).
- Scope: All runtime token reads use the existing refresh mutex; let the refresh method publish the result under that mutex instead of duplicating writes in its default callback. No extra refresh request or protocol change.
- Fix: Read runtime access tokens through the existing refresh mutex in view construction, session-list requests and inactivity management. The default refresher callback no longer duplicates publication; `refreshToken` publishes under the same mutex. Startup-only assignments occur before concurrent views exist. Input/frame processing gains no lock or extra request.
- Verification: The failing owned-PTY race report names the callback write and concurrent view-construction read. The same scenario passes ten race runs after, alongside the unchanged lifecycle/no-flicker gates and full Go checks. No credential or protocol behavior was changed.

### B-153 Remote session-list empty grace reads state without its lock

- Status: `resolved`
- Report: The unchanged `TestAttachNoFullRedrawOnOnlineWrapAroundTabSwitch` failed under the race detector during B-151/B-152 verification (`review-verification-lifecycle-race.log`). `applySessions` reads `sessions` and `lastNonEmpty` before taking the lock while refresh and publisher callbacks update them concurrently.
- Scope: Make the empty-update guard and state transition atomic under the existing state mutex; preserve mock-clock grace semantics, callback boundaries and permanent no-flicker tests.
- Fix: Acquire the existing state mutex before checking the empty-update grace and capture one clock value for the atomic transition. Release before early-return logging. Callbacks and network/reconnect work retain their existing boundaries outside the lock; no second lock acquisition was added.
- Failing coverage: `TestRemoteManagerConcurrentUpdatesPreserveEmptyGrace` races on both `sessions` and `lastNonEmpty` against the original code (`review-verification-remote-corrected-before.log`). Two synchronized workers issue nonempty/empty updates, then assert one live session survives four mock seconds and disappears at five. This pure state test creates no terminal. The first attempted fixture used a nonexistent `Advance` method; its compilation failure (`review-verification-remote-before.log`) is not accepted as defect evidence.
- Verification: Five race runs pass (`review-verification-remote-after.log`). The unchanged no-flicker gate that originally exposed the race and the complete lifecycle gate pass before and after the later attach fixes (`review-verification-lifecycle-final.log`, `review-verification-offline-lifecycle-final.log`). Startup measurement and standard checks pass; see ledger below.

### B-151 Stale-PID detach regression accepts a killed zombie

- Status: `resolved`
- Report: `codex-review.normal-trunk.XeaSRJ.log` found that the stale-PID test does not reap its child until cleanup, so `kill(pid, 0)` still reports a killed zombie as alive.
- Scope: Reap the test-owned child asynchronously and prove it can echo a nonce after detach through owned pipes. Restrict stale start-time protection to Linux, matching existing production policy.
- Fix: Replace PID-existence inference with a test-owned nonterminal echo child, asynchronously reaped by `cmd.Wait`. Send unique input through owned pipes and assert its exact reply before and after detach, then verify it has not exited and the stale store entry is gone. No inherited stdio is used; cleanup joins the waiter and signals only the test-owned child. Skip this Linux start-time identity assertion on other platforms, preserving their existing PID fallback policy.
- Failing coverage: Temporarily removing only the `RecordedProcessMayMatch` guard makes the new test fail on the post-detach pipe write (`review-verification-pid-before.log`). A trap restores the exact production source; `internal/headless/control.go` has no final diff.
- Verification: Five race runs pass with the identity guard (`review-verification-pid-after.log`), the broader reconcile/detach/shutdown unit race gate passes, and Darwin headless test compilation succeeds. Darwin runtime behavior was not claimed or changed.

### B-152 PTY shutdown regression lacks an independent owned terminal boundary

- Status: `resolved`
- Report: `codex-review.normal-trunk.XeaSRJ.log` found direct `newLocalSession`/`runOnce` terminal regression usage prohibited by the repository isolation rule.
- Scope: Move the shutdown scenario into the existing owned-PTY harness, preserve 20 cancellation cycles, and prove real input/output and clean shutdown using its injected mock clock. Keep the existing platform shutdown-order unit tests and all permanent no-flicker tests unchanged.
- Fix: Move the scenario to `integration/pty/session/pty_reader_shutdown_integration_test.go` (`integration && linux`) and run 20 serial host starts/cancels through `ptytest.StartHost`. Before cancellation, prove READY output and a unique ACK response to actual input. Require clean exit using the injected mock clock. The harness opens its own master/slave before Runner construction, routes all stdio to that owned PTY and disables inherited signal resizing. The original prohibited direct-constructor test is retired; platform shutdown-order unit tests and permanent no-flicker tests are untouched.
- Verification: The prohibited baseline was not executed again; its source is the isolation-policy failure evidence. Earlier bare-test passes are not accepted as terminal-isolation proof. The replacement passes the race detector's 20 cancellation cycles (`review-verification-pty-final.log`) and twice in the broader lifecycle gate. Source audit confirms no direct constructor/runOnce, inherited stdio, `/dev/tty`, SIGWINCH or wall-clock sleep/poll helper in the replacement. An initial READY-on-row-zero fixture was hidden by the tab overlay; that fixture failure (`review-verification-pty-after.log`) is excluded from production-defect evidence.

#### Shared verification for B-151–B-156

- Standard checks: Full `go test -p 1 ./...`, `go vet ./...`, `golint ./...` and `golangci-lint run ./...` pass strictly serially, with zero lint issues (`review-verification-go-test-final.log`, `review-verification-vet-final.log`, `review-verification-golint-final.log`, `review-verification-golangci-final.log`). The earlier full-suite failure is retained in `review-verification-go-test.log`, not treated as a pass. All operational commands used detached stdin, no inherited terminal operations and `GOMAXPROCS=2 GOFLAGS=-p=1`. No Android code changed, so the previously recorded targeted Android evidence remains applicable; no emulator or full release sweep was repeated.
- Performance: The existing `TestSessionManagementStartupPerformance` measures five alternating real direct/routed child launches per invocation. B-153's pair measured routed/direct 39.023/27.496 ms before and 30.499/23.477 ms after. A later attach-fix pair rose together from 30.487/28.331 to 35.611/31.916 ms, so three additional alternating baseline/fixed invocations were collected. Routed medians were 46.013, 41.053, 48.145 ms before versus 35.622, 36.880, 39.383 ms fixed; direct medians were 33.374, 27.188, 32.933 ms before versus 31.038, 32.586, 33.261 ms fixed. Median-of-medians routed 46.013 → 36.880 ms, direct 32.933 → 32.586 ms: no observed startup regression, but host variation does not support a stable speedup claim. Production source was restored exactly and compared byte-for-byte after baseline switching. This startup gate does not establish per-keystroke latency; no input/frame hot-path work was added. No relevant token-lock or session-list-update microbenchmark exists. An initial wrong-package invocation ran no tests (`review-verification-startup-before.log`), so it is excluded from performance evidence.
- Validation command ledger (each Go invocation wrapped by `setsid --wait env GOMAXPROCS=2 GOFLAGS=-p=1`, stdin `/dev/null`, output retained in the named root log):
  - `go test -race -p 1 ./internal/headless -run '^TestDetachWithUnreachableSocketDoesNotKillPIDOnlyRecord$' -count=1 -timeout=60s` failed with the PID-only mutation; `-count=5` passed with restored production code.
  - `go test -race -p 1 ./internal/headless ./internal/session -run 'Test(Reconcile|Detach|ShutdownPTYReader)' -count=1 -timeout=120s` passed (`review-verification-unit-race.log`).
  - `GOOS=darwin GOARCH=amd64 go test -c -o review-verification-headless-darwin-binary.log ./internal/headless` passed compilation (`review-verification-darwin-build.log`).
  - `go test -race -p 1 -tags integration ./integration/pty/session -run '^TestLocalSessionCancelWaitsForPTYReaderBeforeClose$' -count=1 -timeout=120s` passed the corrected owned-PTY fixture; the first row-zero fixture failed.
  - `go test -race -p 1 ./internal/session -run '^TestRemoteManagerConcurrentUpdatesPreserveEmptyGrace$' -count=1 -timeout=60s` failed against the original guard; `-count=5` passed after. The initial compilation-only fixture error is excluded above.
  - `go test -race -p 1 -tags integration ./integration/pty/session ./integration/pty/attach -run 'TestLocalSessionCancelWaitsForPTYReaderBeforeClose$|TestHost.*(WrapAroundTabSwitch|RepaintBaseTopRowBeforeOverlay)|TestAttachNoFullRedraw.*TabSwitch' -count=1 -timeout=180s` initially exposed B-153, then passed twice after (`review-verification-lifecycle-final.log`, `review-verification-offline-lifecycle-final.log`).
  - `go test -race -p 1 ./internal/attach -run '^TestMultiAttachRespondsToKeysWhileOffline$' -count=10 -timeout=120s` failed both initial fixture revisions, exposing the further defects above.
  - `go test -p 1 ./internal/attach -run '^TestClientSetCompositorPreservesOwnerContext$' -count=1` failed before; `go test -race -p 1 ./internal/attach -run '^Test(MultiAttachRespondsToKeysWhileOffline|Client.*Compositor.*)$' -count=10 -timeout=120s` passed after all attach fixes.
  - `go test -p 1 -tags integration ./integration/pty/attach -run '^TestSessionManagementStartupPerformance$' -count=1 -v -timeout=90s` passed the two B-153 measurements, the two later attach measurements, and six alternating baseline/fixed invocations (`review-verification-startup-{baseline,fixed}-{1,2,3}.log`). The wrong-package attempt is excluded above.
  - `go test -p 1 ./...` failed initially, passed finally; `go vet ./...`, `golint ./...`, `golangci-lint run ./...` passed after final source restoration.
  - `gofmt` on owned Go paths, documentation readback, `git diff --check`, owned-terminal source audit and byte-for-byte baseline-source restoration checks passed. The removed direct-constructor test was never used as new evidence.
- Remaining gap: None for these findings. A final clean normal branch review remains required after committing this batch.

### B-150 Malformed shared text causes excessive overlapping token decoding

- Status: `resolved`
- Report: `codex-review.normal-trunk.Usi5an.log` measured about 8.8 seconds scanning `"LGE".repeat(1000)` (3,000 characters) on the JVM. Each failed candidate decodes all shortened prefixes, and discovery repeats this at every later prefix. The shared-text receiver runs this work on the UI thread.
- Scope: Remove repeated truncation/decoding, use decoded version and embedded payload length to identify one candidate boundary, and preserve valid wrapped tokens, later-prefix discovery and format limits.
- Fix: Search only token prefixes without greedily copying their bodies. Scan a bounded candidate once to decode its version and, for embedded tokens, the 23-byte header and declared endpoint byte length. Use the resulting encoded payload length to choose one boundary, then run the existing full format/CRC validator once. Invalid versions and endpoint lengths stop early. No shortened-prefix decoding loop remains.
- Failing regression gate: The probe body now retained in `android/app/src/test/resources/ShareScanProbe.java` failed its two-second diagnostic bound on all three pre-fix runs (`review-share-scan-before-performance.log`; the baseline used the identical probe body in `review-share-scan-probe.log` before recompiling production classes). The 3 KB case took 5,650.374, 4,369.433 and 4,330.540 ms. Three comparable fixed runs passed at 3.271, 1.873 and 1.402 ms (`review-share-scan-after-performance.log`). Median 4,369.433 → 1.873 ms, for this reproduction only. The probe uses Java 21, two active processors, 256 MB heap, the same input sizes and fresh JVMs; compilation precedes the measured method call. Its wall-clock diagnostic bound measures actual CPU work, separately from virtual-time lifecycle/retry tests, and is not a universal phone latency guarantee.
- Functional coverage: Unit tests cover 1,000 malformed prefixes both alone and before a valid token, valid embedded payloads across encoded-length rounding boundaries and the maximum endpoint length, UTF-8 byte lengths, lowercase/wrapping, truncation, CRC corruption, invalid decoded version and zero endpoint length followed by a valid bare token. Existing invalid/oversize/later-candidate coverage remains intact. Full Android unit tests and instrumentation compilation passed initially and after the final UTF-8 discovery assertion (`review-share-scan-after-unit.log`, `review-share-scan-final-unit.log`).
- Device verification: The actual shared-text receiver opens a wrapped real relay token after 1,000 malformed prefixes and displays live host ticks. The existing single-invalid-candidate intent and token-isolation cases also pass: three tests, zero failures/skips (`review-share-scan-after-connected.log`). The run was serialized at two CPUs/7 GB and cleaned up owned emulator/adb/harness resources. No full release sweep ran.
- Code-path review: The header length is read in bytes from the existing encoding, not endpoint characters. Candidate scanning is bounded by the existing maximum; the version and 1–2,048-byte endpoint restrictions apply before a payload boundary is accepted. The existing validator still checks version, decoded payload size, CRC and endpoint decoding. Discovery continues at later prefixes after a malformed candidate. The public parser, token format and receiver handoff are unchanged. No cache or asynchronous UI workaround was added.
- Validation commands (serialized; Java 21 for Android, `GOMAXPROCS=2 GOFLAGS=-p=1` for Go):
  - From `android`, `./gradlew --no-daemon --max-workers=2 -Dorg.gradle.parallel=false -Dkotlin.compiler.execution.strategy=in-process '-Dorg.gradle.jvmargs=-Xmx1536m -Dfile.encoding=UTF-8' :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed twice, including the final added UTF-8 assertion.
  - From root, `/usr/lib/jvm/java-21-openjdk-amd64/bin/java -XX:ActiveProcessorCount=2 -Xmx256m --source 21 --class-path android/app/build/tmp/kotlin-classes/debug:/home/mike/.gradle/caches/modules-2/files-2.1/org.jetbrains.kotlin/kotlin-stdlib/2.0.21/618b539767b4899b4660a83006e052b63f1db551/kotlin-stdlib-2.0.21.jar android/app/src/test/resources/ShareScanProbe.java` passed three times, detached with `setsid --wait` and `/dev/null` input. The three failing baseline commands used `review-share-scan-probe.log` as the identical source body. Review's original jshell probe is preserved in `codex-review.normal-trunk.Usi5an.log`.
  - `setsid --wait ./android/scripts/run-integration-tests.sh </dev/null` with `LINGON_IT_CPU_QUOTA_PERCENT=200 LINGON_IT_MEMORY_MAX=7G LINGON_IT_ONLY=shared_text_with_many_invalid_prefixes_opens_wrapped_token,shared_text_finds_wrapped_valid_token_after_invalid_candidate,share_tokens_are_isolated` passed.
  - `setsid --wait go test -p 1 ./... </dev/null`, `go vet ./...`, `golint ./...` and `golangci-lint run ./...` passed strictly serially (`review-share-scan-go-test-final.log`, `review-share-scan-vet-final.log`, `review-share-scan-golint-final.log`, `review-share-scan-golangci-final.log`, zero lint issues). An earlier Go test returned a running session while lint continued; it completed successfully, but the earlier logs are not accepted as serialized gate evidence, so all four checks were repeated sequentially. Documentation readback and `git diff --check` passed.
- Remaining gap: None for the reported scanning failure. The focused timing result does not establish general Android latency. A final clean branch review is still required after this commit.

### B-148 Superseded share authentication can invalidate a newer connection

- Status: `resolved`
- Report: `codex-review.normal-trunk.q6nqlb.log` found that foreground recovery can start another authentication for the same endpoint/token. An obsolete failure can then clear the healthy websocket or its permissions because identity checks do not distinguish attempts.
- Scope: Only the latest share authentication may publish success or failure. Closing/switching the connection must invalidate pending attempts. Preserve current transient retry and authorization behavior.
- Fix: Increment a private authentication-attempt generation before launching each share request and whenever the websocket is closed/switched. Check the captured generation before accepting either success or failure, alongside the existing endpoint/token identity checks. An obsolete attempt cannot publish metadata, clear permissions, set an error or schedule reconnect.
- Failing baseline: Gated unit regressions for obsolete transient failure, permanent denial and success all failed before the generation fence (`review-share-attempt-before-unit.log`). The two failures observed a healthy connection become disconnected; the obsolete success overwrote the newer control permission. The actual app regression also observed `Connected` become `Disconnected` after releasing the old failure (`review-share-attempt-before-connected.log`).
- Verification: Unit gates establish the newer socket first, release and join the older attempt, verify current metadata/permission/connection epoch, deliver a new snapshot through that socket, and advance the test scheduler by 10 seconds to prove no retry or extra socket appears. The device test gates the first request, uses real relay authentication and websocket for the foreground attempt, joins the obsolete result, and observes further live frames with the same connection epoch and one websocket. It passes after; retries, isolation, takeover and view-only restrictions also pass. Commands and evidence are below.
- Code-path review: All completion handling resumes on the view-model dispatcher; the private generation is issued there before IO starts. Both completion branches reject obsolete generations before state or retry changes. Close/switch invalidation also covers endpoint/token changes, manual recovery and clearing the view model. A current transient failure retains its normal backoff; no reconnect/control request was added to ordinary typing. No public API, storage or wire change.

### B-149 Token discovery skips valid tokens after invalid candidates

- Status: `resolved`
- Report: `codex-review.normal-trunk.q6nqlb.log` reproduced `findInText("LGBinvalid\n" + validToken)` returning null because the greedy whitespace match consumes both prefixes.
- Scope: Continue searching subsequent candidate prefixes after a failed candidate; preserve wrapped bare/embedded tokens and existing candidate-size limits.
- Fix: Resume searching after the failed candidate's prefix, allowing later prefixes inside its whitespace-delimited match to be examined. Bound regex capture by the existing maximum candidate length and compile the pattern once. Keep existing CRC, version, endpoint-byte limits and wrapped-token parsing.
- Failing baseline: The new parser test failed to find a wrapped valid token after `LGBinvalid` (`review-share-attempt-before-unit.log`). The actual `ACTION_SEND` regression remained idle with no selected token/session because the shared-text receiver did not discover the later wrapped embedded token (`review-share-attempt-before-connected.log`).
- Verification: The parser test now finds wrapped bare and embedded tokens after single/multiple invalid candidates and an oversized 7,000-character candidate; the existing wrapped-token, UTF-8 endpoint and oversize rejection tests pass. The device regression launches `ShareReceiverActivity` with real shared text, then observes the exact token/session and live host ticks. It passes after. Commands and evidence are below.
- Code-path review: Search always advances beyond the failed three-character prefix, while bounded capture limits each candidate independently. It can reach valid prefixes beyond the earlier capture limit. Parsed CRC/version validation and normalization still precede the receiver's private pending-launch handoff to the main activity. No intent/protocol/persistence format was changed.

#### Shared verification for B-148–B-149

- Full Android unit tests and instrumentation compilation passed (`review-share-attempt-after-unit.log`). Six targeted device tests passed, zero failures/skips (`review-share-attempt-after-connected.log`): obsolete share-auth result, shared-text discovery, two transient retries, token isolation, control-share typing takeover, and view-only protection. Device runs were serialized at two CPUs/7 GB and cleaned up their owned emulator/adb/harness resources. The full release sweep was not run.
- Performance: No relevant authentication-generation or shared-text scanning timing/allocation gate exists. The generation fence performs constant work only during connection attempts/completions and close/switch; frame and input handling are unchanged. The parser reuses one compiled pattern and bounds each capture using the existing candidate limit. Correctness gates prove no obsolete-result reconnect/extra socket and preserved first-input takeover without a reconnect/control-request round trip. No measured latency claim is made; earlier startup/parser benchmark evidence remains recorded.
- Validation commands (serialized; Java 21 for Android, `GOMAXPROCS=2 GOFLAGS=-p=1` for Go):
  - From `android`, `./gradlew --no-daemon --max-workers=2 -Dorg.gradle.parallel=false -Dkotlin.compiler.execution.strategy=in-process '-Dorg.gradle.jvmargs=-Xmx1536m -Dfile.encoding=UTF-8' :app:testDebugUnitTest --tests '*ShareTokensTest' --tests '*AppViewModelTest.obsolete*ShareAuthentication*'` failed four new regressions before. The same options with `:app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed after.
  - From root, `setsid --wait ./android/scripts/run-integration-tests.sh </dev/null` with `LINGON_IT_CPU_QUOTA_PERCENT=200 LINGON_IT_MEMORY_MAX=7G LINGON_IT_ONLY=obsolete_share_authentication_failure_preserves_new_connection_frames,shared_text_finds_wrapped_valid_token_after_invalid_candidate` failed both baselines. The final command passed with `LINGON_IT_ONLY=obsolete_share_authentication_failure_preserves_new_connection_frames,shared_text_finds_wrapped_valid_token_after_invalid_candidate,share_authentication_recovers_after_two_transient_failures,share_tokens_are_isolated,control_share_typing_reclaims_control_after_real_handoff,resize_setting_disabled_for_view_only_share_token`.
  - `setsid --wait go test -p 1 ./... </dev/null` passed (`review-share-attempt-go-test.log`); `go vet ./...`, `golint ./...` and `golangci-lint run ./...` passed (`review-share-attempt-vet.log`, `review-share-attempt-golint.log`, `review-share-attempt-golangci.log`, zero lint issues). Documentation readback and `git diff --check` passed.
- Remaining gap: None for the reported share-flow defects. A final clean branch review is still required after this commit.

### B-147 Android selection inserts spaces after single-code-point wide glyphs

- Status: `resolved`
- Report: `codex-review.normal-trunk.3Bmcfs.log` found that selection requires a nonempty grapheme before recognizing a continuation, but the host encodes ordinary CJK and single-code-point emoji as a rune plus a zero-rune continuation with an empty grapheme.
- Repro: Select and copy `日本 🙂 ok` using host-shaped cells; copied text must retain the original spaces without introducing spaces inside `日本`. Starting a touch selection on a wide continuation must select the owning glyph, as it does for a stored grapheme.
- Scope: Recognize the host's existing continuation encoding consistently in selection extraction and touch anchoring. Preserve real blank cells, concealed text and row boundaries; no protocol or Unicode-width table change.
- Fix: A shared `TerminalSnapshot.isWideContinuation` recognizes a zero-rune, empty-grapheme cell with matching attributes after a populated cell. Selection extraction and touch anchoring both use it. Ordinary spaces remain explicit space runes; a row-start zero or mismatched attributes is not treated as a continuation. Concealed cells still follow the existing redaction path before extraction.
- Failing baseline: `singleCodePointWideGlyphsUseHostContinuationEncoding` failed against the original extraction (`review-selection-before-unit.log`). The device regression `wideRuneSelectionCopiesOriginalTextAndAnchorsContinuationTouches` independently reproduced `日 本  🙂  ok` instead of `日本 🙂 ok` (`review-selection-before-connected.log`). With corrected extraction but the original touch handler temporarily restored, the same regression reached its next assertion and reproduced omission of `本` when selecting from its continuation (`review-selection-touch-before-connected.log`). A shell cleanup trap restored the fixed handler after that isolated run.
- Regression coverage: Unit tests cover forward/reverse ranges, CJK and a single-code-point emoji with both null and empty grapheme arrays, leading-cell-only copying, actual spaces, combining/ZWJ clusters, row boundaries, mismatched attributes and concealment. The device test drives actual drags, verifies the selected copy value, then selects forward and backward from continuation cells. All five tests in the terminal-view instrumentation class passed, including existing selection highlight/pan and link tests (`review-selection-after-connected.log`). Full Android unit tests and instrumentation compilation passed (`review-selection-after-unit.log`).
- Code-path review: Host `renderGrapheme` clears stored graphemes for single-code-point glyphs, and `setCell` writes matching-attribute zero continuations for width two. The helper recognizes that existing encoding without introducing a Unicode width table or changing the wire format. Touch coordinates snap once to the owning cell; extraction skips its continuation once. Hidden-cell handling precedes the helper, and real spaces and newlines remain intact. No rendering or permanent no-flicker test was altered.
- Performance: No relevant selection/touch timing or allocation gate exists. The shared predicate performs bounded cell/attribute comparisons, does not allocate, and removes the per-touch `CellAttr` construction in this path. It adds no snapshot scan or per-frame work. The full unit and device regressions are the applicable correctness gates; no measured latency improvement is claimed.
- Validation commands (serialized; Java 21 for Android, `GOMAXPROCS=2 GOFLAGS=-p=1` for Go):
  - From `android`, `./gradlew --no-daemon --max-workers=2 -Dorg.gradle.parallel=false -Dkotlin.compiler.execution.strategy=in-process '-Dorg.gradle.jvmargs=-Xmx1536m -Dfile.encoding=UTF-8' :app:testDebugUnitTest --tests '*TerminalSelectionTest' :app:compileDebugAndroidTestKotlin` failed on the new unit baseline before compilation could run. The same options with `:app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed after the fix.
  - From root, `setsid --wait ./android/scripts/run-integration-tests.sh </dev/null` with `LINGON_IT_CPU_QUOTA_PERCENT=200 LINGON_IT_MEMORY_MAX=7G LINGON_IT_CLASS=systems.pkt.lingon.terminal.TerminalGridViewInstrumentedTest#wideRuneSelectionCopiesOriginalTextAndAnchorsContinuationTouches` failed for each baseline above. The final command used the complete `LINGON_IT_CLASS=systems.pkt.lingon.terminal.TerminalGridViewInstrumentedTest` and passed five tests, zero failures/skips. Each run owned its emulator/adb/harness cleanup; no full release sweep ran.
  - `setsid --wait go test -p 1 ./... </dev/null` passed (`review-selection-go-test.log`); `go vet ./...`, `golint ./...` and `golangci-lint run ./...` passed (`review-selection-vet.log`, `review-selection-golint.log`, `review-selection-golangci.log`, zero lint issues). Documentation readback and `git diff --check` passed.
- Remaining gap: None for the reported selection behavior. A final clean branch review is still required after this commit.

### B-146 Private share authentication rejects debug/local HTTP fallback

- Status: `resolved`
- Report: Review `codex-review.normal-trunk.n4q91n.log` found that the unconditional usable-cookie check rejects the relay's Secure cookie on an HTTP endpoint before the existing token-query websocket fallback can run.
- Scope: Keep usable-cookie validation for HTTPS, permit successfully authenticated HTTP shares to use the existing token fallback, and prove authentication plus handshake together with actual Secure-cookie attributes. Preserve account/share cookie isolation.
- Fix: Require the usable handshake grant only for HTTPS. Successfully authenticated HTTP metadata still supplies a private, endpoint/token/session-bound connection context; its handshake uses the existing token query and never sends the Secure grant or account cookies. HTTPS retains its missing-grant rejection and token redaction.
- Unit repro: `cleartextShareAuthenticationWithSecureCookieReachesTokenHandshake` receives the relay's Secure/HttpOnly/SameSite cookie attributes, authenticates, then performs a real OkHttp upgrade and checks the token query, share header, absent Cookie header, and exactly two requests. It failed before the scheme guard (`review-cleartext-share-corrected-before-unit.log`) and passes after. `successfulShareResponseWithoutHandshakeCookieIsRejected` now uses a real HTTPS server and trusted test-only PKCS12 identity, so the HTTPS requirement is independently exercised. The first TLS fixture read its resource through the wrong class loader and is not accepted as HTTPS evidence (`review-cleartext-share-before-unit.log`); the corrected baseline has one cleartext failure and a passing HTTPS rejection.
- Android repro: With the real relay issuing Secure cookies, `cleartext_share_authentication_uses_token_fallback` failed before with an explicit disconnected/missing-cookie state (`review-cleartext-share-before-connected.log`). It passes after, renders the shared host's ticks, preserves the selected session and view-only scope, and has no authentication error. A real control-share handoff also passes over HTTP (`review-cleartext-share-after-connected.log`, two tests, zero skips/failures).
- HTTPS preservation: Three targeted HTTPS cases pass (`review-cleartext-share-https-connected.log`): token isolation, control-share typing takeover, and view-only resize/input protection. Full Android unit tests and instrumentation compilation pass (`review-cleartext-share-after-unit.log`). Both emulator runs use the owned profile, run alone with two CPUs/7 GB, and clean up emulator, adb and harness resources. The full release sweep was not run.
- Harness/fixture verification: Optional test-only `-cleartext` and `LINGON_IT_CLEARTEXT=1` expose the existing HTTP relay path; HTTPS remains the default. Device URL generation preserves the actual harness scheme. `TestHarnessTransportModes` proves real HTTP and HTTPS listeners serve the expected unauthenticated response and clean up (`review-cleartext-harness-test.log`). The HTTP-specific Android case explicitly skips under the default HTTPS sweep; its separate command is documented in `android/README.md`. The new PKCS12 file is only a public test identity, requires no added dependency, and is loaded/trusted by the TLS unit test.
- Code-path review: The scheme check affects only post-authentication cookie usability; status/session validation still precede context publication. Cookie matching, private jars, connection-context checks and HTTPS query redaction remain in place. The cleartext fallback reaches the same authenticated share websocket handler. No production flag, wire/storage change or support-policy change was introduced.
- Performance: No relevant Android share-authentication timing/allocation gate exists. The HTTP upgrade test proves exactly one auth request and one handshake, with no account refresh, and real control-share takeover keeps its no-reconnect/first-input-once assertions in both transport modes. These are correctness/request-count facts; no measured latency claim is made. Earlier startup/parser measurements remain recorded above.
- Validation commands (serialized, Java 21 for Android; `GOMAXPROCS=2 GOFLAGS=-p=1` for Go):
  - From `android`, `./gradlew --no-daemon --max-workers=2 -Dorg.gradle.parallel=false -Dkotlin.compiler.execution.strategy=in-process '-Dorg.gradle.jvmargs=-Xmx1536m -Dfile.encoding=UTF-8' :app:testDebugUnitTest --tests '*RelayWebSocketClientTest.cleartextShareAuthenticationWithSecureCookieReachesTokenHandshake' --tests '*RelayWebSocketClientTest.successfulShareResponseWithoutHandshakeCookieIsRejected'` produced the corrected failing baseline. The same Gradle options with `:app:compileDebugAndroidTestKotlin` passed before, and `:app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed finally.
  - From root, `setsid --wait ./android/scripts/run-integration-tests.sh </dev/null` with Java 21, `LINGON_IT_CPU_QUOTA_PERCENT=200 LINGON_IT_MEMORY_MAX=7G LINGON_IT_CLEARTEXT=1 LINGON_IT_ONLY=cleartext_share_authentication_uses_token_fallback` failed before. With `LINGON_IT_ONLY=cleartext_share_authentication_uses_token_fallback,control_share_typing_reclaims_control_after_real_handoff` it passed after. A separate default-HTTPS run passed with `LINGON_IT_ONLY=share_tokens_are_isolated,control_share_typing_reclaims_control_after_real_handoff,resize_setting_disabled_for_view_only_share_token`.
  - `setsid --wait go test -p 1 ./cmd/lingon-android-harness -run '^TestHarnessTransportModes$' -count=1 </dev/null` passed. `setsid --wait go test -p 1 ./... </dev/null` passed (`review-cleartext-share-go-test.log`), followed by `go vet ./...`, `golint ./...`, and `golangci-lint run ./...` with zero issues. `bash -n android/scripts/run-integration-tests.sh`, Go formatting, documentation readback and `git diff --check` passed. An initial formatting command used the Android working directory and failed to locate the Go file; formatting was rerun from root before verification.
- Remaining gap: None for the reported HTTP fallback. A final clean branch review is still required after this commit.

### B-143 Browser session management does not recover expired owner credentials

- Status: `resolved`
- Report: Review `codex-review.normal-trunk.Ew2AtP.log` found that a live browser terminal cannot create/rename after its access cookie expires, even with a valid refresh cookie.
- Scope: Recover only an explicit pre-execution HTTP 401, retry once with the same request ID, and retain no-retry behavior for uncertain outcomes.
- Repro/coverage: `TestWebUIManagementAuthRecovery` uses a live browser terminal, an expired access cookie and valid refresh cookie, and real HTTPS relay/host operations. It verifies rename and create each produce exactly 401 → refresh 200 → mutation 200, with identical request body/ID, one created terminal, persisted source name and unchanged socket identity. Periodic listing uses fixture snapshots so it cannot recover the deliberately expired cookie first; each successful mutation refreshes from the real relay.
- Before/after: The corrected HTTPS baseline with the original JavaScript failed on a single 401 (`review-management-browser-tls-before.log`). The final fresh/expired tests and six error-policy subcases passed (`review-management-browser-final.log`). Fresh credentials make one mutation request; lost response, malformed JSON, 403, 500, failed refresh and repeated 401 remain bounded as specified.
- Fixture corrections: The initial browser baseline used a module-scoped function unavailable from page evaluation (`review-management-browser-before.log`); the next baseline reproduced the 401 (`review-management-browser-corrected-before.log`). Subsequent runs exposed a zero-status fallback in the network-loss stub and a test-owned Unix socket path exceeding the platform limit (`review-management-browser-after.log`, `review-management-browser-tls-after.log`, `review-management-browser-diagnostic.log`, `review-management-browser-phases.log`). The final fixture uses HTTPS, a shorter path, and validates result errors explicitly. Setup failures are not accepted as regression evidence.

### B-144 Attach management refresh can block indefinitely

- Status: `resolved`
- Report: Review `codex-review.normal-trunk.Ew2AtP.log` found that refresh receives the long-lived attach context before the existing mutation deadline is established.
- Scope: One 15-second operation budget for refresh and mutation; use the client's existing injected clock and prove that timeout releases management availability through a test-owned PTY.
- Fix/coverage: The operation establishes a clock-driven cancellation context before refresh, preserves the timeout cause, and passes that context to the mutation. `TestManagementBudgetIncludesStalledRefresh` and `TestManagementMutationUsesRemainingRefreshBudget` explicitly advance mock time and verify refresh timeout and the single remaining second after a fourteen-second refresh. Both failed before (`review-management-timeout-corrected-before.log`) and passed after with the race detector (`review-management-timeout-after.log`). Parent attach context stays alive.
- PTY proof: `TestAttachManagementRefreshTimeoutAllowsNextRename` drives actual Ctrl+L e, stalls authentication, advances the owned clock by fifteen seconds, observes the deadline error, then performs a successful rename and executes a shell expression whose result differs from the entered command. The corrected baseline failed at the missing timeout (`review-management-timeout-pty-corrected-before.log`); final race run passed (`review-management-timeout-pty-final.log`). It owns every terminal and never touches inherited stdio.
- Fixture corrections: The first unit baseline stalled in server cleanup because its request body was not drained. Only that test-owned process was stopped; `review-management-timeout-before.log` is not accepted as evidence. The server now drains the body and has an explicit cleanup release. Initial PTY readiness used hidden chrome, and an initial post-fix assertion expected an auto-hidden label (`review-management-timeout-pty-before.log`, `review-management-timeout-pty-after.log`); readiness now uses the received snapshot and successful rename is asserted through relay metadata.

### B-145 Attach startup races tab-switch output selection

- Status: `resolved`
- Report/repro: The broader race gate reported `Client.run` reassigning `c.stdout` concurrently with `SetStdout` during `TestAttachNoFullRedrawAfterTabSwitchWithLocalPTYs` (`review-management-lifecycle-no-flicker.log`).
- Fix: Remove the redundant startup assignment. The writer getter already resolves explicit selection, configured stdout, and the default output; tab selection remains protected by the existing render mutex. No additional render-path lock or alternate writer implementation was added.
- Verification: The existing permanent PTY regression failed with the race detector before this removal and passed five times after (`review-management-writer-race-after.log`). It was not edited or weakened. The broader final lifecycle/no-flicker and standard gates are recorded below.

#### Shared verification for B-143–B-145

- Explicit code-path review: Relay 401 precedes host dispatch. Browser recovery reuses the serialized operation and permits only one retry; a changed active authentication view blocks that retry. Uncertain outcomes remain non-retriable. Attach uses one clock timer across refresh and HTTP, stops it on completion, preserves parent cancellation, and leaves the terminal context alive. Startup no longer republishes a writer selected by the tab owner. No public API, wire, storage, compatibility shim or supported-platform policy changed.
- Performance: The existing five-alternating-start gate passed before and after: routed medians 35.757/33.180 ms; direct medians 27.748/29.296 ms (`review-management-startup-before.log`, `review-management-startup-after.log`). This one pair does not establish a speedup or universal latency guarantee. Fresh browser credentials add no refresh request. No relevant browser-authentication timing/allocation gate exists; the focused HTTPS request-count gates are correctness evidence.
- Final verification: After the supporting writer correction, the same startup gate passed again with routed/direct medians 30.672/25.030 ms (`review-management-startup-final.log`). The full targeted lifecycle/no-flicker race gate passed (`review-management-lifecycle-final.log`), then the standard Go suite (`review-management-go-test.log`), vet, golint and golangci-lint passed. Formatting, documentation readback and diff whitespace checks passed. No permanent no-flicker test was changed.
- Validation commands (serialized, `GOMAXPROCS=2 GOFLAGS=-p=1`, detached with `/dev/null` input):
  - `go test -p 1 ./internal/attach -run '^TestManagement(Budget|Mutation)' -count=1 -timeout=30s` produced the corrected failing baseline. The same focused tests with `-race -timeout=45s` passed after.
  - `go test -p 1 -tags integration ./integration/webui -run '^TestWebUIManagementAuthRecovery$' -count=1 -timeout=90s` failed with original JavaScript; the fixed source was restored immediately by the command's cleanup trap. `go test -p 1 -tags integration ./integration/webui -run '^TestWebUI(SessionManagement|ManagementAuthRecovery)$' -count=1 -timeout=120s` passed finally, including six error-policy subcases.
  - `go test -p 1 -tags integration ./integration/pty/attach -run '^TestAttachManagementRefreshTimeoutAllowsNextRename$' -count=1 -timeout=45s` failed before; the final fixture passed with `-race`. `go test -p 1 -race -tags integration ./integration/pty/attach -run '^TestAttachNoFullRedrawAfterTabSwitchWithLocalPTYs$' -count=5 -timeout=90s` passed after removing the startup race.
  - `go test -p 1 -tags integration ./integration/pty/attach -run '^TestSessionManagementStartupPerformance$' -count=1 -v -timeout=90s` passed before and after with the same executable and credential routing.
  - `go test -p 1 -race -tags integration ./integration/pty/attach ./integration/pty/session -run 'TestAttachManagementRefreshTimeoutAllowsNextRename|TestSessionManagement(LocalCLI|RelayAndHeadfulCLI)$|TestHost.*(WrapAroundTabSwitch|RepaintBaseTopRowBeforeOverlay)|TestAttachNoFullRedraw.*TabSwitch' -count=1 -timeout=180s` exposed B-145 first and passed after its fix. `go test -p 1 ./...`, `go vet ./...`, `golint ./...`, and `golangci-lint run ./...` passed finally, with zero lint issues. `gofmt -l` on changed Go files and `git diff --check` reported no problems.
  - Earlier browser diagnostic runs used `-run '^TestWebUISessionManagementExpiredAccess$'`, then `-run '^TestWebUISessionManagement'`, as classified above; the final short-name fixture is authoritative. All tests, build subprocesses, browser instances and harnesses ran serially and cleaned up owned resources.

### B-142 Android bootstrap applies sessions from a superseded context

- Status: `resolved`
- Area: `android`, `authentication`, `session-list`
- Report: B-139's gated-response investigation exposed the same stale-response defect in the initial `bootstrapSessions` request. A delayed successful old-account response can restore sessions/login state after logout or switching endpoints.
- Intended behavior: Fence bootstrap results and failures to the context that issued the request, preserving the new context's sessions, login state, and status.
- Fix: A shared session-context generation and endpoint/username/share identity check fences bootstrap success and failure, management results, and active-session selection after a suspended saved-preference lookup. Stale success cannot restore old sessions/login state; stale errors cannot clear the new account's sessions/status. Current-context recovery retains its existing behavior.
- Regression: `bootstrapSessionFetchIsIgnoredAfterEndpointChange` and `bootstrapSessionFetchIsIgnoredAfterLogout` fail before the fence (`review-followup-bootstrap-before-unit.log`). `staleBootstrapAuthenticationFailureDoesNotClearNewAccount` fails before the fence (`review-followup-bootstrap-error-before-unit.log`), and `pendingSessionPreferenceCannotRestoreOldActiveSessionAfterEndpointChange` fails before the preference check (`review-followup-preference-before-unit.log`). All pass in the full unit suite after. `bootstrap_session_fetch_does_not_restore_old_endpoint_sessions` passes against the real relay with an explicitly gated response and actual Android UI; the login screen remains visible after the old result completes.

### B-139 Android applies superseded management session lists

- Status: `resolved`
- Area: `android`, `session-management`
- Report: Review `codex-review.normal-trunk.6IqSbR.log` at `089c399` finds that logout or endpoint switching during the post-management session fetch allows old sessions/caches and success status into the new context.
- Intended behavior: Revalidate the management generation and authentication context after the suspended fetch, before applying any result or selecting a created terminal.
- Fix: Fetch separately, then recheck the issuing context before caching sessions or assigning the created-session selection. Recheck again before publishing success/error status; the shared preference-selection fence also prevents a later suspended lookup from restoring an old active session.
- Regression: `managementSessionFetchIsIgnoredAfterEndpointChange` and `managementSessionFetchIsIgnoredAfterLogout` cover create and rename with an explicitly released session response. Both fail with the post-fetch fence removed (`review-followup-management-isolated-before-unit.log`) and pass with it restored. `management_session_fetch_does_not_restore_old_endpoint_sessions` passes using a real rename, gated real session-list response, endpoint change, and Android UI state assertions.
- Fixture audit: Early fixtures accidentally gated initial bootstrap or returned old data to a new endpoint request (`review-followup-android-before-unit.log`, `review-followup-android-after-unit.log`, `review-followup-management-before-unit.log`, `review-followup-android-final-unit.log`). Those management failures are not accepted as repro evidence. The final fixture installs its gate after initialization and rejects fresh requests in the unauthenticated new context. The bootstrap issue exposed during that investigation is independently reproduced and fixed as B-142.

### B-140 Offline host rename leaves the old tab label

- Status: `resolved`
- Area: `session-management`, `host-tui`
- Report: Review `codex-review.normal-trunk.6IqSbR.log` finds that renaming updates the local name but repaints an unchanged tab model. Offline/unpublished hosts have no relay update to rebuild it.
- Intended behavior: Ctrl+L e changes the visible local tab label immediately, preserving session identity and terminal rendering invariants.
- Fix: Rebuild the tab model from local names and the current remote-session cache before the existing repaint. No new relay request or alternate rendering path is introduced.
- Regression: `TestOfflineHostRenameUpdatesVisibleTabWithoutRelay` drives actual Ctrl+L e through a `ptytest`-owned host PTY with an injected mock clock, checks row 1 for the new label and absence of the old label, then drives terminal input. It fails before (`review-followup-offline-before.log`) and passes after with the race detector (`review-followup-offline-after.log`). Online/headless lifecycle and permanent tab-switch no-flicker tests also pass, unmodified.

### B-141 Android truncates links at wide Unicode continuation cells

- Status: `resolved`
- Area: `android`, `terminal-links`
- Report: Review `codex-review.normal-trunk.6IqSbR.log` finds that a zero-rune continuation cell terminates `https://example.test/日本`, opening a truncated URL.
- Intended behavior: Extract the full URL once and retain the continuation cell's tappable hit area, including the last wide character. Actual whitespace/concealed cells and hard row boundaries still delimit links.
- Fix: Omit zero-rune continuation cells from extracted text while extending the preceding character's mapped hit area. UTF-16 mapping retains complete Japanese, supplementary emoji, and combining-grapheme paths. Existing whitespace, hidden-cell, punctuation, and row-boundary handling remain.
- Regression: `wideUnicodeLinksKeepFullUrlAndContinuationHitArea` fails before with `/日本` truncated to `/日` (`review-followup-android-before-unit.log`) and passes after, asserting every occupied link cell and the following delimiter. `TerminalGridViewInstrumentedTest.wideUnicodeLinkTapOnLastContinuationOpensFullUrl` passes with actual motion events on the final continuation cell, checking the full URL in an Android ACTION_VIEW intent. Existing link and selection instrumentation also passes.

#### Shared verification for B-139–B-142

- Full Android unit suite and instrumentation compile passed (`review-followup-context-after-unit.log`); both targeted emulator runs passed: six session/context/control cases (`review-followup-context-connected.log`) and four terminal-view cases (`review-followup-links-connected.log`). Owned emulator, adb, and harness cleanup is performed by the integration runner. The full release instrumentation sweep was not run.
- Explicit code-path review confirmed context checks run before cache/selection/status mutation after suspension, tab rebuilding uses current local names without changing identity, and link hit mapping includes the final continuation while leaving real delimiters intact. No public wire format, storage migration, or compatibility shim was added. Permanent no-flicker tests were not modified.
- No relevant timing/allocation gate exists for Android link extraction or context checks. Full unit coverage includes the existing one-scan-per-update and no-scan-for-unchanged-frame checks. Rename rebuilding occurs only on the management operation and reads the existing remote cache. Correctness and no-flicker evidence are recorded; no measured latency improvement is claimed.
- Validation commands (serialized):
  - From `android`, Java 21: `./gradlew --no-daemon --max-workers=2 -Dorg.gradle.parallel=false -Dkotlin.compiler.execution.strategy=in-process '-Dorg.gradle.jvmargs=-Xmx1536m -Dfile.encoding=UTF-8' :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed finally (`review-followup-context-after-unit.log`). Earlier full checks passed before the supporting bootstrap tests (`review-followup-android-verified-unit.log`), and the fixture failures are classified above.
  - The same Gradle options with `:app:testDebugUnitTest --tests '*AppViewModelTest.managementSessionFetchIsIgnoredAfterEndpointChangeOrLogout' --tests '*TerminalLinksTest.wideUnicodeLinksKeepFullUrlAndContinuationHitArea' :app:compileDebugAndroidTestKotlin` produced the initial link repro/management fixture failure. Subsequent targeted baselines used `--tests '*AppViewModelTest.managementSessionFetchIsIgnoredAfter*'` (corrected fixture log above), `--tests '*AppViewModelTest.bootstrapSessionFetchIsIgnoredAfter*'`, `--tests '*AppViewModelTest.staleBootstrapAuthenticationFailureDoesNotClearNewAccount'`, and `--tests '*AppViewModelTest.pendingSessionPreferenceCannotRestoreOldActiveSessionAfterEndpointChange'`; each accepted baseline fails before its fence as recorded above.
  - From root with Java 21 and `GOMAXPROCS=2 GOFLAGS=-p=1 LINGON_IT_CPU_QUOTA_PERCENT=200 LINGON_IT_MEMORY_MAX=7G`, `./android/scripts/run-integration-tests.sh` passed with `LINGON_IT_ONLY=management_session_fetch_does_not_restore_old_endpoint_sessions,bootstrap_session_fetch_does_not_restore_old_endpoint_sessions,settings_create_terminal_and_rename_session,owner_typing_reclaims_control_after_real_handoff,control_share_typing_reclaims_control_after_real_handoff,resize_setting_disabled_for_view_only_share_token`, then passed separately with `LINGON_IT_CLASS=systems.pkt.lingon.terminal.TerminalGridViewInstrumentedTest`. Both use `setsid --wait`, `/dev/null` input, and the isolated emulator profile.
  - From root with `GOMAXPROCS=2 GOFLAGS=-p=1`, `setsid --wait go test -p 1 -tags integration ./integration/pty/session -run '^TestOfflineHostRenameUpdatesVisibleTabWithoutRelay$' -count=1 </dev/null` failed before; the same test with `-race` passed after. `setsid --wait go test -p 1 -race -tags integration ./integration/pty/session ./integration/pty/attach -run 'TestOfflineHostRenameUpdatesVisibleTabWithoutRelay|TestSessionManagement(LocalCLI|RelayAndHeadfulCLI)$|TestHost.*(WrapAroundTabSwitch|RepaintBaseTopRowBeforeOverlay)|TestAttachNoFullRedraw.*TabSwitch' -count=1 </dev/null` passed (`review-followup-lifecycle-no-flicker.log`). Every interactive fixture owns its sub-PTYs.
  - `setsid --wait go test -p 1 ./... </dev/null` passed (`review-followup-go-test.log`), followed by `go vet ./...`, `golint ./...`, and `golangci-lint run ./...` with zero lint issues. Changed Go files pass `gofmt -l`; documentation was read back and `git diff --check` is clean.
  - The completed review ran `GOMAXPROCS=2 go test -count=1 -p 1 ./internal/sessionmanage ./internal/relay ./internal/control ./internal/backoff`, which passed. Review scope was normal `androidurl` at `089c399` against local `trunk` at `8751548`, using the prescribed model and reasoning configuration (`codex-review.normal-trunk.6IqSbR.log`). A final clean review remains pending; B-135 is separately open.

### B-138 Session creation exposes relay credentials in child arguments

- Status: `resolved`
- Area: `session-management`, `headless`, `credentials`
- Report: Normal branch review of `androidurl` at `8ae3235` against `trunk` at `8751548` (`codex-review.normal-trunk.ERQLz0.log`) found that owner-authorized terminal creation adds the current access token to child argv, even when the host authenticated from a protected auth file.
- Repro: Create a terminal through attach's Ctrl+L c, then inspect that test-owned child's `/proc/<pid>/cmdline`. The relay account access token appears after `--token`.
- Fix: Reuse the selected auth file when present; transfer token-only credentials through a private inherited pipe consumed and closed before terminal startup. Only its numeric descriptor is added to the child environment. Filter inherited/resupplied launcher descriptor markers, reject inherited stdio descriptors, and keep credential writing outside the readiness wait so an early child failure cannot block startup cancellation. Generated child arguments and environment carry no access token; explicit user-supplied token flags retain their semantics.
- Regression: `TestSessionManagementChildCredentialsStayOutOfProcessMetadata` drives actual Ctrl+L c through an isolated attach CLI, inspects only the created child's command line/environment, checks auth-file reuse, and proves the child's authenticated publisher can receive a rename. Both auth-file and token-only cases failed before the fix with a credential in `cmdline` and pass after. `TestLaunchTransfersTokenThroughOwnedPipe` verifies a small credential, a credential larger than pipe capacity, failure before reading it, and stale inherited/override descriptor markers. The helper child checks arguments/environment and cleared descriptor metadata. `TestReadLaunchTokenRejectsInheritedStdio` never accesses runner stdio. These unit children use owned pipes and `/dev/null`; interactive integration fixtures use only `ptytest`-owned sub-PTYs.
- Code-path review: The lifecycle host selects the auth source, not remote clients. Both local daemon and relay management use `Runner.Manage`; only token-only hosts allocate the credential pipe. The CLI consumes it before auth resolution, daemon construction, and shell startup. Existing public/facade options and relay messages are unchanged. `SESSION_MANAGEMENT.md` documents the credential boundary.
- Performance: The existing `TestSessionManagementStartupPerformance` compares five alternating direct/routed starts. Initial routed medians were 34.297 ms before and 35.329 ms after; direct medians shifted from 25.974 to 24.930 ms. Repeating three times with original versus private-pipe credential routing held all other code constant: routed medians 34.865/27.902/31.991 ms versus 32.151/31.324/36.170 ms. Median of those medians: 31.991 versus 32.151 ms (+0.160 ms, about 0.5%), with overlapping ranges and substantial host variation. No stable slowdown or speedup is established; no universal latency guarantee is claimed. The fixed routing was restored before final checks.
- Verification commands (serialized from repo root with `GOMAXPROCS=2 GOFLAGS=-p=1`):
  - `setsid --wait go test -p 1 -tags integration ./integration/pty/attach -run '^TestSessionManagementChildCredentialsStayOutOfProcessMetadata$' -count=1 </dev/null` failed before and passed after (`review-credentials-before.log`, `review-credentials-after.log`).
  - `setsid --wait go test -p 1 -race ./internal/headless ./cmd/lingon -run 'TestLaunch|TestReadLaunchToken|TestHeadlessStartup|TestResolveHeadlessRelayConfig' -count=1 </dev/null` passed (`review-credentials-pipe.log`).
  - `setsid --wait go test -p 1 -race -tags integration ./integration/pty/attach -run '^TestSessionManagement' -count=1 -v </dev/null` passed local/relay lifecycle, both credential cases, and startup checks (`review-credentials-lifecycle.log`). The harness/tests use the race detector; this command's real CLI helper is a normal Go build.
  - `setsid --wait go test -p 1 -tags integration ./integration/pty/attach -run '^TestSessionManagementStartupPerformance$' -count=1 -v </dev/null` passed before/after (`review-credentials-startup-before.log`, `review-credentials-startup-after.log`); the same command with `-count=3` passed under original/fixed routing (`review-credentials-startup-baseline-repeat.log`, `review-credentials-startup-fixed-repeat.log`).
  - `setsid --wait go test -p 1 -tags integration ./integration/pty/session ./integration/pty/attach -run 'TestHost.*(WrapAroundTabSwitch|RepaintBaseTopRowBeforeOverlay)|TestAttachNoFullRedraw.*TabSwitch' -count=1 </dev/null` passed (`review-credentials-no-flicker.log`). Existing no-flicker assertions were not modified.
  - `setsid --wait go test -p 1 ./... </dev/null`, `go vet ./...`, `golint ./...`, and `golangci-lint run ./...` passed; golangci-lint reported zero issues (`review-credentials-go-test.log` captures the full tests). Changed files pass `gofmt -l`; docs were read back and `git diff --check` is clean.
- Review-loop boundary: This batch addresses the credential P1 from `codex review -c model=gpt-6-sol -c model_reasoning_effort=high --base trunk`. That completed review's other P1 is B-134, whose control policy requires product direction. A clean final review is not claimed; re-review follows that decision and remediation. No pre-existing or concurrent worktree changes were present.

### B-136 Absolute cursor positioning clamps to scrolling margins with origin mode off

- Status: `resolved`
- Area: `terminal`, `vt-emulator`
- Report: `stash/cupbug.md` describes a bottom-anchored chat feed overwriting earlier lines and being unable to address reserved prompt rows.
- Repro: On a 40-column, 8-row screen, write `ESC[?6l ESC[1;6r ESC[8;1H` (without spaces). CUP incorrectly stops at row 6; oversized row 65535 and HVP have the same failure. Repeated bottom anchors followed by CUU overwrite the first feed row. VPA shares the affected positioning path.
- Fix: Apply the bottom scrolling-margin clamp only with DECOM on. CUP/HVP/VPA use physical screen bounds with DECOM off; origin-relative positioning, inline offsets, column bounds, cursor tracing, and pending-wrap cancellation remain intact.
- Regression: `cursor_position_scroll_region_test.go` covers CUP/HVP origin modes, origin reset, oversized/zero coordinates, VPA column preservation, successive feed rows, both reserved prompt rows, inline offsets, and pending wrap. Before the fix, CUP/HVP/VPA stopped at row 6 instead of 8, the second feed chunk overwrote `one` with `two`, and both prompt-row assertions failed. `TestHostAndAttachBottomAnchoredFeedOutsideScrollRegion` also failed before the fix (feed row 2 empty instead of `one`); it now drives real attach input through the relay and asserts all accumulated feed rows and reserved rows on both rendered terminals. Every terminal control is confined to `ptytest`-owned sub-PTYs; checks use the injected mock clock.
- Verification: The commands below passed, including the full emulator race suite, three consecutive host/attach race runs, existing permanent no-flicker checks unchanged, and all standard Go gates. Explicit code-path review confirmed only DECOM-on positioning sees the margin clamp and all three absolute-position handlers share the corrected path.
- Performance: `BenchmarkAbsoluteCursorPositionScrollRegion` feeds CUP/CUU/HVP/VPA through the real parser with partial margins. Five 1-second samples before/after the clamp change measured median DECOM-off 238.3/237.9 ns per batch and DECOM-on 234.9/236.0 ns per batch. Every sample retained 16 B and 3 allocations per batch. Timing changes are within observed variation; no latency improvement is claimed. Baseline ranges were 235.5–271.0 ns (off), 234.5–238.7 ns (on); fixed ranges were 237.3–239.4 ns (off), 234.7–239.1 ns (on).
- Scope: Reproduced the report's byte stream and a controlled host/attach application fixture; the external Softline application itself was not run. No Android or web UI code changed.

### B-137 Host/attach regression exposes startup and teardown races

- Status: `resolved`
- Area: `session`, `attach`, `verification`
- Repro: Run `TestHostAndAttachBottomAnchoredFeedOutsideScrollRegion` under `-race`. The new owned-PTY fixture exposed raw-state restoration and attach signal-wait checks calling `File.Fd` during descriptor closure, plus publisher `OnSessions` reading the remote-manager pointer while host startup assigns it.
- Fix: Protect terminal control with the existing `terminal.WithFileDescriptor` helper and classify attach stdin before starting its reader. Retain that classification through closure so terminal shutdown keeps its bounded input wait. Construct the remote manager before starting local publishers; keep its background refresh startup after initial local-session readiness.
- Regression: The new host/attach fixture failed with actual race reports in raw restoration, signal-wait terminal inspection, and remote-manager initialization. It now passes three consecutive runs under `-race`. Existing `TestAttachFailsWithoutHost` and `TestAttachExitsWhenHostDisconnects` caught an intermediate post-close classification mistake; both pass three consecutive race runs after retaining the initial classification. Full attach/session suites and permanent host/attach no-flicker checks also pass.
- Review: Descriptor callbacks cannot observe or act on a closed/reused descriptor; publisher callbacks see a fully constructed, stable remote-manager pointer. No new lock, timer, or protocol behavior was introduced.

Validation for B-136 and B-137 (commands serialized, `GOMAXPROCS=2`):

- Before the emulator fix: `setsid --wait go test -p 1 ./internal/terminal/emu -run 'Test(AbsoluteCursorPosition|VPAScrollRegion|BottomAnchoredFeed)' -count=1 </dev/null` failed with the row, overwrite, prompt, inline-offset, and pending-wrap failures above. The new host/attach integration test also failed before the fix using the command below without `-race`, `-count=1`.
- `setsid --wait go test -p 1 -race ./internal/terminal/emu -count=1 </dev/null` passed the full emulator suite.
- `setsid --wait go test -p 1 -race -tags integration ./integration/pty/session -run '^TestHostAndAttachBottomAnchoredFeedOutsideScrollRegion$' -count=3 </dev/null` passed after the final runtime repairs.
- `setsid --wait go test -p 1 -race ./internal/attach -run '^TestAttach(FailsWithoutHost|ExitsWhenHostDisconnects)$' -count=3 </dev/null` passed.
- `setsid --wait go test -p 1 -tags integration ./integration/pty/session ./integration/pty/attach -run 'TestHost.*(WrapAroundTabSwitch|RepaintBaseTopRowBeforeOverlay)|TestAttachNoFullRedraw.*TabSwitch' -count=1 </dev/null` passed (`cupbug-no-flicker.log`). No existing no-flicker test was modified.
- `setsid --wait go test -p 1 ./internal/terminal/emu -run '^$' -bench '^BenchmarkAbsoluteCursorPositionScrollRegion$' -benchmem -benchtime=1s -count=5 </dev/null` passed for both the original and fixed clamp, with all other code and the benchmark held constant. The fixed clamp was restored before the final gates.
- Final standard gates: `setsid --wait go test -p 1 ./... </dev/null` passed (`cupbug-go-test.log`); `go vet -p 1 ./...`, `GOFLAGS=-p=1 golint ./...`, and `GOFLAGS=-p=1 golangci-lint run ./...` passed, with zero golangci-lint issues. The first full test attempt found the intermediate attach shutdown failures; they were fixed before this final passing run. Changed Go files pass `gofmt -l`, and `git diff --check` is clean.

### B-134 Android cannot reclaim control after a handoff

- Status: `resolved`
- Area: `android`, `session-control`
- Report: Third normal branch review against `trunk` found that host input or another controller sets `hasControl=false`, after which every Android input path is blocked. There is no explicit control-reacquisition action; reconnecting or switching sessions is the current workaround. See `codex-review.normal-trunk.x6RgwP.log`.
- Approved policy (2026-10-04): Transparent control takeover on the first authorized keystroke, with no extra button. Account and control-share clients may type after losing the lease; view-only and unknown-scope shares remain blocked. The triggering payload must reach the host exactly once, without reconnecting or an extra control request. Relay control frames remain authoritative for lease ownership. This supersedes B-122's blanket lease-based input guard.
- Re-review: The normal `androidurl` review at `8ae3235` against `trunk` at `8751548` reports this again as P1 (`codex-review.normal-trunk.ERQLz0.log`). The product decision above resolves the previous pause.
- Repro: Connect an Android account or control share, connect a second authorized client with `wants_control=true`, wait for Android to lose the lease, then send one hardware key. The old guard drops that key despite permission to control the session.
- Fix: Distinguish authenticated input permission from current lease ownership. Account input requires login; share input requires the `/auth/share` response's exact `control` scope. Reset share permission on token/endpoint/account changes and failed authentication. Both text and raw bytes use this permission guard; locked/disconnected input remains blocked. Typing does not optimistically change `hasControl`; the relay grants the lease and forwards the same input frame.
- Regression: `ownerInputReclaimsControlWithoutDroppingOrRepeatingFirstPayload` and `authenticatedShareScopeGatesTypingIndependentlyOfLeaseOwnership` both failed before the fix and pass after. Coverage includes text/raw payloads, exact dispatch counts, unchanged lease state until a relay frame, view/empty/unknown scopes despite stale lease state, token changes, dialog dismissal, locked/disconnected/unauthenticated states, and application-cursor translation. Test scheduling uses `StandardTestDispatcher` rather than wall-clock delay assertions.
- End to end: `owner_typing_reclaims_control_after_real_handoff` and `control_share_typing_reclaims_control_after_real_handoff` use a second real websocket client and one hardware key without retries. Each fails at reacquisition under the old guard and passes under the new guard, asserting one host echo, one local dispatch, actual lease ownership, and unchanged connection epoch. `resize_setting_disabled_for_view_only_share_token` passes with real UI input rejected and the dispatch nonce unchanged; its old time-window absence check was replaced with an explicit rejection assertion.
- Fixture audit: The initial control-share peer omitted the share-session header and failed before reaching handoff (`android-control-before-connected.log`, `android-control-after-connected.log`). That is not regression evidence. The corrected peer uses the active share authority. Its old-guard rerun fails specifically after the triggering key (`android-control-share-before-connected.log`); the final corrected three-case run passes with zero failures (`android-control-final-connected.log`). Owned emulator/adb/harness cleanup is handled by the integration runner.
- Code-path review: All share-token assignment/reset paths clear or preserve only the same token's authenticated permission. Account login, endpoint change, logout, expiry, and share switching clear the share grant. Unknown scope defaults to denial. Input dispatch still uses the existing single `In` frame; lease updates remain in Welcome/Control handling. There is no new UI action or wire-protocol change.
- Performance: No relevant Android input timing/allocation gate exists. Focused tests prove takeover adds neither a control-request round trip nor a reconnect and forwards the first payload once. This is correctness/connection evidence, not a latency benchmark or a measured speedup.
- Verification commands (serialized):
  - From `android`, Java 21: `./gradlew --no-daemon --max-workers=2 -Dorg.gradle.parallel=false -Dkotlin.compiler.execution.strategy=in-process '-Dorg.gradle.jvmargs=-Xmx1536m -Dfile.encoding=UTF-8' :app:testDebugUnitTest --tests '*AppViewModelTest.ownerInputReclaimsControlWithoutDroppingOrRepeatingFirstPayload' --tests '*AppViewModelTest.authenticatedShareScopeGatesTypingIndependentlyOfLeaseOwnership'` failed before (`android-control-before-unit.log`). The same Gradle options with `:app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed after (`android-control-after-unit.log`).
  - From root, Java 21 with `GOMAXPROCS=2 GOFLAGS=-p=1 LINGON_IT_CPU_QUOTA_PERCENT=200 LINGON_IT_MEMORY_MAX=7G`: `./android/scripts/run-integration-tests.sh` ran the initial two-case baseline (`LINGON_IT_ONLY=owner_typing_reclaims_control_after_real_handoff,control_share_typing_reclaims_control_after_real_handoff`), first three-case check (the same list plus `resize_setting_disabled_for_view_only_share_token`), corrected old-guard control-share baseline (`LINGON_IT_ONLY=control_share_typing_reclaims_control_after_real_handoff`), and final corrected three-case check. Outcomes and fixture limitation are recorded above. Commands use `setsid --wait`, `/dev/null` input, and the runner's isolated emulator profile, capped at two cores/7 GB; the full instrumentation sweep was not run.
  - From root, `GOMAXPROCS=2 GOFLAGS=-p=1`: `setsid --wait go test -p 1 ./... </dev/null` passed (`android-control-go-test.log`); `go vet ./...`, `golint ./...`, `golangci-lint run ./...` passed with zero lint issues. From the nested `android` Go module, `go test ./...`, `go vet ./...`, `golint ./...`, and `golangci-lint run ./...` passed.
  - `bash -n android/scripts/run-integration-tests.sh`, `gofmt -l cmd/lingon-android-harness/main.go android/cmd/lingon-android-tools/main.go android/cmd/lingon-android-tools/main_test.go`, and `git diff --check` passed. Documentation was read back. Permanent no-flicker regressions were not edited.
- Remaining gap: None for the approved takeover behavior. B-135 remains a separate open share-authentication race; a clean final branch review is not yet claimed.

### B-135 Superseded share-auth requests overwrite the active cookie

- Status: `resolved`
- Area: `android`, `share-authentication`
- Report: Third normal branch review found that overlapping `/auth/share` calls use the same cookie jar. A stale successful response can replace the newer cookie; a stale invalid-token response can delete it before the current HTTPS websocket handshake. UI stale-result checks happen after OkHttp applies cookie writes. See `codex-review.normal-trunk.x6RgwP.log`.
- Repro: Gate an old `/auth/share` response, finish authentication for a new token, then release the old success or invalid-token response before the current websocket handshake. Previously, success replaced the new grant with the old cookie and rejection deleted the new grant. The account cookie also entered the share handshake.
- Fix: Each authentication attempt owns one ephemeral share-cookie jar. Its result carries the private transport into the selected handshake, bound to endpoint, token, and session. Missing/mismatched authentication and success without a usable cookie are rejected. Account refresh bypasses share-marked websocket requests and leaves authentication to the relay, so it cannot consult or substitute account authority. Owner refresh retains its existing behavior. No wire/storage change, global token cache, or persistent authority tracking was added.
- Failing-before evidence: `lateSuccessfulShareAuthenticationCannotReplaceCurrentHandshakeCookie` and `lateInvalidShareAuthenticationCannotDeleteCurrentHandshakeCookie` both failed with the original shared jar (`review-share-cookie-compiled-before-unit.log`). The first attempted baseline had a test compilation error and is not repro evidence (`review-share-cookie-before-unit.log`). `privateShareHandshakeDoesNotConsultAccountCookies` separately failed before repairing the inherited account-refresh check (`review-share-interceptor-before-unit.log`). All three regressions pass after the complete fix.
- Additional coverage: Cookie expiration uses an injected clock value; domain/path/secure matching, account-cookie exclusion, invalidated grants, wrong connection context, missing grant, status/Retry-After preservation, and HTTPS token redaction are covered. The interceptor test proves both successful and rejected share requests reach the relay exactly once without account refresh. The concurrency tests gate response order explicitly and complete a real OkHttp websocket upgrade; timeout bounds do not establish ordering.
- Android-visible verification: Seven targeted real HTTPS relay/app cases passed: `share_tokens_are_isolated`, `share_authentication_recovers_after_two_transient_failures`, `share_token_dialog_rejects_invalid_token`, `control_share_typing_reclaims_control_after_real_handoff`, `owner_typing_reclaims_control_after_real_handoff`, `resize_setting_disabled_for_view_only_share_token`, and `settings_session_management_hidden_for_share` (`review-share-cookie-connected.log`). The isolated runner cleaned up its owned emulator, adb, and harness. The full release sweep was not run.
- Code-path review: The per-attempt jar receives cookies before authentication returns; stale responses mutate only their own jar. The selected result supplies the same transport for the handshake, and option validation occurs before opening a socket. The cloned transport retains the provider's connection pool, dispatcher, and TLS configuration. `/auth/share` and share-marked handshakes bypass account refresh; normal account requests still use it. View-only scope remains denied input and control scope remains relay-authoritative.
- Performance: No relevant share-authentication timing/allocation gate exists. The strongest available gates are the controlled HTTP/websocket regressions and targeted HTTPS app flows. The change adds no authentication request to the normal attach sequence and reuses the existing transport resources. Typing takeover still sends its first payload exactly once without a reconnect or control-request round trip. These are correctness/resource facts, not a measured latency claim; earlier startup/parser measurements remain recorded above.
- Validation commands (serialized, Java 21 for Android, `GOMAXPROCS=2 GOFLAGS=-p=1` for Go):
  - From `android`, `./gradlew --no-daemon --max-workers=2 -Dorg.gradle.parallel=false -Dkotlin.compiler.execution.strategy=in-process '-Dorg.gradle.jvmargs=-Xmx1536m -Dfile.encoding=UTF-8' :app:testDebugUnitTest` with the two `RelayWebSocketClientTest.late*ShareAuthentication*` tests produced the valid failing baseline; the same options with `--tests systems.pkt.lingon.data.AuthRefreshInterceptorTest.privateShareHandshakeDoesNotConsultAccountCookies` produced the interceptor baseline.
  - The same Gradle options with `:app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed initially (`review-share-cookie-after-unit.log`) and finally after the interceptor repair (`review-share-cookie-final-unit.log`).
  - From root, `setsid --wait ./android/scripts/run-integration-tests.sh </dev/null` with Java 21, `LINGON_IT_CPU_QUOTA_PERCENT=200 LINGON_IT_MEMORY_MAX=7G`, and `LINGON_IT_ONLY=share_tokens_are_isolated,share_authentication_recovers_after_two_transient_failures,share_token_dialog_rejects_invalid_token,control_share_typing_reclaims_control_after_real_handoff,owner_typing_reclaims_control_after_real_handoff,resize_setting_disabled_for_view_only_share_token,settings_session_management_hidden_for_share` passed all seven tests.
  - `setsid --wait go test -p 1 ./... </dev/null` passed (`review-share-cookie-go-test.log`), then `go vet ./...`, `golint ./...`, and `golangci-lint run ./...` passed with zero issues. Documentation readback and `git diff --check` passed before commit.
- Remaining gap: None for the cookie race repros. The final clean branch review remains pending.

### B-133 PTY shutdown races descriptor inspection against closure

- Status: `resolved`
- Area: `attach`, `session`, `verification`
- Repro: The first expanded attach race check exposed races in multi-attach raw-state restoration, host terminal-size lookup, and Linux PTY reader descriptor lookup while owned PTYs close. See `tab-loading-race.log`.
- Fix: Use `SyscallConn.Control` for terminal control and `SyscallConn.Read` for each bounded Linux poll/read attempt. Descriptors stay protected against concurrent closure throughout their use; raw restoration cannot act on a stale numeric descriptor. The existing 50 ms poll bound and cancellation checks remain.
- Coverage: `TestWithFileDescriptorFencesConcurrentClose` checks control during concurrent closure and rejects closed/nil files. The previously failing loading and remote-tab cases pass with the race detector, alongside PTY reader shutdown/cancellation checks. These fixtures own their PTYs or pipes.
- Verification: The shared validation commands below passed. Final Android rerun rebuilt the host harness with this change and passed all four selected cases.

### B-131 Loading an uncached local headless tab clears the terminal

- Status: `resolved`
- Area: `attach`, `terminal`
- Repro: Attach to two local headless sockets that have not sent snapshots; press Ctrl+L then n. The loading path emits a full-screen clear on the tab switch.
- Fix: Clear inherited contents only when presenting the first local view. Transfer the displayed framebuffer baseline when switching to a pending view and compose its loading banner without clearing the screen. This baseline does not become the new session's snapshot or sequence. Its first real snapshot replaces the retained contents and loading banner. Loading-frame selection/rendering shares the render lock so it cannot paint an obsolete placeholder after the first snapshot.
- Regression: `TestMultiAttachLocalHeadlessLoadingTabSwitchDoesNotClear` failed before the fix with `ESC[2J` and now checks repeated switches between new and existing pending views, including row-1 base/overlay phases. `TestMultiAttachLocalHeadlessPendingTabPreservesDisplayedFrame` checks a populated framebuffer, uses the existing permanent no-flicker assertion unchanged, and explicitly gates the first new snapshot to prove the eventual replacement. Existing startup-clear and first-snapshot tests remain unchanged and pass.
- Performance scope: No existing timing/allocation gate covers this loading path. Its PTY correctness gate forbids full-screen redraw and confirms that unchanged displayed contents survive while only the loading chrome changes; no latency improvement is claimed.

### B-132 FN soft input corrupts supplementary Unicode

- Status: `resolved`
- Area: `android`, `terminal-input`
- Repro: Enable FN mode and commit an emoji through the IME. UTF-16 halves are encoded separately, so the host receives replacement bytes. Soft CTRL/ALT input had the same problem.
- Fix: Dispatch consecutive FN literals as one string and flush them before shortcuts; preserve shortcut order and modifiers. Soft modifier encoding iterates complete code points, adding one Alt prefix per code point.
- Regression: The initial connected case `fn_soft_ime_preserves_supplementary_unicode` failed before the fix, and `softModifiersPreserveSupplementaryCodePoints` failed its pre-fix unit check. The expanded connected case `soft_ime_preserves_supplementary_unicode_in_fn_and_modifier_modes` passes and verifies actual host-received UTF-8 bytes for emoji, supplementary CJK, and combining text in FN, plus supplementary text under CTRL and ALT. Unit coverage also checks mixed literals/commands, modifier encoding, empty input, and one action for 4,096 consecutive literals.
- Performance scope: No existing timing/allocation gate covers FN input. The literal-run assertion proves that a 4,096-character input no longer creates 4,096 terminal send actions; no measured latency claim is made.

Validation for B-131 through B-133 (commands serialized):

- `GOMAXPROCS=2 GOFLAGS=-p=1 setsid --wait go test ./internal/attach -run '^TestMultiAttachLocalHeadlessLoadingTabSwitchDoesNotClear$' -count=1 </dev/null` failed before the fix (`tab-loading-before.log`).
- The same command with `-run 'TestMultiAttachLocalHeadless(LoadingTabSwitchDoesNotClear|ShowsLoadingWhileWaitingForSnapshot|ClearsLoadingAfterFirstSnapshot)$'` passed after the fix. The expanded selection `-run 'TestMultiAttachLocalHeadless|TestAttachRemoteTabSwitchRendersWithoutExtraInput'` passed (`tab-loading-after.log`). An initial populated-frame fixture had a nil cursor, which was corrected before its successful run.
- `GOMAXPROCS=2 GOFLAGS=-p=1 setsid --wait go test -race ./internal/attach -run 'TestMultiAttachLocalHeadless|TestAttachRemoteTabSwitchRendersWithoutExtraInput' -count=1 </dev/null` exposed B-133. After the descriptor fix, the same selection including `./internal/terminal -run 'TestWithFileDescriptor|TestMultiAttachLocalHeadless|TestAttachRemoteTabSwitchRendersWithoutExtraInput'` passed. The final expanded command `GOMAXPROCS=2 GOFLAGS=-p=1 setsid --wait go test -race ./internal/terminal ./internal/attach ./internal/session -run 'TestWithFileDescriptor|TestMultiAttachLocalHeadless|TestAttachRemoteTabSwitchRendersWithoutExtraInput|TestShutdownPTYReader|TestLocalSessionCancelWaitsForPTYReaderBeforeClose' -count=1 </dev/null` passed (`tab-loading-race-after.log`).
- Android Gradle commands used Java 21 and `--no-daemon --max-workers=2 -Dorg.gradle.parallel=false -Dkotlin.compiler.execution.strategy=in-process -Dorg.gradle.jvmargs='-Xmx1536m -Dfile.encoding=UTF-8'`. `:app:testDebugUnitTest --tests 'systems.pkt.lingon.ui.TerminalInputDispatchTest.softModifiersPreserveSupplementaryCodePoints'` failed before the fix (`unicode-unit-before.log`). Full `:app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed (`android-quality-second.log`).
- Connected runner: `JAVA_HOME=/usr/lib/jvm/java-21-openjdk-amd64 PATH=/usr/lib/jvm/java-21-openjdk-amd64/bin:$PATH GOMAXPROCS=2 GOFLAGS=-p=1 AVD_CPU_CORES=2 LINGON_IT_ONLY=fn_soft_ime_preserves_supplementary_unicode setsid --wait ./android/scripts/run-integration-tests.sh </dev/null` failed before the fix (`unicode-connected-before.log`). Afterward, `LINGON_IT_ONLY=soft_ime_preserves_supplementary_unicode_in_fn_and_modifier_modes,soft_ime_enter_respects_alt_modifier,fn_selection_copies_visible_cells_and_pastes_current_clipboard_in_another_session` passed all three (`unicode-connected-after.log`). The final rerun added `share_authentication_recovers_after_two_transient_failures` and passed all four after rebuilding the host harness (`unicode-connected-final.log`). Owned emulators, adb servers, harnesses, and single-use Gradle daemons were cleaned up.
- Full gates with `GOMAXPROCS=2 GOFLAGS=-p=1`: `setsid --wait go test ./... </dev/null`, `go vet ./...`, `golint ./...`, and `golangci-lint run ./...` passed (`go-quality-second.log`, `go-vet-second.log`, `golint-second.log`, `golangci-lint-second.log`); golangci-lint reported zero issues. Changed Go files passed `gofmt -l`; `git diff --check` was clean.
- Explicit changed-code-path review confirmed snapshot/sequence state is independent from the retained loading framebuffer, literals remain whole at UTF-8 conversion, and terminal controls run only while their file descriptor is live.

### B-129 Review: replay admission, session-list revocation, and share recovery

- Status: `resolved`
- Area: `relay`, `android`, `performance`
- Repro: Reconnect with more than 128 retained frames while the websocket writer cannot drain; revoke credentials while an account session-list websocket remains open; fail Android `/auth/share` transiently and restore connectivity.
- Findings: Healthy replay overflows the live-frame queue; sessionless list connections escape hub revocation; share authentication returns without retrying transient failures.
- Fix: FIFO writer-owned replay with at most one active and one queued replay per connection, without network delivery under the session replay lock. Live queue overflow still disconnects slow consumers. Sessionless account sockets are tracked for immediate credential revocation; registration revalidates authority to fence a revoked handshake, and each session-list update checks authority. Android retries network/408/429/5xx failures, preserves HTTP status and Retry-After, stops on invalid tokens, cancels obsolete token retries, and bounds exponential backoff during long outages.
- Regression evidence:
  - `TestWSReplayLargeBurstPreservesWelcomeReplayLiveOrder` failed before the fix with a healthy client closed before its blocked writer could drain. It now verifies welcome/replay/live FIFO order and delivery to another client while that writer is blocked.
  - `TestSessionListStopsPublishingAfterAuthorityRevocation` failed before the fix for credential reset, logout, and user deletion. `TestSessionListRevocationClosesOnlyTheRevokedUserAndFencesRegistration` checks immediate closure, unrelated users, late revoked handshakes, and cleanup after fresh authentication.
  - Android unit regressions failed before the fix for retry/backoff, Retry-After, and HTTP error metadata. Virtual-time tests also cover invalid tokens, obsolete token retries, and 40 attempts without backoff overflow.
  - `share_authentication_recovers_after_two_transient_failures` failed on the unfixed app with `conn=Disconnected` and HTTP 503 after the only attach request. It passes after the fix, consumes both injected failures, and receives actual terminal output without manual recovery.
- Performance evidence (local Linux amd64, `GOMAXPROCS=2`, five one-second samples):
  - `BenchmarkWSReplay/short`: median 0.932 ms before, 0.798 ms after (14.4% lower); both 2,017 allocations and approximately 136.6 KB per delivered replay.
  - `BenchmarkWSReplay/burst`: 299 small diff frames delivered in median 3.240 ms, 9,569 allocations, approximately 649.9 KB. The old implementation disconnects this burst instead of delivering it.
  - The FIFO envelope adds approximately 3 KiB of fixed queue storage per connection; it does not allocate a wrapper per live frame or concatenate replay into a serialized buffer. Replay remains capped by the existing retained-history policy, including its single oversized-frame allowance.
  - Existing 100-host reconnect gate passed before (0.423 s suite) and after (0.42 s case). The new concurrent 100-client check delivered all 29,900 replay frames in order in 97 ms. These are local delivery/capacity results, not a production soak.
- Validation commands completed:
  - `GOMAXPROCS=2 go test ./internal/relay -run '^TestWSReplayLargeBurstPreservesWelcomeReplayLiveOrder$' -count=1` (expected pre-fix failure).
  - `GOMAXPROCS=2 go test ./internal/relay -run 'TestWSReplayLargeBurst|TestWSConnSend|TestHub.*Replay' -count=1` and `-run 'TestWSReplay|TestWSConn'` passed.
  - `GOMAXPROCS=2 go test ./internal/relay -run '^TestSessionListStopsPublishingAfterAuthorityRevocation$' -count=1` (three expected pre-fix failures); `-run 'TestSessionListStops|TestWSClientAccountSessionList|Test.*Reload|Test.*User.*'` passed after the fix.
  - `GOMAXPROCS=2 go test ./internal/relay -run '^$' -bench '^BenchmarkWSReplay/short$' -benchmem -benchtime=1s -count=5` (baseline); `-bench '^BenchmarkWSReplay$'` passed after the fix. An initial post-change measurement exposed overlapping replay admission; separating the active and queued bounds corrected it before the successful measurement.
  - `LINGON_RELAY_CAPACITY_TEST=1 GOMAXPROCS=2 go test ./internal/relay -run '^TestRelayHundredHostReconnectBurst$' -count=1` (baseline); `-run '^TestRelayHundred(HostReconnect|ClientReplay)Burst$' -count=1 -v` passed after the fix.
  - `GOMAXPROCS=2 go test ./cmd/lingon-android-harness -run '^TestShareAuthFaultsAreFiniteAndIsolated$' -count=1` passed.
  - `GOMAXPROCS=2 go test -race ./internal/relay -run 'TestWSReplay|TestWSConn|TestSessionList|TestSessionNotifier' -count=1` passed.
  - Android commands used Java 21, `--no-daemon --max-workers=2 -Dorg.gradle.parallel=false -Dkotlin.compiler.execution.strategy=in-process -Dorg.gradle.jvmargs='-Xmx1536m -Dfile.encoding=UTF-8'`. `:app:testDebugUnitTest --tests 'systems.pkt.lingon.viewmodel.AppViewModelTest.shareAuthentication*' --tests 'systems.pkt.lingon.data.relay.RelayWebSocketClientTest.shareAuthentication*'` failed before the fix; full `:app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed afterward.
  - Isolated connected runner: `GOMAXPROCS=2 GOFLAGS=-p=1 AVD_CPU_CORES=2 LINGON_IT_ONLY=share_authentication_recovers_after_two_transient_failures setsid --wait ./android/scripts/run-integration-tests.sh </dev/null` failed before the fix; the same runner with `LINGON_IT_ONLY=share_authentication_recovers_after_two_transient_failures,share_tokens_are_isolated,share_token_dialog_rejects_invalid_token,background_foreground_resumes_input` passed afterward (four tests). Owned emulator, adb server, harness, and single-use Gradle daemon were cleaned up.
  - Full gates, serialized with `GOMAXPROCS=2 GOFLAGS=-p=1`: `setsid --wait go test ./... </dev/null`, `go vet ./...`, `golint ./...`, and `golangci-lint run ./...` passed; golangci-lint reported zero issues. `gofmt -l` on changed Go sources and `git diff --check` were clean. The initial full test run uncovered B-130, which was corrected before the passing run.
- Remaining gap: None for these repros. A production soak is outside the local performance evidence above.

### B-130 Relay-restart auth fixture blocks attach shutdown on undrained output

- Status: `resolved`
- Area: `verification`, `session`
- Repro: Run `GOMAXPROCS=2 GOFLAGS=-p=1 go test ./...`; `TestNo401OnExpiredAuthAfterRelayRestart` fails because attach does not exit after cancellation. A focused five-run check reproduced the failure three times before the fixture correction.
- Fix: Drain both fixture-owned output pipes into `io.Discard` and close/join the drain goroutines in cleanup. No inherited terminal is involved. The no-401 and shutdown assertions are unchanged.
- Verification: `GOMAXPROCS=2 GOFLAGS=-p=1 setsid --wait go test ./internal/session -run '^TestNo401OnExpiredAuthAfterRelayRestart$' -count=5 </dev/null` passed all five runs after the change. Full Go tests, vet, golint, and golangci-lint passed.

### B-128 Android integration fixtures interfere across test cases

- Status: `resolved`
- Area: `android`, `verification`
- Repro: In the full release sweep, accumulated harness hosts and notifications make later tests see extra tabs or stale notifications; a transient wall-status assertion can also miss its 250 ms display window. Headless tests attempt to click tabs that a lazy row has not composed.
- Fix: Track and stop hosts started by each test, wait for wall notifications to clear, observe the transient status as it is emitted, select headless fixtures directly, and wait for the terminal echo before dragging to select it.
- Regression coverage: The affected end-to-end cases failed in the full sweep before these fixture corrections and passed in the completed sweep afterward.
- Verification: `LINGON_IT_MEMORY_MAX=9G LINGON_IT_CPU_QUOTA_PERCENT=400 ./android/scripts/run-integration-tests.sh` passed with zero failures (one existing permission-dependent skip). `./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` and all Go quality gates passed.

### B-127 Repeated wall notifications trigger Android auto-group summary

- Status: `resolved`
- Area: `android`, `notifications`
- Repro: Repeated identical wall messages arrive with different event IDs; the app posts separate notifications, and Android creates a `ranker_group` summary that breaks the notification end-to-end assertion.
- Fix: Derive the notification identity from endpoint, sender, source session, and message content so repeated identical alerts update one entry while distinct content retains distinct entries.
- Regression coverage: `AndroidWallNotifierTest` assertions for repeated and distinct content failed before the identity change and passed afterward. The wall notification end-to-end cases passed in the full sweep.
- Verification: Android unit tests, androidTest Kotlin compilation, and the full Android integration sweep passed.

### B-126 Portrait status banner shifts the terminal viewport

- Status: `resolved`
- Area: `android`, `layout`
- Repro: Run `EndToEndTest.loaded_tall_session_uses_system_visible_bottom_before_and_after_keyboard_toggle` in the release integration sweep. The terminal top moves from y=323 to y=418 while its bottom stays fixed after a status banner appears.
- Regression coverage: The existing connected test fails before the fix and checks terminal and quick-key bounds through keyboard show/hide.
- Fix: Render the portrait status banner over the terminal panel so showing it does not consume layout height.
- Verification: The connected regression and full Android integration sweep passed; Android unit tests and androidTest Kotlin compilation passed.

### B-125 Zoomed pan test stops before the scrollback boundary

- Status: `resolved`
- Area: `android`, `terminal-pan`, `verification`
- Repro: Run `EndToEndTest.keyboard_visible_zoomed_pan_across_scrollback_boundary_is_continuous`. With a 35-row live top, the test caps 0.35-row drags at 60, traversing only 21 rows, so it never reaches scrollback.
- Regression coverage: The existing connected test fails before the correction and checks both boundary directions plus per-move continuity.
- Fix: Compute drag distance from the actual live-top row and keep enough movement samples to cross the boundary in both directions.
- Verification: The corrected connected regression and full Android integration sweep passed.

### B-124 Focused lifecycle resume jumps a cursor-followed camera to live bottom

- Status: `resolved`
- Area: `android`, `terminal-camera`, `lifecycle`
- Repro: Run `EndToEndTest.focused_background_resume_preserves_live_camera_when_cursor_still_fits`. Input places the cursor and camera at row 0 in a tall terminal, then background/resume restores the visible start at row 39.
- Fix: Enter manual camera mode only after an actual pan, preserve a top-aligned camera during viewport changes, and capture viewport state when the activity pauses so a later disposal cannot overwrite it. The connected fixture now explicitly positions a manual top camera.
- Regression coverage: The connected lifecycle test and the new `TerminalViewportPolicyTest` top-alignment case failed before the fix and passed afterward. Both assert camera position across viewport changes.
- Verification: Android unit tests, androidTest Kotlin compilation, the connected regression, and the full Android integration sweep passed.

### B-123 FN selection starts automatically and paste uses V

- Status: `resolved`
- Area: `android`, `terminal-input`, `terminal-selection`
- Report: FN+S is an extra step to select text, and FN+P is less familiar than paste with V.
- Repro: Enable FN with two ALT taps and drag over terminal text without pressing S; no selection is made. FN+V sends a literal V while FN+P pastes.
- Intended behavior: FN enables terminal selection immediately, keeps it active across terminal switches, and disables it when FN is turned off. FN+C copies the selected text and FN+V pastes the current Android clipboard. S and P become literal keys in FN mode.
- Fix: Selection mode now derives from the persistent FN button state and follows it across session changes. FN+C retains copy; FN+V reads the current Android clipboard; FN+S and FN+P send literal keys. The Android keyboard guide documents the new controls.
- Regression coverage: `TerminalInputDispatchTest` asserts V/paste and literal S/P mappings; `EndToEndTest.fn_selection_copies_visible_cells_and_pastes_current_clipboard_in_another_session` selects without S, copies, confirms FN-off clears selection, re-enters FN, switches terminals, pastes both copied and externally updated clipboard text with V, and selects/copies again in the destination terminal. The unit and connected regressions failed before the fix and passed afterward.
- Verification: The targeted connected Android test and full Android release sweep passed in the isolated emulator harness; `./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin`, `go test ./...`, `go vet ./...`, `golint ./...`, and `golangci-lint run ./...` passed.
- Remaining gap: None for this repro.

### B-122 Android sends input after control is lost

- Status: `resolved`
- Policy superseded (2026-10-04): B-134 records the approved transparent takeover on authorized typing. The historical blanket lease guard and its tests below no longer describe the intended behavior; view-only authorization restrictions remain required.
- Area: `android`, `terminal-input`, `session-control`
- Report: The full release instrumentation sweep exposed a case where a connected session has no control but the Android app still sends typed input to the host.
- Repro: Connect normally, set `hasControl=false` through the test seam, then type a character. The host echoes it because `sendRawInput` and `sendRawBytes` only gate view-only share tokens.
- Intended behavior: Both text and raw-byte input are blocked whenever the active session lacks control.
- Regression coverage: `EndToEndTest.headless_resize_action_disabled_and_input_remains_blocked_without_control` fails before the fix once the test closes Settings before sending input.
- Fix: `sendRawInput` and `sendRawBytes` now reject input whenever `hasControl` is false and report whether the cause is a view-only token or unavailable control.
- Verification: The Android connected regression passed after the fix; `AppViewModelTest.inputIsBlockedWithoutControlForTextAndRawBytes` covers both ViewModel entry points. The full Android unit suite passed.
- Remaining gap: None for this repro.

### B-121 Android soft keyboard drops ALT on Enter

- Status: `resolved`
- Area: `android`, `terminal-input`
- Report: Tapping the on-screen ALT control and then the soft keyboard Return key sends ordinary Enter.
- Repro: Focus the Android terminal input, tap ALT, then press Return through the IME. The host receives `0d` instead of the expected `1b 0d`.
- Intended behavior: ALT+Return sends ESC CR. Plain Return remains CR; the on-screen ALT modifier is consumed after Enter.
- Fix: IME Enter callbacks now use the modifier-aware text dispatch. That dispatch prefixes terminal CR with ESC when ALT is active; FN mode continues to send plain CR without consuming FN.
- Regression coverage: `TerminalInputDispatchTest` checks LF and CR with ALT, plain and CTRL Enter, and CTRL+ALT+Enter. `EndToEndTest.soft_ime_enter_respects_alt_modifier` drives both IME `sendKeyEvent(ENTER)` and `commitText("\n")`, checks `1b 0d` on the host, and confirms plain Enter after ALT is consumed. Both unit Enter regressions and the connected Android regression failed before the fix and passed afterward.
- Verification: `./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin`, the targeted connected Android test in the isolated emulator harness, `go test ./...`, `go vet ./...`, `golint ./...`, and `golangci-lint run ./...` passed. The IME callback, dispatch, and hardware Enter paths were reviewed.
- Remaining gap: None for this repro.

### B-120 Release install reused an older Android APK

- Status: `resolved`
- Area: `android`, `build`, `installation`
- Report: After installing the release, the phone does not show the new FN CTRL/ALT controls.
- Repro: Commit `68bb607` contains the FN controls, but `android/app/build/outputs/apk/release/output-metadata.json` still names `664dbf6`; `make android-install-release` installs that existing APK without building the current release.
- Intended behavior: A default release install builds the current release before installing it. An explicit `APK=` path installs that file without rebuilding.
- Fix: `android/Makefile` builds the release before a default `install-release`; `APK=` bypasses the build for an explicitly selected file. Root target and usage documentation reflect that behavior.
- Regression coverage: `TestRootAndroidInstallReleaseBuildsCurrentAPKByDefault` checks the build precedes install and verifies the explicit `APK=` path skips it. The default case failed before the fix and passes after. The existing selected-device/custom-APK regression still passes.
- Verification: `make android-install-release ADB_SERIAL=ZY22KSWBW2` assembled and signer-verified the release APK, then installed it successfully on the connected Motorola phone. Android package metadata reports `versionName=v0.5.0-alpha.1.0.20260925075645-68bb607e883b`, matching the FN controls commit. `./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin`, `go test ./...`, `go vet ./...`, `golint ./...`, and `golangci-lint run ./...` passed.
- Remaining gap: None for the stale APK repro.

### B-119 Soft FN mode cannot send CTRL or ALT function keys

- Status: `resolved`
- Area: `android`, `terminal-input`
- Report: CTRL is disabled while FN is active, so the soft keyboard cannot send CTRL+F1; FN also occupies the ALT button, leaving ALT+F1 unavailable.
- Repro: In the Android terminal, tap ALT twice to enter FN, then try CTRL followed by 1. CTRL is disabled and F1 is sent without CTRL. There is no ALT+F1 path through the on-screen controls.
- Intended behavior: While FN is active, the existing CTRL button cycles off → CTRL → ALT → C+A → off. The selected modifier stays active across function keys until changed or FN is turned off. SHFT remains independent, and FN+S/C/P keep their selection and clipboard meanings.
- Fix: In FN mode the existing CTRL key cycles off → CTRL → ALT → C+A → off; its state persists across F-key sends and resets when FN is turned off. SHFT still combines with each modifier state. Outside FN, CTRL and ALT retain their existing behavior.
- Regression coverage: `TerminalInputDispatchTest.fnModifierButtonCyclesThroughCtrlAltAndBoth` covers the state cycle; `fnFunctionKeysCombinePersistentModifierAndShiftStates` covers function-key encodings and FN+S/C/P commands. `EndToEndTest.fn_modifier_button_cycles_ctrl_alt_and_both_without_consuming_them` drives the on-screen keys and checks the host receives CTRL+F1/F2, ALT+F1/F2, C+A+F10, SHFT+C+A+F11, SHFT+F12, and plain F1 after resetting FN. The unit regression failed to compile before implementation, and the connected regression failed on the disabled FN CTRL key before implementation; both pass after the fix.
- Verification: `./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed. The new connected test plus the existing FN layout and cross-session selection/clipboard connected tests passed in the isolated emulator harness. `go test ./...`, `go vet ./...`, `golint ./...`, and `golangci-lint run ./...` passed. The changed UI and dispatch paths were reviewed.
- Remaining gap: None for this repro; the full Android integration sweep passed.

### B-118 Root Makefile lacked a release APK install target for phones

- Status: `resolved`
- Area: `android`, `build`, `installation`
- Report: Running `make android-install` does not install the app on the phone.
- Repro: From the repository root, `make android-install` delegates to Gradle `:app:installDebug`, while there was no root target to install the release-signed APK on the phone. `go test ./internal/buildscripts -run '^TestRootAndroidInstallReleaseUsesSelectedPhoneAndReleaseAPK$' -count=1` failed before the fix with `No rule to make target 'android-install-release'`.
- Intended behavior: Keep `android-install` for debug installs; provide `install-release` under `android/` and `android-install-release` at the root so a release APK can be installed on a phone, with `APK` and `ADB_SERIAL` overrides. Remove the transport-named `adb-install` alias.
- Fix: The root target delegates to the Android `install-release` target, which installs the prebuilt release APK on one selected device. The debug target still uses Gradle `:app:installDebug`. Help and README use the canonical names; the old `adb-install` alias was removed.
- Regression coverage: `TestRootAndroidInstallReleaseUsesSelectedPhoneAndReleaseAPK` runs the root target against a fake adb with two connected devices and checks that `ADB_SERIAL` selects the phone and `APK` selects the release file. It failed before the new target and passes afterward.
- Verification: The focused test and `go test ./...`, `go vet ./...`, `golint ./...`, `golangci-lint run ./...`, `./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin`, and `git diff --check` pass. `make -n android-install` still resolves to `:app:installDebug`. The existing release APK's signer matches `android/lingon-release-cert.pem`. The root `make android-install-release ADB_SERIAL=<phone>` completed with `Performing Streamed Install` and `Success` on the connected Motorola phone; `adb shell pm path systems.pkt.lingon` confirmed the installed package. The temporary isolated adb server was stopped; the preexisting server was left running.
- Remaining gap: None for this repro.

### B-117 Android soft-keyboard Shift navigation gesture

- Status: `resolved`
- Area: `android`, `terminal-input`
- Report: The soft keyboard's Shift state is not available when tapping Lingon's separate navigation keys. Use a two-second hold on arrows, Home, End, PgUp, and PgDn to send Shift plus the key, combined with active ALT/CTRL. A short tap retains the current behavior. No extra Shift button is wanted.
- Repro: Open the Android terminal and soft keyboard, hold ↑ for two seconds, and inspect the bytes received by the host; current behavior sends an unshifted arrow. Repeat with ALT, CTRL, and PgUp/PgDn.
- Intended behavior: Short taps send the current key with active modifiers; holding a navigation key for two seconds sends its Shift-modified terminal sequence exactly once. FN mode supplies its own persistent SHFT toggle for function keys.
- Follow-up: While FN is active, the portrait quick-key PGUP and PGDN positions show F11 and F12 and send those function keys on tap. The `/` position becomes a persistent SHFT toggle for FN+1–0 and F11/F12. FN+@/# are literal again. Leaving FN restores `/`, PGUP, and PGDN and clears SHFT.
- Regression coverage:
  - `TerminalInputDispatchTest.quickNavigationKeysEncodeShiftAndActiveModifiers` checks all arrows, Home, End, PgUp, and PgDn with Shift and ALT/CTRL combinations. It failed to compile before the encoder was added and passes after the change.
  - `TerminalInputDispatchTest.fnNumberKeysEncodeF1ThroughF10AndFnShiftEncodesModifiedFunctionKeys` checks F1–F10, shifted F1/F10/F11/F12, and literal FN+@/#. It failed to compile before shifted function-key support and passes after the change.
  - `EndToEndTest.touch_navigation_long_press_sends_shift_with_active_modifiers` checks the actual on-screen gesture below and above the two-second threshold, host bytes, ALT/CTRL combinations, Home/End/PgUp/PgDn, and single-send behavior.
  - `EndToEndTest.fn_mode_replaces_page_keys_with_f11_and_f12` checks the actual FN layout, F11/F12 host bytes, persistent SHFT, shifted F1/F11, SHFT toggle off, and reset on leaving FN. It failed on the missing F11 key before the UI change and passes after it.
- Verification: `./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed; the two new targeted Android tests and existing `touch_modifiers_forward_all_arrows_and_fn_keys_stay_active` passed in the isolated emulator harness. The initial batch revealed that counting echo lines was unreliable when the fixed terminal viewport scrolled; assertions now check the newest echoed bytes in order, and the focused rerun passed. `go test ./...`, `go vet ./...`, `golint ./...`, and `golangci-lint run ./...` passed.
- Remaining gap: The compact landscape quick-key layout does not expose the `/` or PGUP/PGDN positions, so its FN UI does not expose SHFT/F11/F12. Shift+FN numbers in that layout require a separate UI decision.

### B-116 Android FN input, terminal selection, and clipboard

- Status: `resolved`
- Area: `android`, `terminal-input`, `terminal-rendering`
- Report: Modified arrow chords are still reported as missing in the Android app. Add a rotating ALT/FN control, persistent FN function-key shortcuts, terminal text selection, and copy/paste across sessions through the current Android clipboard.
- Repro: Use each on-screen ALT/Ctrl arrow combination; cycle ALT into FN and type `1` through `0`, `@`, `#`; use FN+S, drag over text, FN+C, switch sessions, and FN+P.
- Intended behavior: ALT cycles off → ALT → FN → off; FN stays active after a key and excludes Ctrl. In selection mode one finger selects and two fingers pan, while pinch zoom works. FN+C replaces the clipboard text; FN+P reads the current Android clipboard. Selection is drawn with a white background.
- Regression coverage:
  - `TerminalInputDispatchTest` covers all four arrows with Alt, Ctrl, and Ctrl+Alt; ALT → FN cycling; FN commands; F1–F12 touch encodings; and physical Ctrl+Alt+F-key encodings. The first new test run failed to compile because the FN API did not exist; the physical F-key test then failed before its implementation. Both pass after the changes.
  - `TerminalSelectionTest` covers forward and reverse multi-row selection, Unicode continuation cells, and concealed text. The concealed-cell test failed before the copy path was corrected.
  - `TerminalGridViewInstrumentedTest.selectionDragCopiesCellsWithWhiteHighlightAndTwoFingerPanPreservesSelection` drives touch events and checks copied text, white highlight, and two-finger pan.
  - `EndToEndTest.touch_modifiers_forward_all_arrows_and_fn_keys_stay_active` and `fn_selection_copies_visible_cells_and_pastes_current_clipboard_in_another_session` drive the Android UI against harness hosts. The existing physical modified-arrow end-to-end test remains in place.
- Verification:
  - `./gradlew :app:testDebugUnitTest` and `:app:compileDebugAndroidTestKotlin` pass. Previously failing ViewModel tests pass after giving share authentication a test dispatcher, making the fake repository reflect endpoint writes, using a distinct fake socket per connection, and keeping the DataStore scope inside the test lifetime. The `HttpClientProviderTest` DataStore scheduler deadlock was also fixed in test setup.
  - Targeted connected runs pass for physical modified arrows; on-screen Alt, Ctrl, and Ctrl+Alt arrows and persistent FN function keys; cross-session selection/copy/paste using the current Android clipboard; and the grid's white highlight, one-finger selection, and two-finger pan. The first connected run failed because the isolated host echo PTY buffered input; after putting that harness-owned PTY in raw mode, the modified-key run exposed missing modifier metadata in test-injected keys. Both failures were corrected and all four cases pass.
  - The integration harness now stores AVD data and emulator preferences under ignored `android/build/integration-emulator/`, overrides Android home paths, and uses an isolated adb server port. The final harness path passed a connected grid smoke test.
  - `go test ./...`, `go vet ./...`, `golint ./...`, `golangci-lint run ./...`, shell syntax checks, and `git diff --check` pass.
- Remaining gap: None for this repro. A full Android integration sweep remains the release gate under repository policy.

### B-115 Relay reconnect storm times out host pings with a dozen sessions

- Status: `needs_verification`
- Area: `relay`, `publisher`, `performance`, `reconnect`
- Summary: Roughly a dozen remote sessions repeatedly reconnect; the relay runs hot and hosts close with `ping timeout` soon after registration.
- Report: At least 50–100 concurrent sessions must remain usable on a two-core relay.
- Repro: Reconnect a dozen or more hosts using one account while watching relay CPU and `relay.ws.read.failed` logs; hosts register and then disconnect with a host-originated ping timeout.
- Investigation:
  - Each websocket handshake persisted token metadata even when unchanged, and each host registration persisted the full store.
  - Every registration/disconnect woke every session-list websocket, each of which rebuilt and sent the full list. A 100-host reconnect burst could therefore trigger up to 100 list pushes per subscriber. The publisher's two-second ping timeout made the burst prone to false disconnects under scheduling pressure.
  - On `gertrude3` on 2026-09-22, the running relay used about 93% CPU with 12 active sessions and a 25,696,473-byte `state.json`. It contained 62,456 access tokens; 62,451 were expired and only five were current. Every host registration and disconnect serialized and rewrote that file. The host WebSocket reader started only after this synchronous save, so the relay could not answer host pings while persistence was busy.
- Fix:
  - Coalesce session-list notifications per user over 100 ms. New subscribers still receive an immediate current list; a pending timer is canceled when the last subscriber leaves.
  - Persist host/client token classification only when access or refresh token metadata changes.
  - Increase default publisher heartbeat interval from 2 to 5 seconds and pong timeout from 2 to 10 seconds. Environment overrides remain available.
  - Prune expired access tokens from the in-memory store and persisted JSON during every save. Keep current access tokens and refresh tokens.
  - Start the host WebSocket read loop immediately after registration, before state persistence, so pings can be answered during a slow save. Preserve unregister and close behavior when persistence fails.
- Regression coverage:
  - `TestSessionNotifierCoalescesReconnectBurst` uses a mock clock to prove 100 changes yield one notification for each of 100 subscribers. It failed before the fix because notifications arrived immediately; it passes after the fix. `TestSessionNotifierKeepsUsersSeparate` checks account isolation.
  - `TestMarkTokenClientTypeSkipsUnchangedMetadata` checks reconnects do not trigger redundant token persistence.
  - `TestStoreSavePrunesExpiredAccessTokens` uses a mock clock and checks expired tokens are removed from memory and disk while current access and refresh tokens survive. It failed before the fix and passed afterward.
  - `TestHostPingAnsweredWhileRegistrationPersists` blocks a registration save and requires a live host ping to receive a pong before the save can finish. It failed before the fix with a ping timeout and passed afterward.
  - Opt-in `TestRelayHundredHostReconnectBurst` reloads 100 persisted sessions and distinct host tokens alongside 62,451 expired tokens, reconnects 100 WebSockets concurrently, checks all 100 are active together, and checks the saved JSON contains only the 100 current access tokens. It uses no PTY or inherited terminal.
- Verification:
  - The opt-in 100-host check passed with `GOMAXPROCS=2` and also under `go test -race` after seeding the expired-token backlog (0.36 s and 4.38 s, respectively).
  - `go test -race ./internal/relay -run '^TestSessionNotifier' -count=1` passed.
  - `go test -race ./internal/relay -run '^(TestHostPingAnsweredWhileRegistrationPersists|TestWSHostPersistFailureClosesReplacedHost|TestStoreSavePrunesExpiredAccessTokens)$' -count=3` passed.
  - `go test ./internal/relay -count=3` passed after correcting the ping regression's test-server cleanup ordering.
  - `go test ./internal/session -run '^TestNo401OnExpiredAuthAfterRelayRestart$' -count=1` passed after pruning.
  - `go test ./...`, `go vet ./...`, `golint ./...`, and `golangci-lint run ./...` passed after the follow-up fix.
  - The changed notifier, persistence, publisher heartbeat, and relay handler paths were reviewed after implementation. A pre-existing relay logger nil check was removed to clear staticcheck, and the pre-existing relay-restart integration test now injects zero reconnect jitter; it passed three consecutive runs.
- Remaining gap: Deploy the fix and observe the actual two-core relay with real publishers and terminal replay. Verify that `state.json` shrinks, CPU settles, and hosts remain connected without repeated `ping timeout` cycles. A sustained 100-session production soak is still needed to establish CPU headroom.
- Follow-up report (2026-09-22 22:26 UTC): Hosts including `psl`, `cpkt`, and `omm` cycled through register, connect, immediate `ping timeout`, and disconnect. Attach clients came online quickly, but new sessions remained unstable. Logs showed 7–15 seconds between `relay.hub.host.register` and `relay.host.connect.done`; the prior 100-host test only checked registration and missed this heartbeat window.

### B-114 Android modifier chords and Alt+Arrow input are not consistently forwarded

- Status: `resolved`
- Area: `android`, `terminal-input`, `attach`, `pty`
- Summary: Android terminal input does not consistently encode Ctrl+Alt chords
  or modified navigation keys. In particular, on-screen modifier toggles bypass
  the cursor-key encoding path for quick-key arrows, and physical Alt+Up/Down can be
  dropped before it reaches the terminal. Attach scrollback also does not
  recognize standard modified CSI arrow sequences.
- Report:
  Ctrl and Alt should be independently selectable in the Android terminal,
  then apply together to the next key regardless of letter case. Alt+Up/Down
  must work from Android and Lingon attach.
- Desired behavior:
  Android must send conventional terminal encodings for Ctrl, Alt, and
  Ctrl+Alt character chords and modified navigation keys. Attach must accept
  Alt-modified Up/Down in its scrollback input mode without regressing normal
  remote input forwarding.
- Repro:
  1. In Android, toggle Alt then use the on-screen Up or Down quick key, or
     press Alt+Up/Down on a physical keyboard.
  2. Observe that the quick key produces a malformed escape sequence and the
     physical key can be ignored.
  3. In attach scrollback, send CSI `1;3A` or `1;3B` and observe that it is
     not treated as an Up/Down navigation command.
- Investigation:
  - Android's quick keys pass cursor escape sequences through the ordinary
    character modifier encoder. With Alt selected, that encoder prepended ESC
    to every byte of `ESC [ A/B`, rather than emitting a single modified cursor
    sequence. Physical modified cursor events had no Unicode character, then
    fell through the letter-only key-code fallback and were dropped.
  - Attach's normal relay input already forwards modified bytes unchanged. Its
    scrollback parser, however, accepts only unmodified CSI `A/B` finals and
    discarded `CSI 1;3A/B`, the conventional Alt+Up/Down encoding.
- Regression coverage:
  - `TerminalInputDispatchTest` verifies uppercase Ctrl+Alt letters, Android
    Alt/Ctrl+Alt/Ctrl+Alt+Shift cursor encodings, and preserves modified CSI
    sequences in application-cursor mode.
  - `EndToEndTest.keyboard_input_forwards_ctrl_alt_and_modified_arrows` drives
    actual Android modifier key-down/up events and checks the host receives
    Alt+Up (`CSI 1;3A`), Ctrl+Alt+Shift+Down (`CSI 1;8B`), and Ctrl+Alt+Shift+D
    (`ESC 0x04`).
  - `TestAttachScrollbackModifiedArrowKeysMoveImmediately` is an isolated PTY
    integration test that drives `CSI 1;3A/B` through attach scrollback and
    asserts observable viewport movement. Unit coverage keeps host and attach
    scrollback parsers aligned for Alt and Ctrl+Alt+Shift arrows.
- Verification:
  - `./gradlew :app:testDebugUnitTest --tests
    systems.pkt.lingon.ui.TerminalInputDispatchTest` passed (16 tests).
  - `./gradlew :app:testDebugUnitTest` completed without failing test-result
    XML reports.
  - `./gradlew :app:compileDebugAndroidTestKotlin` passed.
  - The targeted connected test compiled and packaged, but could not run: no
    Android device/emulator was connected. The adb server that Gradle started
    for device discovery was stopped afterward.
  - `go test -count=1 ./internal/attach ./internal/session -run
    TestScrollbackInputMappings` passed.
  - `go test -count=1 -tags integration ./integration/pty/attach -run
    TestAttachScrollbackModifiedArrowKeysMoveImmediately` passed.
  - `go test ./...` reached an unrelated failure in pre-existing
    `internal/session.TestNo401OnExpiredAuthAfterRelayRestart` (`timeout
    waiting for condition`), matching the known B-113 suite instability. No
    input-related test failed.
- Follow-up verification (2026-09-24): The targeted connected
  `keyboard_input_forwards_ctrl_alt_and_modified_arrows` test passes on the
  isolated emulator, proving the host receives Alt+Up, Ctrl+Alt+Shift+Down,
  and Ctrl+Alt+Shift+D. The full Android unit and Go standard checks pass.
  The Android test injector now sets modifier metadata on the key events.

### B-113 Session-list polling floods production logs at info level

- Status: `needs_verification`
- Area: `server`, `logging`, `relay`
- Summary: Successful session-list polling requests are emitted as info-level
  HTTP access logs, creating a high-volume production log stream.
- Report:
  Normal session polling produces repeated successful access events at info
  level.
- Desired behavior:
  All successful HTTP access requests must emit trace-level logs. Client errors
  must remain warn-level logs and server errors must remain error-level logs.
- Repro:
  1. Poll a relay session list.
  2. Observe a successful access event is emitted at info level for every poll.
- Regression coverage:
  - `TestAccessLogUsesResponseSeverity` directly records the middleware's
    logger calls and asserts 200 → trace, 400 → warn, and 500 → error.
- Verification:
  - `go test ./internal/server -run
    'TestAccessLog(EmitsStatusAndIP|UsesResponseSeverity)' -count=1` passed.
  - `go test ./...` initially passed before the unrelated relay lint cleanup.
    After that cleanup was reverted, the full suite twice failed in the
    pre-existing `internal/session` integration test
    `TestNo401OnExpiredAuthAfterRelayRestart` with `timeout waiting for
    condition`, including one isolated rerun.
  - `go vet ./...` and `golint ./...` passed.
  - `golangci-lint run ./...` is blocked by pre-existing staticcheck findings
    in `internal/relay/http.go` (SA4023); no unrelated relay change is included
    in this access-log fix.
  - Follow-up during B-115: removed the unreachable relay logger nil check and
    made the relay-restart test's host and attach jitter deterministic. The
    targeted restart test passed three consecutive runs; `go test ./...` and
    `golangci-lint run ./...` now pass.

### B-112 Attach alt-screen exit can leak prompt background across its restored line

- Status: `resolved`
- Area: `attach`, `render`, `terminal`, `pty`
- Summary: After an attached application leaves the alternate screen with its
  cursor at the lower-right corner, the attach display can retain the blue
  background used by the first three cells of the shell prompt across the
  restored line.
- Report:
  The engineer observes this only through Lingon attach; the same command in a
  direct xterm or SSH terminal restores the prompt background correctly.
- Desired behavior:
  Leaving an application alternate screen must not transfer delayed-wrap state
  to the restored primary screen. Prompt-specific background styling must
  remain confined to its source cells.
- Repro:
  1. Use a shell prompt whose first three cells have a blue background.
  2. Attach to the session and start an alternate-screen application.
  3. Exit while the application cursor is at the lower-right corner.
  4. Observe the restored prompt line must have the default background beyond
     its styled cells, not the prompt's blue background.
- Investigation:
  - The emulator keeps delayed-wrap state globally while switching between
    primary and alternate screens. A final printable cell at the alternate
    screen's lower-right corner leaves that state set. The first primary-screen
    prompt character then triggers an unintended scroll with the prompt's
    active background color, filling the restored line.
- Regression coverage:
  - `TestAltScreenSwitchClearsDelayedWrap` asserts that a bottom-right
    alternate-screen write cannot scroll or recolor the restored primary
    screen.
  - `TestAttachAltScreenExitDoesNotLeakPromptBackgroundToRestoredPromptLine`
    is an isolated PTY integration regression that verifies the visible attach
    terminal keeps prompt background styling limited to its first three cells.
- Verification:
  - The attach PTY regression failed before the fix with the restored prompt
    line background `0x1000004` (blue), then passed after the alternate-screen
    transition began clearing delayed-wrap state.
  - `go test ./internal/terminal/emu -run TestAltScreenSwitchClearsDelayedWrap -count=1` passed.
  - `go test -tags integration ./integration/pty/attach -run TestAttachAltScreenExitDoesNotLeakPromptBackgroundToRestoredCursorCell -count=1` passed.
  - `go test ./...`, `go vet ./...`, and `golint ./...` passed.
  - `golangci-lint run ./...` is blocked by the installed linter binary: it
    was built with Go 1.26 while this repository requires Go 1.27.

### B-111 Relay restart causes excessive reconnect replay load with many hosts

- Status: `resolved`
- Area: `relay`, `publisher`, `session`, `scrollback`, `performance`
- Summary: Restarting a relay with roughly 15–20 active hosted sessions causes an excessive CPU spike while hosts reconnect and replay terminal state.
- Report:
  The engineer observes huge CPU load during relay restart when many sessions reconnect. The current defaults may retain and replay too much terminal history per session.
- Desired behavior:
  Relay restart and reconnect must have bounded, predictable CPU and memory use for 15–20 active sessions, without losing the current terminal state required to recover an attached client.
- Repro:
  1. Run 15–20 Lingon hosts that have accumulated normal terminal scrollback.
  2. Restart the relay.
  3. Observe all hosts reconnect and resend recovery state.
  4. Measure relay and host CPU, allocation rate, and resident memory until recovery completes.
- Investigation:
  - The default `terminal.scrollback_lines` is 5,000. A normal local host retains that data twice: once in the terminal emulator and once in `localSession`; both stores deep-copy every row and cell.
  - On a client reconnect after relay history is lost, the relay forwards the client's hello to its host. `Publisher.readWS` responds by deep-copying all retained scrollback, converting every row to protobuf, building all 100-row frames, and sending them synchronously before the current snapshot.
  - At 80 columns, one 5,000-row `terminal.Cell` store is approximately 12.2 MiB (32 bytes per cell). The two retained copies are therefore approximately 24.4 MiB per host, or approximately 488 MiB across 20 hosts, before snapshot, protobuf, websocket, and GC-transient allocations.
  - The relay clones and sizes each recovery frame while recording its 512 KiB per-session replay history. That cap bounds retained relay history to roughly 10 MiB for 20 sessions, but it does not bound the one-time decode/clone/encode allocation work of a 5,000-row resend.
  - Publisher reconnect backoff starts at a fixed one second with no jitter, so hosts and clients that lost the same relay restart retry in lockstep. Each host handshake also serializes and atomically rewrites the full relay `state.json`; each connection notifies every session-list websocket, which recomputes and sends the active-session list.
- Proposed remediation (pending approval):
  - Lowered the default scrollback cap to 1,337 lines while retaining the existing config and `--scrollback-lines` override for larger histories.
  - Added a cryptographically random, bounded 0–10 second reconnect jitter for publisher hosts and multi-attach clients, so recovery work is spread rather than synchronized. Direct/single attach remains unchanged because it has no reconnect loop.
  - Explicitly excluded: a 20-host restart regression, because it would impose unsuitable load on the development machine.
  - Follow-up: profile a production-shaped restart using the existing opt-in `pprof` build before deciding whether to batch persistence/session-list notifications or change the recovery protocol to request scrollback only when needed.
- Regression coverage:
  - `TestPolicyWithJitterUsesBoundedSample` and `TestPolicyWithJitterClampsSample` verify the additive jitter range and defensive bounds.
  - `TestPublisherReconnectDelayAddsConfiguredJitter` and `TestReconnectDelayAddsConfiguredJitter` verify publisher and multi-attach retry paths apply their policy jitter.
  - The isolated PTY test harness injects zero jitter so existing terminal integration regressions remain deterministic; production code uses crypto-secure randomness.
- Verification:
  - `go test ./internal/backoff ./internal/publisher ./internal/attach -count=1` passed.
  - `go test ./internal/config ./internal/session -count=1` passed.
  - `go test ./...` passed.
  - `go vet ./...` passed.
  - `golangci-lint run ./...` passed.
  - `golint ./...` is unavailable in this environment; equivalent `go run golang.org/x/lint/golint@latest ./...` passed.

### B-110 Detached headless startup reports ready before its PTY launches

- Status: `resolved`
- Area: `headless`, `startup`, `session`
- Summary: The headless daemon signals detached-startup readiness after binding its local socket but before its initial local terminal runner has launched a shell.
- Report:
  Review found that an invalid shell can make the initial local runner fail immediately after the detached parent has already received a successful `ready` status and printed its background-start message.
- Desired behavior:
  Detached startup reports ready only after the initial local PTY has launched. If runner initialization fails, startup returns the error and never signals ready.
- Repro:
  1. Start a headless daemon with an invalid shell path.
  2. Observe the runner fail during its initial local-session start.
  3. Verify no startup-ready callback or detached success status is emitted.
- Regression coverage:
  - `TestDaemonDoesNotReportReadyWhenInitialShellFails`.
- Verification:
  - Investigation confirmed `headlessd.Daemon.Run` starts `runner.Run` in a goroutine and calls `OnStartupReady` before the runner's initial local session invokes `startShell`.
  - Added an initial-PTY readiness signal from the session runner. The daemon waits for it before serving its local socket or reporting detached startup readiness, and propagates startup errors instead.
  - The regression failed before the fix because the daemon returned `nil` after `start shell: fork/exec ... no such file or directory`; it now passes.
  - `go test ./internal/headlessd -run TestDaemonDoesNotReportReadyWhenInitialShellFails -count=1` passed.
  - `go test ./internal/session ./internal/headlessd -count=1` initially exposed an unrelated `TestRemoteInputSurvivesStaleSessionsList` timing failure; rerunning that exact test passed, and the full suite passed afterward.
  - `go test ./...` passed.
  - `go vet ./...` passed.
  - `golangci-lint run ./...` passed.
  - `golint ./...` is unavailable in this environment; equivalent `go run golang.org/x/lint/golint@latest ./...` passed.

### B-109 Android certificate UI loses canonical endpoint entries

- Status: `needs_verification`
- Area: `android`, `certificates`, `settings`
- Summary: The trusted-certificate UI indexes certificate-flow entries with a raw selected endpoint while certificate storage uses canonical HTTP URL keys.
- Report:
  Review found that `https://relay.example` is persisted as `https://relay.example/`, but the ViewModel later looks up the flow with the selected raw endpoint. A certificate add or removal can therefore make the UI display an empty trusted-certificate list while the certificate remains stored and active for HTTP clients.
- Desired behavior:
  Certificate storage and all flow consumers must use the same canonical endpoint key, while legacy raw-key entries remain visible.
- Repro:
  1. Select endpoint `https://relay.example`.
  2. Add a trusted certificate.
  3. Observe the persisted/flow key is `https://relay.example/`.
  4. Refresh the certificate state and observe the settings UI must still show the certificate.
- Regression coverage:
  - `AppViewModelTest.certificateFlowUsesNormalizedSelectedEndpointKey`.
  - `CertificateStoreTest.listFindsLegacyRawEndpointKeyThroughNormalizedLookup`.
- Verification:
  - Added shared certificate endpoint-key normalization for storage and the ViewModel lookup. Certificate-flow entries are normalized and legacy/raw endpoint entries are merged by certificate ID.
  - Review found that storage lookups still decoded raw legacy keys. `loadState` now normalizes that decoded map before list/add/remove operations, with `CertificateStoreTest.listFindsLegacyRawEndpointKeyThroughNormalizedLookup` covering the normalized lookup of a legacy no-slash entry.
  - Targeted Android unit tests for both the ViewModel and legacy-store regressions were attempted before and after the fixes, but cannot start because `JAVA_HOME` is unset and `java` is not on `PATH`.
  - `./gradlew :app:compileDebugAndroidTestKotlin` was also blocked by the missing JDK before compilation began.
  - `go test ./...` passed.
  - `go vet ./...` passed.
  - `golangci-lint run ./...` passed.
  - `golint ./...` is unavailable in this environment; equivalent `go run golang.org/x/lint/golint@latest ./...` passed.
  - Remaining gap: Android unit and instrumentation-test compilation must run in an environment with a JDK.

### B-108 Attach clients require a second Escape keystroke

- Status: `resolved`
- Area: `attach`, `input`, `pty`
- Summary: A standalone Escape entered through either attach UI is retained by the normal-input mouse-report filter until another byte arrives.
- Report:
  The engineer reports that Lingon multi-attach, and likely single attach/client mode, require Escape to be pressed twice before applications receive Escape. This differs from normal SSH/xterm terminal behavior and prevents a single Escape from working in interactive applications.
- Desired behavior:
  A single Escape typed through both single attach and multi-attach must reach the remote application as one `0x1b` byte without waiting for a subsequent keystroke. Attach scrollback mouse reports must continue to work.
- Repro:
  1. Start a Lingon host running a shell.
  2. Attach with either `lingon attach` or multi-attach/client mode.
  3. Run a noncanonical one-byte read in the remote shell.
  4. Press Escape once.
  5. Observe that the read remains blocked until a second key is pressed.
- Regression coverage:
  - `TestAttachSingleEscKeyPassthrough`
  - `TestAttachMultiEscKeyPassthrough`
- Verification:
  - Investigation found `mouseReportFilter` buffers every `ESC` in normal attach input while it waits to distinguish `ESC [ <...M` mouse reports. Mouse reporting is now scoped to attach scrollback, where input takes the dedicated scrollback parser, so the normal-input filter is stale and causes the retained-Escape behavior.
  - Removed the stale normal-input filter from both attach input loops; scrollback continues to parse its own mouse reports.
  - The new regressions failed before the change with `waiting for ESC_OK` for both attach modes, then passed after the change.
  - `go test -tags integration ./integration/pty/attach -run 'TestAttach(Single|Multi)EscKeyPassthrough|Test(Attach|MultiAttach)ScrollbackScopesMouseReporting' -count=1` passed.
  - `go test ./...` passed.
  - `go vet ./...` passed.
  - `golangci-lint run ./...` passed.
  - `golint ./...` is unavailable in this environment; equivalent `go run golang.org/x/lint/golint@latest ./...` passed.

### B-107 Browser share websocket cannot set share-session header

- Status: `resolved`
- Area: `relay`, `websocket`, `auth`, `share`, `webui`
- Summary: Browser share-token websocket attach fails when an account cookie is also present.
- Report:
  Review found that the fix for lingering share cookies required `X-Lingon-Share-Session` to consume a share-session cookie, but browser `WebSocket` cannot set custom headers. The Web UI share flow sends the share session only as a cookie and sends an empty `SessionId` hello for share views.
- Desired behavior:
  Browser Web UI share attaches should be able to use the share-session cookie even when a valid account cookie is present. Generic account websocket listing should still ignore lingering share cookies.
- Repro:
  1. Authenticate a browser account and keep the account cookie.
  2. Attach with a share token so `/auth/share` sets `bifrons_share_session`.
  3. Open browser `/ws/client` without custom headers and send a Web UI share hello: empty `SessionId`, empty `ClientId`, `ClientType=web`.
  4. Observe the websocket should attach to the shared terminal instead of taking the account session-list path.
- Regression coverage:
  - `TestWSClientConnectsWithShareSessionCookie`
  - `TestWSClientAccountSessionListIgnoresLingeringShareCookie`
- Verification:
  - Fixed by allowing a valid share-session cookie to override account auth only for the Web UI share hello shape: empty `SessionId`, empty `ClientId`, and `ClientType=web`.
  - Preserved generic account websocket listing by keeping non-share no-session hellos on the account session-list path.
  - `go test ./internal/relay -run 'TestWSClient(AccountSessionListIgnoresLingeringShareCookie|ConnectsWithShareSessionCookie|StaleShareCookieDoesNotOverrideAccountAuth)' -count=1` passed.
  - `go test ./internal/relay -count=1` passed.
  - `go test ./...` passed.
  - `go vet ./...` passed.
  - `golangci-lint run ./...` passed.
  - `go run golang.org/x/lint/golint@latest ./...` passed.

### B-106 Lingering share cookie overrides authenticated websocket listing

- Status: `resolved`
- Area: `relay`, `websocket`, `auth`, `share`
- Summary: A valid account-authenticated `/ws/client` listing connection can be silently switched to share-cookie auth.
- Report:
  Review found that after a websocket hello without `SessionId`, `handleWSClient` consumed a `bifrons_share_session` cookie even when normal user auth had already succeeded. That lets a stale share cookie override a valid account websocket session-list request and reconnect the client to a previous share.
- Desired behavior:
  Share cookies should be consumed only when the request explicitly asks for share-session auth, or when normal auth fails. A normal authenticated websocket without `SessionId` should remain account-authenticated and receive session-list frames.
- Repro:
  1. Authenticate as a normal user and keep a valid access cookie.
  2. Also keep a valid `bifrons_share_session` cookie from a previous share.
  3. Open `/ws/client` without `X-Lingon-Share-Session` and send hello without `SessionId`.
  4. Observe the connection should receive account session-list frames, not a share-session welcome.
- Regression coverage:
  - `TestWSClientAccountSessionListIgnoresLingeringShareCookie`
- Verification:
  - Fixed by removing the post-hello implicit share-cookie override for successfully account-authenticated websocket connections.
  - Preserved explicit share-cookie websocket attach by requiring `X-Lingon-Share-Session` when a share-session cookie should be consumed.
  - Added account session-list handling for authenticated websocket hellos without `SessionId`, so lingering share cookies do not turn the listing connection into a share attach.
  - `go test ./internal/relay -run 'TestWSClient(AccountSessionListIgnoresLingeringShareCookie|ConnectsWithShareSessionCookie|StaleShareCookieDoesNotOverrideAccountAuth)' -count=1` passed.
  - `go test ./internal/relay -count=1` passed.
  - First `go test ./...` run exposed an isolated `TestWSHostPersistFailureClosesReplacedHost` timing failure; rerunning that exact test passed, rerunning `go test ./internal/relay -count=1` passed, and rerunning `go test ./...` passed.
  - `go vet ./...` passed.
  - `golangci-lint run ./...` passed.
  - `go run golang.org/x/lint/golint@latest ./...` passed.

### B-105 Share foreground recovery reopens healthy authenticated websocket

- Status: `needs_verification`
- Area: `android`, `share`, `websocket`, `lifecycle`
- Summary: Share-token foreground recovery can re-authenticate and reopen an already healthy websocket.
- Report:
  Review found that `connectActiveSession()` builds its active connection key with `sessionId = null` whenever `shareToken` is active, while `openWebSocket()` records the key with the authenticated session id returned by `/auth/share`. Later share lifecycle calls therefore miss the healthy-connection guard and reconnect.
- Desired behavior:
  Once `/auth/share` resolves the real session id, share-mode reconnect checks should reuse that authenticated session id so healthy foreground/reselection paths do not tear down and reopen the websocket.
- Repro:
  1. Enter share-token mode and authenticate `/auth/share` to `real-session`.
  2. Open the websocket successfully.
  3. Trigger foreground recovery while the socket remains open.
  4. Observe no second share authentication, websocket close, or websocket reconnect should occur.
- Regression coverage:
  - `AppViewModelTest.shareForegroundRecoveryReusesAuthenticatedSessionConnection`
- Verification:
  - Fixed by deriving the share-mode connection key from the authenticated `activeSessionId` after it replaces the placeholder shared session id.
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.viewmodel.AppViewModelTest.shareForegroundRecoveryReusesAuthenticatedSessionConnection` was attempted but blocked because `JAVA_HOME` is not set and no `java` executable is available on `PATH`.
  - `go test ./...` passed.
  - `go vet ./...` passed.
  - `golangci-lint run ./...` passed.
  - `go run golang.org/x/lint/golint@latest ./...` passed.
  - Remaining gap: Android unit tests must be rerun in an environment with a JDK.

### B-104 Android terminal link scan runs twice per frame update

- Status: `needs_verification`
- Area: `android`, `terminal`, `performance`
- Summary: Terminal link detection can scan the full terminal snapshot twice on normal frame updates.
- Report:
  Review found that `TerminalGridView.update(...)` recomputes `TerminalLinks.findHttpsLinks(snapshot)` in both the snapshot-reference change block and the `frameSeq` change block. Normal snapshot/diff updates can change both, causing two full terminal-cell scans and avoidable UI-thread allocation work.
- Desired behavior:
  Terminal links should be recomputed once per effective snapshot/frame update and skipped when neither the snapshot nor frame sequence changed.
- Repro:
  1. Call terminal update with a new snapshot object and a new `frameSeq`.
  2. Observe link detection should run exactly once for that effective update.
- Regression coverage:
  - `TerminalLinksTest.refreshForUpdateScansOnceWhenSnapshotAndFrameSeqChange`
  - `TerminalLinksTest.refreshForUpdateSkipsScanWhenSnapshotAndFrameSeqAreUnchanged`
- Verification:
  - Fixed by collecting `snapshotChanged` and `frameSeqChanged` during `TerminalGridView.update(...)` and refreshing terminal links once after both state transitions are evaluated.
  - Added `TerminalLinks.refreshForUpdate(...)` so the one-scan behavior is unit-testable without instantiating an Android `View`.
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.terminal.TerminalLinksTest` was attempted but blocked because `JAVA_HOME` is not set and no `java` executable is available on `PATH`.
  - `go test ./...` passed.
  - `go vet ./...` passed.
  - `golangci-lint run ./...` passed.
  - `go run golang.org/x/lint/golint@latest ./...` passed.
  - Remaining gap: Android unit tests must be rerun in an environment with a JDK.

### B-103 User-management local guard accepts headerless loopback proxy requests

- Status: `resolved`
- Area: `relay`, `auth`, `users`, `security`
- Summary: `/users` management endpoints can treat headerless same-machine reverse-proxy traffic as local.
- Report:
  Review found that the new local-only guard for user-management endpoints accepts requests where both `RemoteAddr` and server `LocalAddr` are loopback and no `Forwarded`/`X-Forwarded-*` headers are present. A same-machine reverse proxy that strips forwarding headers can produce exactly that shape for a remote original client.
- Desired behavior:
  User-management endpoints should require an authenticated request that is locally connected and explicitly targeted at a loopback host such as `localhost`, `127.0.0.1`, or `::1`. Headerless loopback proxy traffic preserving a public relay `Host` must be rejected.
- Repro:
  1. Send an authenticated `/users` request with `RemoteAddr=127.0.0.1:<port>`.
  2. Set `http.LocalAddrContextKey` to a loopback listener address.
  3. Omit forwarding headers and set `Host=relay.example.com`.
  4. Observe the request should return 403.
- Regression coverage:
  - `TestUsersEndpointsRejectHeaderlessLoopbackProxyWithLoopbackListener`
- Verification:
  - Fixed by requiring local user-management requests to target a loopback host (`localhost`, `127.0.0.1`, or `::1`) in addition to having loopback socket addresses and no forwarded-client headers.
  - `go test ./internal/relay -run 'TestUsersEndpoints(RejectHeaderlessLoopbackProxyWithLoopbackListener|RejectHeaderlessLoopbackProxyHost|RejectLoopbackProxyForRemoteClient|RejectSpoofedLoopbackForwardedClient|RejectLoopbackProxyOnPublicListener)|TestUsersLifecycle' -count=1` passed.
  - `go test ./internal/relay -count=1` passed.
  - `go test ./...` passed.
  - `go vet ./...` passed.
  - `golangci-lint run ./...` passed.
  - `go run golang.org/x/lint/golint@latest ./...` passed.

### B-102 Review regressions in PTY shutdown and nil-user refresh

- Status: `needs_verification`
- Area: `session`, `relay`, `auth`
- Summary: Recent race/auth changes introduced a non-Linux local-session shutdown hang risk and broke refresh-token use for embedded relay servers without a user store.
- Report:
  Review found that local-session shutdown waited for the PTY reader before closing the PTY, which can hang on non-Linux builds where `readPTY` blocks in `file.Read` and ignores context. Review also found that `/auth/refresh` validation dereferenced or rejected nil `Users` store configurations even though `NewHTTPServer` supports embedded callers without a user store.
- Desired behavior:
  Linux should preserve the race-safe reader shutdown order. Non-Linux builds should close the PTY before waiting so blocking reads are unblocked. Relay refresh should reject tokens for removed users only when a user store exists, and should continue accepting valid refresh tokens when the server was constructed with `Users == nil`.
- Repro:
  1. On a non-Linux build, cancel a local session while its PTY reader is blocked in `Read`.
  2. Observe shutdown should not hang waiting for the reader before closing the PTY.
  3. Construct `HTTPServer` with a store containing a valid refresh token and `Users == nil`.
  4. POST `/auth/refresh` with that token and observe the request should return 200 instead of panic or 500.
- Regression coverage:
  - `TestShutdownPTYReaderLinuxWaitsBeforeClose`
  - `TestShutdownPTYReaderLinuxCancelsContext`
  - `TestShutdownPTYReaderNonLinuxClosesBeforeWaiting`
  - `TestRefreshFlowWithoutUserStore`
- Verification:
  - Fixed by moving PTY-reader shutdown into platform-specific helpers: Linux cancels and waits before closing to preserve the race fix, while non-Linux cancels and closes before waiting to unblock `file.Read`.
  - Fixed `/auth/refresh` by validating removed users only when `HTTPServer.Users` is non-nil, preserving embedded nil-user-store refresh support.
  - `go test ./internal/session -run 'TestShutdownPTYReaderLinux|TestLocalSessionCancelWaitsForPTYReaderBeforeClose' -count=1` passed.
  - `go test ./internal/relay -run 'TestRefreshFlowWithoutUserStore|TestRefreshRejectsTokenForRemovedUser|TestRefreshFlow' -count=1` passed.
  - `go test -race ./internal/session -run 'TestShutdownPTYReaderLinux|TestLocalSessionCancelWaitsForPTYReaderBeforeClose' -count=1` passed.
  - `GOOS=darwin go test -c -o /dev/null ./internal/session` passed, covering compile validity of the non-Linux shutdown helper and regression test.
  - `go test ./...` passed.
  - `go vet ./...` passed.
  - `golangci-lint run ./...` passed.
  - `go run golang.org/x/lint/golint@latest ./...` passed.
  - Remaining gap: non-Linux shutdown helper test is build-tagged `!linux` and must run on a non-Linux builder for direct platform execution.

### B-100 Android cleartext share-token websocket loses authentication

- Status: `needs_verification`
- Area: `android`, `share`, `relay`, `websocket`
- Summary: Android share-token attaches to cleartext/local endpoints can authenticate `/auth/share` and then fail `/ws/client` with 401.
- Report:
  Review found that Android now depends on the Secure `bifrons_share_session` cookie for share-token websocket authentication. Debug/local cleartext endpoints use `http://`/`ws://`, where OkHttp will not send Secure cookies, so the websocket request has only `X-Lingon-Share-Session: 1` and no usable credential.
- Desired behavior:
  Android should keep the share-session cookie path for HTTPS endpoints and preserve a token fallback for cleartext/local websocket endpoints so debug and local relays continue to work.
- Repro:
  1. Use an Android debug/local cleartext endpoint such as `http://127.0.0.1:8080/v1`.
  2. Authenticate a share token through `/auth/share`.
  3. Open `/ws/client` through the Android websocket client.
  4. Observe that the websocket should authenticate instead of failing with 401 because the Secure share cookie is unavailable over cleartext.
- Regression coverage:
  - `RelayWebSocketClientTest.cleartextShareTokenWebsocketRequestCarriesTokenFallback`
  - `RelayWebSocketClientTest.httpsShareTokenWebsocketRequestUsesShareSessionCookieOnly`
  - `AuthRefreshInterceptorTest.cleartextShareTokenWebsocketRequestBypassesAuthRefreshWithoutCookie`
- Verification:
  - Fixed by adding the share-token `?token=` fallback only for cleartext websocket requests, while keeping HTTPS share-token websocket requests on the Secure share-session cookie path.
  - Updated the Android auth-refresh interceptor to bypass local refresh auth for share websocket requests that carry an explicit share-token query fallback, so cleartext share websockets can reach the relay even when no Secure share cookie is available.
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.data.relay.RelayWebSocketClientTest --tests systems.pkt.lingon.data.AuthRefreshInterceptorTest` was attempted but blocked because `JAVA_HOME` is not set and no `java` executable is available on `PATH`.
  - `go test ./...` passed.
  - `go vet ./...` passed.
  - `golangci-lint run ./...` passed.
  - `go run golang.org/x/lint/golint@latest ./...` passed.
  - Remaining gap: Android unit tests must be rerun in an environment with a JDK.

### B-099 Headless session can hang irrecoverably against existing relay

- Status: `needs_verification`
- Area: `headless`, `relay`, `session`, `pty`
- Summary: Current Lingon may wedge a headless-served terminal when connected to the previous relay.
- Report:
  The engineer reported a totally hanging Lingon session against the previous relay after running the current version. The break appears unrecoverable from the client side and may involve the new/current Lingon running as headless while serving a terminal.
- Desired behavior:
  A current headless Lingon session should continue serving PTY output/input through an existing compatible relay. Relay disconnects, reconnects, client detach, or shutdown paths must not leave the PTY/session in a wedged state that cannot recover.
- Repro:
  1. Run the current Lingon as a headless session publishing to the previous relay.
  2. Attach to the served terminal through the relay.
  3. Observe whether terminal output/input stops permanently and whether reconnecting fails to recover the session.
- Regression coverage:
  - `TestRunnerPublishCanDisableRemoteSessions`
- Verification:
  - Live inspection of session `psl` PID `3789305` showed `/usr/local/bin/lingon -s psl` was alive, `offline=false`, heartbeat state was updating, the child `/bin/bash` was idle on `/dev/pts/16`, and the process held two established TLS connections to the relay.
  - That made a permanent-offline rejection unlikely and pointed to headless mode running both the host publisher and the interactive remote-session manager in the same daemon.
  - Fixed by adding `session.Options.DisableRemoteSessions` and setting it from `headlessd`, so headless daemons still publish the local PTY to the relay for Android/remote attach but no longer create a relay client/view connection for remote sessions.
  - `go test ./internal/session -run TestRunnerPublishCanDisableRemoteSessions -count=1` passed.
  - `go test ./internal/session ./internal/headlessd -count=1` passed.
  - `go test -tags integration ./integration/pty/attach -run 'TestRelayHeadlessMultiAttachReceivesInitialSnapshot|TestRealCLIRelayHeadlessInitialConnectAndWinchResizePTY' -count=1` passed, proving relay-published headless sessions still render and resize through normal attach.
  - Remaining gap: not yet reproduced against the engineer's exact previous relay instance.

### B-098 Android share receiver rejects wrapped embedded tokens

- Status: `needs_verification`
- Area: `android`, `share`, `tokens`
- Summary: Android share-token extraction fails when a valid embedded share token is line-wrapped by the source app.
- Report:
  Review found that `ShareTokens.findInText` only extracted contiguous alphanumeric/hyphen token candidates even though the token parser accepts spaces and newlines inside Crockford bodies. A long `LGE...` embedded token shared through Android's share sheet can be wrapped by chat/email apps, causing extraction to pass only the first fragment to `parse` and report no Lingon token found.
- Desired behavior:
  Android shared-text extraction should accept a valid bare or embedded Lingon token even when the token body contains whitespace wrapping, while still rejecting oversized or invalid candidates.
- Repro:
  1. Generate a valid embedded `LGE...` token.
  2. Insert line breaks into the token body as a share-sheet source app may do.
  3. Pass surrounding prose plus the wrapped token to `ShareTokens.findInText`.
  4. Observe the embedded token should parse with the original random bytes and endpoint.
- Regression coverage:
  - `ShareTokensTest.findInTextAcceptsWrappedEmbeddedToken`
- Verification:
  - Fixed by widening shared-text candidate extraction to include token body whitespace and by parsing bounded candidate prefixes, so valid wrapped tokens can be found even when nearby prose follows the token.
  - Added `ShareTokensTest.findInTextAcceptsWrappedEmbeddedToken`, which wraps a valid embedded token across newlines and verifies extraction preserves the embedded endpoint and random bytes.
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.share.ShareTokensTest` was attempted but blocked because `JAVA_HOME` is not set and no `java` executable is available on `PATH`.
  - Remaining gap: Android unit and compile checks must be rerun in an environment with a JDK.

### B-097 `lingonx -o` should start local headless when logged out

- Status: `resolved`
- Area: `cli`, `headless`, `offline`, `auth`, `dx`
- Summary: `lingonx -o` can still fail while logged out when relay endpoint configuration exists.
- Report:
  The engineer reported that `lingonx -o` does not start a headless offline session when logged out.
- Desired behavior:
  `lingonx -o` / `lingon -x -o` should start a local-only offline headless session when relay auth is unavailable and no CLI relay/auth flags were explicitly supplied. Ambient config/env relay endpoints should not prevent the explicit offline command from starting local headless while logged out. Explicit CLI relay/auth intent, such as `--endpoint` or `--auth-file`, should still fail visibly if auth is unavailable.
- Repro:
  1. Configure a relay endpoint in Lingon config.
  2. Remove or point away the auth file.
  3. Run `lingonx -o --session <id>`.
  4. Observe the command should report background startup and the local headless session should be attachable.
- Regression coverage:
  - `TestResolveHeadlessRelayConfigConfiguredOfflineEndpointNoAuthFallsBackLocalOnly`
  - `TestResolveHeadlessRelayConfigEnvOfflineEndpointNoAuthFallsBackLocalOnly`
  - `TestRealCLILingonXOfflineStartsLocalHeadlessWithConfiguredEndpointLoggedOut`
  - `TestRealCLILingonXReportsMissingRelayAuthBeforeDetachedExit`
- Verification:
  - Reproduced at the resolver boundary: configured/env endpoint plus explicit offline and missing auth still returned `auth file not found`.
  - Fixed by allowing explicit offline startup to fall back to local-only when auth is unavailable unless relay/auth intent came from CLI flags (`--endpoint`, `--token`, or `--auth-file`).
  - Preserved explicit CLI relay failure behavior: `lingonx --endpoint ... --auth-file <missing>` still fails visibly instead of silently starting local-only.
  - `go test ./cmd/lingon -run 'TestResolveHeadlessRelayConfig|TestHeadlessStartupReporter' -count=1` passed.
  - `go test -tags integration ./integration/pty/attach -run TestRealCLILingonXOfflineStartsLocalHeadlessWithConfiguredEndpointLoggedOut -count=1 -v` passed.
  - `go test -tags integration ./integration/pty/attach -run TestRealCLILingonXReportsMissingRelayAuthBeforeDetachedExit -count=1 -v` passed.
  - `go test ./cmd/lingon -count=1` passed.
  - `go test ./...` passed.
  - `go vet ./...` passed.
  - `golint ./...` passed.
  - `golangci-lint run ./...` passed.

### B-096 Detached headless startup hides child initialization failures

- Status: `resolved`
- Area: `cli`, `headless`, `auth`, `dx`
- Summary: `lingonx` / `lingon -x` can report background startup while the detached child immediately exits with a hidden startup error.
- Report:
  The engineer reported that `lingonx` or `lingon -x` appeared to do nothing when relay login/auth was unavailable. The important failure is not necessarily the relay requirement itself; it is that the detached parent discarded the foreground child's startup error, so the CLI did not explain that auth was missing or that `--offline` was required for local-only startup.
- Desired behavior:
  Detached headless startup must have an explicit parent/child startup contract. The parent should print the existing background-started message only after the child reports readiness. If the child fails during initialization, the parent should fail visibly with the real error and actionable guidance.
- Repro:
  1. Run `lingonx --endpoint https://relay.example/v1 --auth-file <missing> --session <id>`.
  2. Observe the parent should not print `headless session starting in background`.
  3. Observe the command should fail with `headless startup failed`, the missing-auth reason, and guidance to use `--offline` for local-only headless startup.
- Regression coverage:
  - `TestHeadlessStartupReporterWritesReadyStatus`
  - `TestHeadlessStartupReporterWritesFailureWithOfflineHint`
  - `TestResolveHeadlessRelayConfigDefaultNoAuthRequiresOffline`
  - `TestRealCLILingonXDefaultMissingRelayAuthRequiresOffline`
  - `TestRealCLILingonXReportsMissingRelayAuthBeforeDetachedExit`
- Verification:
  - Implemented a one-shot JSON startup status pipe from detached foreground child to parent. The parent now prints background startup only after a `ready` status and returns a visible `headless startup failed: ...` error when the child reports initialization failure.
  - Added `headlessd.Options.OnStartupReady`, fired after the daemon binds its local socket, writes local state, starts the runner, and starts the local attach HTTP server.
  - The child closes the startup-status fd immediately after reporting readiness so it does not linger in the daemon or user shell environment.
  - Review follow-up preserved the normal logged-out `lingonx` / `lingon -x` failure path by requiring explicit `--offline` before missing default auth can fall back to local-only mode.
  - Reproduced the review regression before the resolver fix: `go test ./cmd/lingon -run TestResolveHeadlessRelayConfigDefaultNoAuthRequiresOffline -count=1` failed because default missing auth did not require `--offline`.
  - `go test ./cmd/lingon -run 'TestResolveHeadlessRelayConfig|TestHeadlessStartupReporterWritesFailureWithOfflineHint' -count=1` passed.
  - `go test -tags integration ./integration/pty/attach -run TestRealCLILingonXDefaultMissingRelayAuthRequiresOffline -count=1 -v` passed.
  - `go test ./cmd/lingon -run 'TestHeadlessStartupReporter|TestResolveHeadlessRelayConfig|TestResolveHeadlessSize|TestHeadlessAliasEnabled' -count=1` passed.
  - `go test -tags integration ./integration/pty/attach -run 'TestRealCLILingonX(ReportsMissingRelayAuthBeforeDetachedExit|StartedHeadlessThenLingonXAttachRendersPrompt)' -count=1 -v` passed.
  - `go test ./internal/headlessd -count=1` passed.
  - `go test ./...` passed.
  - `go vet ./...` passed.
  - `go run golang.org/x/lint/golint@latest ./...` passed.
  - `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...` passed.

### B-095 Android integration emulator should launch on a non-active workspace

- Status: `resolved`
- Area: `android`, `integration`, `emulator`, `desktop`
- Summary: Managed Android integration emulator windows open on the active desktop and interfere with other work.
- Report:
  The Android integration test launcher starts the emulator GUI directly. On Xfce/xfwm4 this places the emulator on the current workspace, interrupting unrelated laptop work during long integration runs.
- Desired behavior:
  If `xws` exists on `PATH`, managed Android integration tests should start the emulator through `xws -w 8 -- ...` so the emulator lands on workspace index 8 (Workspace 9). If `xws` is unavailable, the launcher should fall back to the current direct emulator invocation. The workspace should be overrideable for local setups.
- Repro:
  1. Run the managed emulator launch used by `make android-integration-test`.
  2. Observe emulator windows appear on the active workspace.
- Regression coverage:
  - Shell syntax validation for `android/scripts/run-integration-tests.sh`.
  - Manual `xws` + `wmctrl` verification with `lingon-medium`.
- Verification:
  - Manual assertion before wiring: `xws -w 8 -- ~/Android/Sdk/emulator/emulator -avd lingon-medium -port 5554 -gpu host -no-snapshot`; `wmctrl -l` showed both `Emulator` and `Android Emulator - lingon-medium:5554` on workspace index `8`.
  - `bash -n android/scripts/run-integration-tests.sh`
  - `LINGON_IT_ONLY=top_bar_settings_is_accessible make android-integration-test` passed and logged `Starting emulator lingon-medium on port 5554 via xws workspace 8...`.

### B-092 Resize integration tab-row comparator treats control active tab as content mismatch

- Status: `resolved`
- Area: `integration`, `pty`, `session`, `resize`
- Summary: `TestHostResizeLargeViewportClearThenShortCommandMatchesControl` fails because the screen comparator normalizes tab titles but still compares active-tab markers between different control sessions.
- Report:
  Prerelease Go integration audit found a deterministic failure in `integration/pty/session`: the host and control screen bodies matched after resize, clear, and short command output, but row 1 differed only in which normalized tab carried the active `*` marker.
- Desired behavior:
  Resize content comparison tests should compare terminal body output and cursor state after normalizing tab titles. They should not fail because the host-under-test and control host are intentionally different active local tabs.
- Repro:
  1. Run `go test -count=1 -tags integration ./integration/pty/session -run '^TestHostResizeLargeViewportClearThenShortCommandMatchesControl$' -v`.
  2. Observe row 1 mismatch: `host: " <SESSION>  *<SESSION>"`, `control: " *<SESSION>  <SESSION>"`.
- Regression coverage:
  - `TestHostResizeLargeViewportClearThenShortCommandMatchesControl`
- Verification:
  - `go test -count=1 -tags integration ./integration/pty/session -run '^TestHostResizeLargeViewportClearThenShortCommandMatchesControl$' -v`
  - `go test -p 1 -count=1 -tags integration ./integration/...`

### B-093 Resize integration tab-bar assertions require unclipped long titles

- Status: `resolved`
- Area: `integration`, `pty`, `session`, `resize`
- Summary: Resize integration tests failed deterministically when long session names were clipped in the visible tab row, even though the tab bar and terminal body were rendered.
- Report:
  Prerelease Go integration audit found deterministic failures in `TestHostResizePreservesScrolledWideOutputWithTabBarVisible` and `TestHostResizePromptAdvanceWhileShrunkRestoresExpandedRowsWithTabBar`. Both expected a full active session title on row 1, but the viewport legitimately clipped the active title and, in one case, also showed the connection banner on the same row.
- Desired behavior:
  Tests should prove the tab bar is visible under constrained widths without requiring complete long session titles that cannot fit in the viewport.
- Repro:
  1. Run `go test -count=1 -tags integration ./integration/pty/session -run '^TestHostResizePreservesScrolledWideOutputWithTabBarVisible$' -v`.
  2. Observe row 1 contains `viewport-preserve-wide-scroll-output-peer` and a clipped active title, but not the full active session name.
  3. Run `go test -count=1 -tags integration ./integration/pty/session -run '^TestHostResizePromptAdvanceWhileShrunkRestoresExpandedRowsWithTabBar$' -v`.
  4. Observe row 1 contains the tab row plus `connected to ...`, but not the full active session name.
- Regression coverage:
  - `TestHostResizePreservesScrolledWideOutputWithTabBarVisible`
  - `TestHostResizePromptAdvanceWhileShrunkRestoresExpandedRowsWithTabBar`
- Verification:
  - `go test -count=1 -tags integration ./integration/pty/session -run '^TestHostResizePreservesScrolledWideOutputWithTabBarVisible$' -v`
  - `go test -count=1 -tags integration ./integration/pty/session -run '^TestHostResizePromptAdvanceWhileShrunkRestoresExpandedRowsWithTabBar$' -v`
  - `go test -p 1 -count=1 -tags integration ./integration/...`

### B-094 Android integration batch leaves terminal renderer unmeasured

- Status: `blocked`
- Area: `android`, `integration`, `terminal`
- Summary: Full Android integration prerelease gate failed in the 45-test batch with connected sessions whose terminal renderer reported zero render scale and no visible rows.
- Report:
  `make android-integration-test` passed the initial 7-test batch and the late-host 1-test scenario, then failed the larger 45-test instrumentation batch with seven failures. Most failures timed out while the app was connected and receiving frames, but reported `renderScaleX=0.0`, `renderScaleY=0.0`, and `visibleEndRowExclusive=0`; two touch tests could not find the `terminal_focus` node.
- Desired behavior:
  Android instrumentation tests should wait for or establish a measured terminal surface before terminal-render, zoom, resize, or focus interactions, and the prerelease integration target should pass reliably under its managed emulator resource profile.
- Repro:
  1. Run `make android-integration-test`.
  2. Observe the large `EndToEndTest` batch fail after earlier Android integration batches pass.
- Regression coverage:
  - Existing Android instrumentation failures include `zoomed_viewport_does_not_reset_after_frame_and_resume`, `terminal_fills_from_top_before_live_scrolling`, `resize_setting_disabled_for_view_only_share_token`, `login_with_unreachable_endpoint_shows_error`, `headless_resize_action_disabled_and_input_remains_blocked_without_control`, `pinch_zoom_adjusts_view_and_resets`, and `share_token_width_is_authoritative`.
- Verification:
  - `make android-integration-test` failed in the large `EndToEndTest` batch after the earlier Android integration batches passed.
  - `LINGON_IT_ONLY=zoomed_viewport_does_not_reset_after_frame_and_resume make android-integration-test` reproduced the failure repeatedly.
  - Additional temporary diagnostics showed `UiState` was connected to `host-4` with rows/cols/frame data while the Compose tree still rendered `settings_screen` and no `terminal_view`/`terminal_focus` was mounted. The experimental diagnostics and harness changes did not fix the repro and were not kept.
  - Remaining gap: root cause for stale Settings-route composition during focused Android instrumentation is unresolved, so the full Android prerelease gate and release gate were not completed.

### B-091 Android link detection includes closing format delimiters

- Status: `needs_verification`
- Area: `android`, `terminal`, `links`
- Summary: Android terminal link detection can include closing delimiters from common terminal output formats.
- Report:
  Review found that terminal output such as `<https://example.test>` or quoted URLs is scanned until whitespace, leaving closing `>` or quote characters in the detected URL.
- Desired behavior:
  Link underlines and tap handling should target the actual URL, not terminal formatting delimiters around it. Existing balanced URL punctuation such as `https://example.test/a_(b)` must remain intact.
- Repro:
  1. Detect links in terminal text containing `<https://angle.example/test>` or `"https://quote.example/test"`.
  2. Observe the detected link URL should exclude the closing `>` or quote.
  3. Observe balanced URL punctuation should still be preserved.
- Regression coverage:
  - `TerminalLinksTest.findHttpsLinksTrimsClosingFormatDelimiters`
  - `TerminalGridViewInstrumentedTest.delimitedHttpsLinkTapOpensTrimmedLink`
- Verification:
  - Fixed parser trimming so trailing `>`, double quote, single quote, and backtick format delimiters are excluded from detected URLs while balanced closing punctuation remains preserved.
  - `./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.terminal.TerminalLinksTest` passed.
  - `./gradlew :app:testDebugUnitTest` passed.
  - `./gradlew :app:compileDebugAndroidTestKotlin` passed.
  - `./gradlew :app:connectedDebugAndroidTest -Pandroid.testInstrumentationRunnerArguments.class=systems.pkt.lingon.terminal.TerminalGridViewInstrumentedTest#delimitedHttpsLinkTapOpensTrimmedLink` was attempted but blocked by `No connected devices!`; the adb server started by Gradle was stopped with the SDK adb.
  - `go test ./...` passed.
  - `go vet ./...` passed.
  - `golint ./...` passed.
  - `golangci-lint run ./...` passed.

### B-090 Headless offline startup must preserve explicit relay configuration

- Status: `resolved`
- Area: `cli`, `headless`, `offline`, `relay`
- Summary: `lingon -x --offline` with explicit relay configuration could start as local-only and lose the ability to publish when toggled online.
- Report:
  Review found that the headless no-auth fallback skipped endpoint and token resolution whenever `--offline` was set. That changed offline startup from "configured relay but initially disconnected" into "local-only" for explicit relay invocations.
- Desired behavior:
  `--offline` controls initial connection state only. If relay configuration is explicit or auth is available, the headless daemon must retain endpoint/token/auth and start with publishing configured but offline. Only default, non-explicit, no-auth startup may fall back to local-only mode.
- Repro:
  1. Resolve headless startup with `--offline --endpoint https://relay.example/v1 --token secret-token`.
  2. Observe the resolved daemon config should keep the endpoint and token while setting `Offline=true`.
  3. Resolve headless startup with `--offline` and `client.endpoint` set through config or `LINGON_CLIENT_ENDPOINT`, but with missing auth.
  4. Observe explicit configured endpoints should fail with the auth error instead of silently becoming local-only.
  5. Resolve headless startup with `--offline`, no explicit relay/auth flags, and an auth file containing multiple stored endpoints.
  6. Observe default offline local-only startup should not fail with endpoint ambiguity when relay publishing was not requested.
  7. Resolve headless startup with `--offline`, no explicit relay/auth flags, and a single valid stored auth endpoint.
  8. Observe the daemon should keep the inferred endpoint/auth/token while starting offline, so it can publish when toggled online later.
  9. Observe default `--offline` with no auth may still become local-only.
- Regression coverage:
  - `TestResolveHeadlessRelayConfigPreservesExplicitOfflineRelay`
  - `TestResolveHeadlessRelayConfigDefaultOfflineNoAuthFallsBackLocalOnly`
  - `TestResolveHeadlessRelayConfigDefaultOfflineSingleStoredAuthPreservesRelay`
  - `TestResolveHeadlessRelayConfigDefaultOfflineAmbiguousStoredAuthFallsBackLocalOnly`
  - `TestResolveHeadlessRelayConfigExplicitOfflineAuthFileAmbiguityRequiresEndpoint`
  - `TestResolveHeadlessRelayConfigExplicitOfflineEndpointRequiresAuth`
  - `TestResolveHeadlessRelayConfigConfiguredOfflineEndpointRequiresAuth`
  - `TestResolveHeadlessRelayConfigEnvOfflineEndpointRequiresAuth`
- Verification:
  - Fixed by resolving relay endpoint/auth independently of the initial offline state and only falling back to local-only for non-explicit no-auth startup.
  - Review follow-up fixed explicit endpoint detection for config/env endpoints, not only `--endpoint`.
  - Review follow-up fixed non-explicit offline startup so it skips auth endpoint inference and starts local-only even when stored auth contains multiple endpoints; explicit `--auth-file` still reports endpoint ambiguity.
  - Review follow-up narrowed non-explicit offline fallback to endpoint ambiguity only, preserving a single inferred stored relay auth entry.
  - `go test ./cmd/lingon -run 'TestResolveHeadlessRelayConfig' -count=1` passed.
  - `go test ./cmd/lingon -run 'TestResolveHeadlessRelayConfig|TestResolveHeadlessSize|TestHeadlessAliasEnabled' -count=1` passed.
  - `go test -tags integration ./integration/pty/attach -run TestRealCLILingonXStartedHeadlessThenLingonXAttachRendersPrompt -count=1` passed.
  - `go test ./...` passed.
  - `go vet ./...` passed.
  - `golint ./...` passed.
  - `golangci-lint run ./...` passed.

### B-089 Local headless sessions keeps stale records when orphan socket files remain

- Status: `resolved`
- Area: `headless`, `cli`, `sessions`
- Summary: `lingonx sessions` can list local headless sessions whose daemon PIDs are gone because stale Unix socket files still exist.
- Report:
  The engineer reports `lingonx sessions` lists `lj` and `ljsh` as local headless sessions even though those PIDs are gone. A relay/non-headless `lj` may still exist, but that should not keep stale local-headless records alive.
- Desired behavior:
  Local headless session records represent running headless daemons. For records with a real positive PID, the PID is authoritative; if it is dead, reconcile must remove the state entry and any owned orphan socket file. Socket existence alone is only a fallback for records without a usable PID. Cleanup must never remove arbitrary paths from malformed state.
- Repro:
  1. Persist a local headless session record with a positive dead PID.
  2. Leave a Unix socket inode at the recorded socket path.
  3. Run local headless session reconciliation, as used by `lingonx sessions`.
  4. Observe the stale session should be pruned instead of listed.
- Regression coverage:
  - `TestReconcilePrunesDeadPIDEvenWhenSocketFileRemains`
  - `TestReconcileKeepsNoPIDRecordWithSocket`
  - `TestReconcileDoesNotRemoveStaleRecordPathOutsideHeadlessDir`
  - `TestReconcileDoesNotRemoveSocketOutsideHeadlessDir`
- Verification:
  - Reproduced with a persisted local headless record using a positive dead PID plus an orphan Unix socket inode.
  - Fixed by making positive PID liveness authoritative during reconciliation and removing orphan socket files when pruning dead-PID records.
  - Review follow-up fixed cleanup so reconciliation removes only Unix socket paths owned under this store's headless directory; malformed paths and sockets outside that directory are left untouched while their state records are pruned.
  - `go test ./internal/headless -run 'TestReconcile(PrunesDeadPIDEvenWhenSocketFileRemains|KeepsNoPIDRecordWithSocket)' -count=1` passed.
  - `go test ./cmd/lingon -run 'Test(SessionsHeadlessUsesLocalState|Detach|SendHeadless|ConfigDirForLoader)' -count=1` passed.
  - `go test ./...` passed.
  - `go vet ./...` passed.
  - `golint ./...` passed.
  - `golangci-lint run ./...` passed.

### B-088 `lingonx attach` to local headless session renders blank screen

- Status: `resolved`
- Area: `cli`, `attach`, `headless`, `pty`
- Summary: `lingon attach -x` works for local headless sessions, but invoking the same mode through the `lingonx` argv0 alias reportedly shows nothing.
- Report:
  The engineer reports that attaching to a headless session via `lingon attach` works, while `lingonx attach` shows no terminal content. The command sounds ambiguous, but the current README and CLI contract indicate `lingonx attach` should be equivalent to `lingon attach -x`.
- Desired behavior:
  `lingonx attach` is a local-headless attach UI. It must either render the selected local headless session content, show a visible local-headless loading/status state while connecting or waiting for the first snapshot, or exit with an actionable error. A plain black terminal is never valid behavior, and neither is a loading label painted over stale pre-attach terminal contents.
- Repro:
  1. With no relay auth file, start a local headless session through the real CLI alias: `lingonx --session <id> --shell <shell>`.
  2. Observe the parent reports background startup, but the detached child exits before writing local headless state because it tries to resolve relay auth.
  3. Run `lingonx attach <id>`.
  4. Observe the attached terminal should render the local headless session prompt/content rather than having no real session to attach to.
  5. If a local headless socket exists but does not produce a first snapshot yet, observe the attach UI should show `loading local headless` rather than a blank screen.
  6. Observe the loading UI must clear stale terminal contents before painting the status label.
  7. Once the first local-headless snapshot arrives and the view is ready, observe the `loading local headless` label must disappear.
- Regression coverage:
  - `TestMultiAttachLocalHeadlessShowsLoadingWhileWaitingForSnapshot`
  - `TestMultiAttachLocalHeadlessClearsLoadingAfterFirstSnapshot`
  - `TestRealCLILingonXAttachLocalHeadlessRendersPrompt`
  - `TestRealCLILingonXAttachWithoutSessionIDRendersSingleLocalHeadlessPrompt`
  - `TestRealCLILingonXStartedHeadlessThenLingonXAttachRendersPrompt`
  - `TestRealCLIRootHeadlessAttachLocalHeadlessRendersPrompt`
- Verification:
  - Added isolated real-CLI PTY coverage for `lingonx attach <session-id>`, `lingonx attach` with one local headless session, and `lingon -x attach <session-id>`.
  - Reproduced the real failing path with `go test -tags integration ./integration/pty/attach -run TestRealCLILingonXStartedHeadlessThenLingonXAttachRendersPrompt -count=1`; failure evidence was timeout waiting for the local headless session to appear after real `lingonx` detached startup.
  - Manual foreground-child repro exposed the hidden child error: `auth file not found ...; run lingon login`.
  - Fixed by allowing default headless foreground startup to fall back to local-only/offline mode when relay auth is absent and no relay/auth flags were explicitly supplied.
  - Reproduced the remaining black-screen state with `go test ./internal/attach -run TestMultiAttachLocalHeadlessShowsLoadingWhileWaitingForSnapshot -count=1`; failure evidence was an all-blank screen while a local headless socket was connected but no snapshot had arrived.
  - Fixed by rendering local-headless loading status immediately after active local view registration and allowing loading renders to use the attach client's blank snapshot fallback before the first real snapshot.
  - Screenshot follow-up showed `loading local headless` painted over stale terminal contents. Fixed by forcing a full clear for pre-snapshot loading renders and extending `TestMultiAttachLocalHeadlessShowsLoadingWhileWaitingForSnapshot` to assert full-redraw ANSI output.
  - Screenshot follow-up showed `loading local headless` remaining after local session content rendered. Fixed by clearing loading status in the shared ready path for local-headless views as well as relay views.
  - `go test ./internal/attach -run 'TestMultiAttachLocalHeadless(ShowsLoadingWhileWaitingForSnapshot|ClearsLoadingAfterFirstSnapshot)' -count=1` passed.
  - `go test -tags integration ./integration/pty/attach -run TestRealCLILingonXStartedHeadlessThenLingonXAttachRendersPrompt -count=1` passed.
  - `go test -tags integration ./integration/pty/attach -run 'TestRealCLI(LingonXStartedHeadlessThenLingonXAttachRendersPrompt|LingonXAttachLocalHeadlessRendersPrompt|LingonXAttachWithoutSessionIDRendersSingleLocalHeadlessPrompt|RootHeadlessAttachLocalHeadlessRendersPrompt)' -count=1` passed.
  - `go test ./...` passed.
  - `go vet ./...` passed.
  - `golint ./...` passed.
  - `golangci-lint run ./...` passed.

### B-087 Geometry zero dimensions must preserve explicit dimensions and fail before detached headless startup

- Status: `resolved`
- Area: `cli`, `geometry`, `headless`, `session`
- Summary: `--geometry 80x0` and `--geometry 0x50` could lose the explicit dimension in normal host startup, and invalid detached headless geometry could fail only after reexec.
- Report:
  Review found that non-headless partial-zero geometry was parsed correctly but later session startup auto-detected both dimensions when either dimension was zero. Review also found that detached headless startup accepted invalid free-form geometry in the parent and failed only in the detached child with output redirected away.
- Repro:
  1. Start a normal host with `--geometry 80x0` in a terminal sized `90x30`.
  2. Observe the host should use `80x30`, preserving the explicit columns and auto-detecting only rows.
  3. Start detached headless with `-x --geometry bad`.
  4. Observe the parent should reject the invalid geometry before reexec instead of reporting background startup.
- Regression coverage:
  - `TestRunnerPartialSizeAutoDetectPreservesExplicitDimension`
  - `TestValidateHeadlessReexecFlagsRejectsInvalidGeometry`
  - `TestValidateHeadlessReexecFlagsAcceptsValidGeometry`
- Verification:
  - Reproduced partial non-headless geometry overwrite with `go test ./internal/session -run TestRunnerPartialSizeAutoDetectPreservesExplicitDimension -count=1`; failure evidence was `runner size = 90x30, want 80x30`.
  - Fixed by only auto-detecting dimensions whose configured value is `<= 0`, preserving explicit dimensions.
  - Fixed detached headless validation by parsing `--geometry` before `startHeadlessReexec` starts the child.
  - `go test ./internal/session -run TestRunnerPartialSizeAutoDetectPreservesExplicitDimension -count=1` passed.
  - `go test ./cmd/lingon -run 'TestValidateHeadlessReexecFlags|TestResolveRootHostSizeGeometryZeroDimensionsUseAutoDetectSentinels|TestResolveHeadlessSizeGeometryZeroDimensionsUseDefaults' -count=1` passed.
  - `go test ./cmd/lingon ./internal/session -count=1` passed.
  - `go test ./internal/session -run TestRemoteInputSurvivesStaleSessionsList -count=1` passed after one full-suite transient failure.
  - `go test ./...` passed.
  - `go vet ./...` passed.
  - `golint ./...` passed.
  - `golangci-lint run ./...` passed.

### B-086 Android terminal links must not span hard row breaks without wrap metadata

- Status: `resolved`
- Area: `android`, `terminal`, `links`
- Summary: Android terminal link detection could join a URL at the last column of one row with unrelated text on the next row.
- Report:
  Review found that link detection inferred soft wrapping from a nonblank final cell, so hard line breaks that exactly filled the terminal width could produce incorrect URLs spanning the next prompt or command row.
- Repro:
  1. Render a terminal snapshot with `https://example.test` exactly filling a 20-column row.
  2. Render `/prompt` on the next row after a hard line break.
  3. Detect links from the snapshot.
  4. Observe the link must be `https://example.test`, not `https://example.test/prompt`.
- Regression coverage:
  - `TerminalLinksTest.findHttpsLinksDoesNotJoinHardLineBreaksAtLastColumn`
  - `TerminalLinksTest.findHttpsLinksDoesNotJoinRowsWithoutWrapMetadata`
- Verification:
  - Reproduced with `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.terminal.TerminalLinksTest.findHttpsLinksDoesNotJoinHardLineBreaksAtLastColumn`.
  - Fixed by treating snapshot row boundaries as delimiters unless explicit wrap metadata is available.
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.terminal.TerminalLinksTest.findHttpsLinksDoesNotJoinHardLineBreaksAtLastColumn` passed.
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.terminal.TerminalLinksTest` passed.
  - `cd android && ./gradlew :app:testDebugUnitTest` passed.
  - `cd android && ./gradlew :app:compileDebugAndroidTestKotlin` passed.

### B-085 `lingon attach` disables xterm text selection

- Status: `resolved`
- Area: `attach`, `terminal`, `xterm`, `mouse`
- Summary: Starting `lingon attach` in xterm suppresses the normal selection pointer and prevents ordinary drag text selection while Lingon is running.
- Report:
  The engineer reports that `lingon attach` in xterm has no selection pointer and ordinary terminal text selection is unavailable while Lingon is running, behavior they have not seen in other terminal apps.
- Repro:
  1. Start a Lingon host session.
  2. Start `lingon attach` for that session in xterm.
  3. Try to drag-select text in the terminal.
  4. Observe normal xterm selection is suppressed.
- Regression coverage:
  - `TestAttachStartupDoesNotEnableMouseReporting`
  - `TestMultiAttachStartupDoesNotEnableMouseReporting`
  - `TestAttachScrollbackScopesMouseReporting`
  - `TestMultiAttachReconnectDisablesScrollbackMouseReporting`
- Verification:
  - Reproduced with `go test -tags integration ./integration/pty/attach -run TestAttachStartupDoesNotEnableMouseReporting -count=1`.
  - Failure evidence: attach startup raw output contains `ESC[?1000h` and `ESC[?1006h`, which enable xterm mouse reporting and suppress normal drag selection.
  - Review follow-up reproduced stale mouse reporting after multi-attach reconnect with `go test -tags integration ./integration/pty/attach -run TestMultiAttachReconnectDisablesScrollbackMouseReporting -count=1`.
  - Fixed by removing unconditional attach startup mouse-reporting enablement and scoping mouse reporting to attach scrollback mode.
  - Fixed reconnect follow-up by resyncing the outer terminal mouse mode from the replacement active view after reconnect.
  - `go test -tags integration ./integration/pty/attach -run 'Test(AttachStartupDoesNotEnableMouseReporting|MultiAttachStartupDoesNotEnableMouseReporting|AttachScrollbackScopesMouseReporting)$' -count=1` passed.
  - `go test -tags integration ./integration/pty/attach -run TestMultiAttachReconnectDisablesScrollbackMouseReporting -count=1` passed.
  - `go test -tags integration ./integration/pty/attach -run TestAttachScrollbackWheelUpReachesOlderRows -count=1` passed.
  - `go test ./internal/attach -count=1` passed.
  - `go test ./...` passed.
  - `go vet ./...` passed.
  - `golint ./...` passed.
  - `golangci-lint run ./...` passed.
  - Review follow-up verification: `go test ./...`, `go vet ./...`, `golint ./...`, and `golangci-lint run ./...` passed after the reconnect fix.

### B-084 Android certificates settings detail overlaps the system top area

- Status: `needs_verification`
- Area: `android`, `settings`, `certificates`, `layout`
- Summary: The Manage certificates settings detail starts too high on Android 15/16 and can appear smashed into the system/top banner area.
- Report:
  The engineer reports that Manage certificates sits far too high, visually between/on top of the very top banner on Android 15/16.
- Repro:
  1. Open Android settings.
  2. Tap Certificates.
  3. Observe the certificates detail page starts too close to the top/system area instead of using the same safe top spacing as the settings page.
- Regression coverage:
  - `certificates_setting_returns_to_settings_flow` exercises opening the certificates detail through settings and returning to settings.
- Verification:
  - `cd android && ./gradlew :app:compileDebugAndroidTestKotlin` passed.
  - `cd android && LINGON_IT_ONLY=certificates_setting_returns_to_settings_flow,endpoint_setting_dialog_stays_in_settings_flow make integration-test` passed.
  - `cd android && ./gradlew :app:testDebugUnitTest` passed.
  - Remaining gap: needs phone confirmation on Android 15/16 that the certificates detail no longer overlaps or crowds the system/top area.

### B-083 Android settings detail actions leave the settings flow

- Status: `needs_verification`
- Area: `android`, `settings`, `navigation`, `certificates`, `dialogs`
- Summary: Selecting Endpoint, Certificates, Theme, App lock, or similar settings closes the settings page and shows the detail UI over the underlying app screen.
- Report:
  The engineer reports that choosing settings rows such as Endpoint closes the settings screen and shows the endpoint dialog on top of the original screen. Certificates also return to the main screen after configuration. This is confusing now that settings is a page.
- Repro:
  1. Open Android settings.
  2. Tap Endpoint.
  3. Observe the settings page disappears behind the endpoint dialog.
  4. Tap Certificates.
  5. Observe the app leaves settings and returns to the previous app screen after certificate management.
- Regression coverage:
  - `endpoint_setting_dialog_stays_in_settings_flow`
  - `certificates_setting_returns_to_settings_flow`
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.ui.SettingsScreenTest` passed.
  - `cd android && ./gradlew :app:compileDebugAndroidTestKotlin` passed.
  - `cd android && LINGON_IT_ONLY=endpoint_setting_dialog_stays_in_settings_flow,certificates_setting_returns_to_settings_flow,settings_page_opens_and_returns_to_login,settings_page_uses_rendered_back_button,follow_on_read_settings_toggle_defaults_off_and_toggles make integration-test` passed.
  - `cd android && ./gradlew :app:testDebugUnitTest` passed.
  - Remaining gap: needs phone confirmation that all setting-detail flows now feel intuitive in the installed app.

### B-082 Android settings screen should resemble Android system settings layout

- Status: `needs_verification`
- Area: `android`, `settings`, `ui`, `layout`
- Summary: The Android settings screen uses the system font, but still does not visually read like Android 15/16 system Settings.
- Report:
  The engineer reports that the settings screen still does not look like Android system settings. Lingon theme colors should remain, but the layout should be closer to Android 15/16 Settings.
- Repro:
  1. Open Lingon settings.
  2. Compare against Android 15/16 Settings visual language: page title, grouped settings sections, large touch rows, current-value summaries, switches/carets, and modern Material spacing.
  3. Observe Lingon still uses a compact terminal-like list with text-only rows and per-row dividers.
- Regression coverage:
  - `SettingsScreenTest.settingsLayoutUsesSystemSettingsScale`
  - Existing settings e2e coverage still opens, closes, navigates back, and toggles Follow on read through actual user-visible settings interactions.
- Verification:
  - Android settings research checked AOSP settings guidance and Android 15/16 Settings redesign references.
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.ui.SettingsScreenTest` passed.
  - `cd android && ./gradlew :app:testDebugUnitTest` passed.
  - `cd android && ./gradlew :app:compileDebugAndroidTestKotlin` passed.
  - `cd android && LINGON_IT_ONLY=top_bar_settings_is_accessible,settings_page_opens_and_returns_to_login,settings_page_uses_rendered_back_button,settings_page_closes_and_reload_triggers,follow_on_read_settings_toggle_defaults_off_and_toggles make integration-test` passed.
  - Remaining gap: needs device confirmation from the engineer that the visual result now matches the intended Android 15/16 system Settings feel closely enough.

### B-081 Android transient session expiry clears sessions and bursts queued wall notifications

- Status: `needs_verification`
- Area: `android`, `auth`, `sessions`, `notifications`, `wall`
- Summary: A transient Android auth/session expiry can throw the user out of every session even when refresh succeeds, then later drain queued wall notifications in a burst.
- Report:
  The engineer saw a "session expired" bug in the Android app. Relogin was not required, but all sessions were cleared and multiple queued wall notifications arrived afterward.
- Repro:
  1. Start Android logged in with attached sessions and background wall notifications enabled.
  2. Trigger a websocket authorization failure or 401 while the refresh token remains valid.
  3. Observe the app should refresh/recover instead of clearing sessions or resetting wall polling state.
  4. Queue wall events during the transient failure and reconnect; wall delivery should use the normal monotonic cursor path without treating recoverable auth churn as logout.
- Regression coverage:
  - `AppViewModelTest.websocketUnauthorizedRefreshSuccessPreservesSessionsAndRecovers`
  - `AppViewModelTest.websocketAuthorizationFrameRefreshSuccessPreservesSessionsAndRecovers`
  - `AppViewModelTest.websocketUnauthorizedRefreshFailureExpiresSession`
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.viewmodel.AppViewModelTest.websocketUnauthorizedRefreshSuccessPreservesSessionsAndRecovers --tests systems.pkt.lingon.viewmodel.AppViewModelTest.websocketUnauthorizedRefreshFailureExpiresSession --tests systems.pkt.lingon.viewmodel.AppViewModelTest.websocketAuthorizationFrameRefreshSuccessPreservesSessionsAndRecovers` passed.
  - `cd android && ./gradlew :app:testDebugUnitTest` passed.
  - `cd android && ./gradlew :app:compileDebugAndroidTestKotlin` passed.
  - Remaining gap: no targeted instrumentation repro yet for the exact phone notification burst; the fix is unit-proven at the auth/session boundary and needs device confirmation before marking resolved.

### B-080 Android settings screen should use the system font

- Status: `resolved`
- Area: `android`, `settings`, `typography`, `ui`
- Summary: The Android settings screen must use the platform/default Material font, while retaining Lingon theme colors.
- Report:
  The installed Android app still showed the new settings screen in the app-wide monospace font instead of looking like normal Android settings.
- Repro:
  1. Open the Android settings screen.
  2. Observe settings rows and headings use the same monospace typography as the terminal UI.
  3. Expected: settings keeps Lingon colors but uses the standard Android system font.
- Regression coverage:
  - `SettingsScreenTest.settingsTypographyUsesPlatformDefaultFontFamily`
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.ui.SettingsScreenTest` passed.
  - `cd android && ./gradlew :app:testDebugUnitTest` passed.
  - `cd android && ./gradlew :app:compileDebugAndroidTestKotlin` passed.

### B-079 Session lists should be sorted alphanumerically everywhere

- Status: `resolved`
- Area: `sessions`, `relay`, `attach`, `host`, `headless`, `android`, `api`
- Summary: All user-visible and API session lists must be sorted by session name instead of recency, discovery, id, or prior tab order.
- Report:
  The engineer requested alphanumeric session ordering in local PTY, local headless, attach, relay, the sessions API, and the Android app.
- Repro:
  1. Create or receive sessions in non-alphabetic display-name order such as `Charlie`, `Alpha`, `Bravo`.
  2. List sessions through `/sessions`, SDK/CLI, host/attach tab models, local headless, or Android state.
  3. Observe the list must be `Alpha`, `Bravo`, `Charlie` regardless of activity timestamps, ids, or update order.
- Regression coverage:
  - Go:
    - `TestLessUsesNameThenID`
    - `TestSortSessionsByName`
    - `TestStoreListSessionsSortsByName`
    - `TestListSessionsAPISortsByName`
    - `TestListSessionsSortsByName`
    - `TestHeadlessSessionSource`
    - `TestFirstLocalHeadlessSessionUsesIDOrder`
    - `TestMergeSessionsSortsLocalSessionsByName`
    - `TestMergeSessionsIgnoresPreviousOrderAndSortsByName`
    - `TestOrderSessionsSortsKnownSessionsByName`
    - `TestOrderSessionsSortsUnseenSessionsByName`
    - `TestOrderSessionsSortsFirstPayloadByName`
  - Android:
    - `AppViewModelTest.sessionsAreSortedByNameAcrossBootstrapRefreshAndWebsocketUpdates`
- Verification:
  - `go test -count=1 ./internal/sessionorder ./internal/mvu ./internal/relay ./internal/session ./internal/attach ./cmd/lingon . -run 'Test(LessUsesNameThenID|SortSessionsByName|StoreListSessionsSortsByName|ListSessionsAPISortsByName|ListSessionsSortsByName|FirstLocalHeadlessSessionUsesIDOrder|HeadlessSessionSource|MergeSessions|OrderSessions|NextSessionID)'` passed.
  - `cd android && ./gradlew :app:testDebugUnitTest --tests systems.pkt.lingon.viewmodel.AppViewModelTest.sessionsAreSortedByNameAcrossBootstrapRefreshAndWebsocketUpdates` passed after preserving missing-session grace in the expected sorted result.
  - `go test -count=1 ./...` passed.
  - `cd android && ./gradlew :app:testDebugUnitTest` passed.
  - `cd android && ./gradlew :app:compileDebugAndroidTestKotlin` passed.
  - `go vet ./...` passed.
  - `golint ./...` passed.
  - `golangci-lint run ./...` passed.

### B-078 Android settings should be a page, not a popup menu

- Status: `resolved`
- Area: `android`, `topbar`, `settings`, `ui`
- Summary: Replace the fragile Android top-bar popup menu with a themed settings page opened by a settings cog.
- Report:
  The engineer proposed replacing the broken top-bar menu with a separate settings page so controls are scrollable, future settings have room, and menu overlay/close behavior cannot regress.
- Repro:
  1. Open the Android top-bar settings affordance.
  2. The app should navigate to a full settings screen instead of showing a popup over terminal content.
  3. Press the rendered back button and observe the prior screen returns without menu overlap or close flicker.
- Regression coverage:
  - Android instrumentation:
    - `top_bar_settings_is_accessible`
    - `settings_page_opens_and_returns_to_login`
    - `settings_page_uses_rendered_back_button`
    - `settings_page_closes_and_reload_triggers`
    - `follow_on_read_settings_toggle_defaults_off_and_toggles`
- Verification:
  - `cd android && ./gradlew :app:compileDebugKotlin` passed.
  - `cd android && ./gradlew :app:testDebugUnitTest` passed.
  - `cd android && ./gradlew :app:compileDebugAndroidTestKotlin` passed.
  - Android targeted e2e passed:
    - `LINGON_IT_ONLY=top_bar_settings_is_accessible,settings_page_opens_and_returns_to_login,settings_page_uses_rendered_back_button,settings_page_closes_and_reload_triggers,follow_on_read_settings_toggle_defaults_off_and_toggles make integration-test`
  - Resource profile:
    - `android/test-artifacts/resource-profile-20260515-122804-51366/summary.txt`

### B-077 Android top-bar menu close button flickers instead of closing

- Status: `resolved`
- Area: `android`, `topbar`, `menu`, `ui`, `event-dispatch`
- Summary: The Android top-bar menu can flicker and remain open when pressing the rendered X close button.
- Report:
  The engineer reports that the menu is still broken and that pressing X does not close it, it only flickers.
- Repro:
  1. Open the Android top-bar menu.
  2. Press the rendered X close button.
  3. Observe that the menu flickers and remains open instead of closing.
- Fix:
  - Removed the separate popup window from the top-bar menu path.
  - The menu is now an anchored overlay child of the menu button layout, and open/close render as separate button nodes with explicit accessibility click actions.
- Regression coverage:
  - Strengthened `menu_overlay_does_not_cover_close_button` to open through the rendered menu button, click the rendered close button or its clickable ancestor, assert the menu disappears structurally, and assert the rendered menu button returns.
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest` passed.
  - `cd android && ./gradlew :app:compileDebugAndroidTestKotlin` passed.
  - `cd android && LINGON_IT_ONLY=menu_overlay_does_not_cover_close_button make integration-test` passed.
  - `cd android && LINGON_IT_ONLY=menu_overlay_does_not_shift_login make integration-test` passed.

### B-076 Android top-bar menu is not anchored to its button

- Status: `resolved`
- Area: `android`, `topbar`, `menu`, `ui`
- Summary: The Android top-bar menu can appear in inconsistent places because the popup is positioned with a fixed window offset instead of the measured menu-button anchor.
- Report:
  The engineer reports that the menu is still "all over the place" after the earlier close-button overlap fix.
- Repro:
  1. Open the Android top-bar menu in different layouts/orientations.
  2. Observe that the popup position is derived from the window top/end rather than the actual menu button bounds, so it can drift away from the button or overlap top-bar controls.
- Fix:
  - Removed window-level popup placement from the top-bar menu path.
  - The menu now opens as an overlay child anchored to the measured menu button, so it is positioned from the same layout tree as the top bar instead of a separate window offset.
- Regression coverage:
  - Strengthened `menu_overlay_does_not_cover_close_button` to use UIAutomator screen bounds, proving the rendered first menu item is below the rendered menu button instead of relying on popup-local Compose semantics.
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest` passed.
  - `cd android && ./gradlew :app:compileDebugAndroidTestKotlin` passed.
  - `cd android && LINGON_IT_ONLY=menu_overlay_does_not_cover_close_button make integration-test` passed.
  - `cd android && LINGON_IT_ONLY=menu_overlay_does_not_shift_login make integration-test` passed.

### B-075 Distinct wall notification regression cleared its own expected notification

- Status: `resolved`
- Area: `android`, `integration`, `notifications`, `test-harness`
- Summary: The distinct wall notification regression waited for the second message with a helper that clears non-matching wall notifications, so it could delete the first notification before asserting both remained active.
- Report:
  Review flagged that `waitForWallNotificationBody(secondMessage, ...)` reused destructive stale-notification cleanup. That default is useful for single-message tests, but unsafe when the test needs multiple notifications to remain visible.
- Repro:
  1. Post the first distinct wall message and wait until its notification is visible.
  2. Post the second distinct wall message.
  3. While waiting for the second message, observe only the first notification.
  4. The old helper clears the first notification as unexpected, invalidating the later two-notification assertion.
- Fix:
  - Added an explicit `clearUnexpectedWallNotifications` option to `waitForWallNotificationBody`.
  - Kept destructive cleanup as the default for existing single-message tests.
  - Disabled destructive cleanup only while the distinct-notifications test waits for the second message.
- Regression coverage:
  - `background_distinct_wall_messages_post_distinct_system_notifications` now preserves the first expected notification while waiting for the second.
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest` passed.
  - `cd android && ./gradlew :app:compileDebugAndroidTestKotlin` passed.
  - `cd android && LINGON_IT_ONLY=background_distinct_wall_messages_post_distinct_system_notifications make integration-test` passed.

### B-074 Android disabling Follow on read can leave cursor-follow active

- Status: `resolved`
- Area: `android`, `terminal`, `viewport`, `follow-on-read`
- Summary: Disabling the Follow on read toggle could leave `TerminalGridView` in cursor-follow mode, allowing later passive output to keep panning the camera even though the setting was off.
- Report:
  Review flagged that `cameraMode` and `cursorFollowReturnMode` could both remain `CursorFollow` after read-driven follow, and toggling Follow on read off only changed the boolean. The engineer confirmed this matched suspected behavior.
- Repro:
  1. Enable Follow on read.
  2. Receive passive output that moves the cursor and causes read-driven cursor-follow.
  3. Disable Follow on read.
  4. Receive more passive output with cursor movement.
  5. Observe the viewport can continue following the cursor even though the toggle is disabled.
- Fix:
  - Read-driven cursor-follow now preserves the previous return mode instead of replacing it with `CursorFollow`.
  - Disabling Follow on read exits active cursor-follow and normalizes an invalid `CursorFollow` return mode back to `LiveBottom`.
- Regression coverage:
  - Added `disabling_follow_on_read_stops_passive_cursor_follow`, which enters read-driven cursor-follow, disables the setting, then verifies a later passive cursor move does not pan the camera.
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest` passed.
  - `cd android && ./gradlew :app:compileDebugAndroidTestKotlin` passed.
  - `cd android && LINGON_IT_ONLY=disabling_follow_on_read_stops_passive_cursor_follow make integration-test` passed.

### B-073 Android targeted final-batch integration failures

- Status: `open`
- Area: `android`, `integration`, `notifications`, `viewport`
- Summary: A retest of the previously failing final Android integration batch shows that most entries were stale, but three current failures still reproduce.
- Report:
  While verifying the reported `menu_overlay_does_not_cover_close_button` failure through `make test-android`, the first 5-test batch and 44-test viewport batch were made green, but the later 29-test batch failed. A later targeted rerun of the originally listed failures showed only three still reproducing.
- Repro:
  1. Run the targeted batch:
     `cd android && LINGON_IT_ONLY=foreground_manual_wall_delivery_does_not_post_system_notification,focused_background_resume_preserves_live_camera_when_cursor_still_fits,keyboard_visible_zoomed_pan_across_scrollback_boundary_is_continuous,keyboard_tab_switch_preserves_bottom_anchor_visual,loaded_tall_session_uses_system_visible_bottom_before_and_after_keyboard_toggle,background_wall_delivery_posts_system_notification,background_distinct_wall_messages_post_distinct_system_notifications,headless_resize_button_resizes_remote_headless_session make integration-test`
  2. Observe the current failures listed below.
- Current failures:
  - `focused_background_resume_preserves_live_camera_when_cursor_still_fits`
  - `keyboard_visible_zoomed_pan_across_scrollback_boundary_is_continuous`
  - `background_wall_delivery_posts_system_notification`
- Stale entries that passed in the targeted rerun:
  - `foreground_manual_wall_delivery_does_not_post_system_notification`
  - `keyboard_tab_switch_preserves_bottom_anchor_visual`
  - `loaded_tall_session_uses_system_visible_bottom_before_and_after_keyboard_toggle`
  - `background_distinct_wall_messages_post_distinct_system_notifications`
  - `headless_resize_button_resizes_remote_headless_session`
- Verification:
  - `cd android && LINGON_IT_ONLY=menu_overlay_does_not_cover_close_button make integration-test` passed.
  - The first 5-test batch inside `make test-android` passed.
  - The 44-test viewport batch inside `make test-android` passed.
  - `cd android && LINGON_IT_ONLY=foreground_manual_wall_delivery_does_not_post_system_notification,focused_background_resume_preserves_live_camera_when_cursor_still_fits,keyboard_visible_zoomed_pan_across_scrollback_boundary_is_continuous,keyboard_tab_switch_preserves_bottom_anchor_visual,loaded_tall_session_uses_system_visible_bottom_before_and_after_keyboard_toggle,background_wall_delivery_posts_system_notification,background_distinct_wall_messages_post_distinct_system_notifications,headless_resize_button_resizes_remote_headless_session make integration-test` failed with three current failures and five stale entries passing.

### B-072 Android terminal insets use API 30 runtime APIs despite minSdk 26

- Status: `resolved`
- Area: `android`, `terminal`, `viewport`, `compatibility`, `insets`
- Summary: `TerminalGridView` used API 30 `WindowInsets.Type` and `WindowInsets.getInsets(...)` calls even though the app declares `minSdk = 26`.
- Report:
  Review flagged that the terminal layout/draw path could crash on supported Android 8-10 devices. The engineer noted that Android 15-16 support is more important, but asked whether the P1 is real and why latest SDK would imply Android 8-10.
- Repro:
  1. Install the APK on a device or emulator running API 26-29.
  2. Open a terminal view so `effectiveViewportHeightPx()` runs from layout/draw.
  3. Observe that unguarded API 30 insets methods are reachable on a supported pre-30 runtime.
- Fix:
  - Replaced unguarded platform `WindowInsets.Type.*` reads with `WindowInsetsCompat` in the terminal view.
  - Updated Android instrumentation insets helpers to use the same compat API.
- Regression coverage:
  - Removed all `WindowInsets.Type` references from app and test source so pre-30 runtime-only APIs cannot re-enter these code paths without a visible source diff.
- Verification:
  - `rg "WindowInsets.Type" android/app/src/main/java android/app/src/test android/app/src/androidTest` returns no matches.
  - `cd android && ./gradlew :app:testDebugUnitTest` passed.
  - `cd android && ./gradlew :app:compileDebugAndroidTestKotlin` passed.
  - `cd android && LINGON_IT_ONLY=soft_keyboard_inset_keeps_terminal_viewport_above_keyboard make integration-test` passed.

### B-071 Host local PTY input is unavailable before relay/auth connection

- Status: `resolved`
- Area: `host`, `local-pty`, `startup`, `input`, `relay`
- Summary: A Lingon host local PTY can appear hung before relay connection/auth refresh completes; typed input is not accepted even though local PTYs must remain usable before connecting.
- Report:
  The engineer reports that `lingon host` local PTY hangs before it is connected and input cannot be entered. Expected behavior is that local typing is always accepted; if relay publishing is not connected yet, input should still reach the local PTY and relay state can catch up later.
- Repro:
  1. Start a host local PTY with relay publishing enabled while auth refresh/relay connection is delayed.
  2. Type into the host before the relay reports connected.
  3. Observe that the local shell does not accept the input until connection/auth unblocks.
- Regression coverage:
  - Added `TestHostLocalPTYAcceptsInputWhileAuthRefreshIsBlocked`, which runs a host runner inside a test-owned PTY, blocks `/auth/refresh`, waits for the local shell to render while refresh is still blocked, sends input through the outer PTY, and asserts the local PTY processes it before relay/auth completes.
- Verification:
  - Reproduced before the fix: the regression only saw outer terminal echo or a ready prompt while auth/remote refresh blocked; the local shell did not emit `PRECONNECT_OK`.
  - `go test -count=1 ./internal/session -run TestHostLocalPTYAcceptsInputWhileAuthRefreshIsBlocked -v`
  - `go test -count=1 ./internal/session`
  - `go test -count=1 ./internal/session ./internal/publisher ./internal/relayclient`

### B-070 Android menu popup overlaps top bar close button

- Status: `resolved`
- Area: `android`, `topbar`, `menu`, `ui`, `hit-target`
- Summary: Opening the Android top-bar menu can place the popup over the top-bar menu/close button, making the close button impossible to press.
- Report:
  The engineer reports that the menu overlaps the top bar and covers the close button underneath. This is unacceptable and a regression from earlier behavior.
- Repro:
  1. Open the Android top-bar menu.
  2. Observe that the popup overlaps the top bar menu/close button hit target.
  3. Attempt to press the close button; the popup intercepts the hit.
- Regression coverage:
  - Added `menu_overlay_does_not_cover_close_button`, an Android UI regression that opens the top-bar menu and verifies a real tap on the top-bar close/menu button dismisses the menu.
  - Fixed the test touch helper to send node-local center coordinates instead of root coordinates, so the regression performs the intended hit test.
- Verification:
  - Reproduced in connected instrumentation: the original bounds assertion saw the popup covering the close-button area and the first hit-test version timed out waiting for the menu to close.
  - `cd android && LINGON_IT_ONLY=menu_overlay_does_not_cover_close_button make integration-test` passed.
  - `cd android && ./gradlew :app:testDebugUnitTest` passed.
  - `cd android && ./gradlew :app:compileDebugAndroidTestKotlin` passed.

### B-069 Android scrollback panning becomes line-stepped when fully zoomed out

- Status: `needs_verification`
- Area: `android`, `terminal`, `viewport`, `pan`, `scrollback`, `zoom`
- Summary: When fully zoomed out, some sessions lose smooth pixel panning in scrollback and fall back to per-line movement.
- Report:
  The engineer reports that scrolling into the scrollbuffer on some sessions is no longer smooth per-pixel scrolling and is instead per-line. The invariant is that all panning, scrollback entry/reentry, and zoom-related camera movement must preserve pixel-level offsets everywhere.
- Repro:
  1. Use the Android terminal fully zoomed out.
  2. In a session where the live terminal content fits the viewport, drag into scrollback.
  3. Observe that movement only happens after whole-line thresholds instead of preserving partial-cell camera displacement.
- Regression coverage:
  - Extended the existing pixel-continuity regression to cover `DefaultTerminalZoom`, not only zoomed-in mode.
  - Extended the existing reverse-before-snapshot regression to cover `DefaultTerminalZoom`, not only zoomed-in/keyboard-visible modes.
- Verification:
  - `./gradlew :app:compileDebugAndroidTestKotlin`
  - `./gradlew :app:testDebugUnitTest`
  - Connected instrumentation still needs a device/emulator confirmation for the expanded Android view regressions.

### B-068 Android panning/scrollback does not work when fully zoomed out

- Status: `needs_verification`
- Area: `android`, `terminal`, `viewport`, `pan`, `scrollback`, `zoom`
- Summary: At full zoom-out/default zoom, the Android terminal does not allow normal panning, making scrollback and horizontal camera movement impossible or unreliable.
- Report:
  The engineer reports that the scrollbuffer does not work when fully zoomed out and that panning in general does not work at that zoom level.
- Repro:
  1. Use the Android terminal at fully zoomed-out/default zoom.
  2. Try to pan a terminal whose content is wider or taller than the visible viewport, or try to pan upward into scrollback.
  3. Observe that the camera does not pan as expected.
- Regression coverage:
  - Added `terminal_default_zoom_drag_pans_wide_live_content`, an Android view regression proving default/full-zoom drag can pan horizontally when terminal content is wider than the viewport.
  - Added `terminal_default_zoom_drag_can_enter_scrollback_from_live_edge`, an Android view regression proving default/full-zoom drag can request scrollback when the live view is already at the edge.
- Verification:
  - `./gradlew :app:compileDebugAndroidTestKotlin`
  - `./gradlew :app:testDebugUnitTest`
  - Connected instrumentation still needs a device/emulator confirmation for the new Android view regressions.

### B-067 Android input-follow consumes Enter before cursor returns left

- Status: `needs_verification`
- Area: `android`, `terminal`, `viewport`, `input-follow`, `horizontal-camera`
- Summary: With follow-on-read disabled, typing can pan horizontally to keep the cursor visible, but pressing Enter can leave the camera far to the right instead of restoring to the preferred horizontal offset when the cursor returns to the left.
- Report:
  The Android terminal follows the cursor while writing a long line. After pressing Enter, the cursor returns to the left/new prompt, but the camera can remain horizontally panned right with the cursor outside the camera view. Expected behavior is that keyboard-associated cursor movement on Enter restores the horizontal camera to the preferred column, typically `0`, when the cursor fits there.
- Repro:
  1. Use a zoomed terminal where the full terminal width is wider than the phone viewport.
  2. Type far enough to pan the camera right.
  3. Press Enter.
  4. Observe the camera remains right-panned instead of returning to the preferred left column with the cursor visible.
- Regression coverage:
  - Added unit coverage for the input-follow invariant: a pending keyboard follow is not consumed until the terminal cursor actually moves.
  - Added `terminal_keyboard_enter_follow_waits_for_cursor_movement_before_consuming_input`, an Android view regression that simulates typing a long zoomed line, an intermediate redraw before Enter echo, and the final newline cursor at column 0.
- Verification:
  - `./gradlew :app:testDebugUnitTest --tests 'systems.pkt.lingon.terminal.TerminalViewportPolicyTest'`
  - `./gradlew :app:compileDebugAndroidTestKotlin`
  - Targeted `:app:connectedDebugAndroidTest` for the new instrumentation regression was attempted but could not run because Gradle reported `No connected devices!`.

### B-066 Host local PTY long readline line corrupts after resize and Ctrl+A/Ctrl+E

- Status: `resolved`
- Area: `host`, `local-pty`, `resize`, `readline`, `terminal-render`
- Summary: After resizing a Lingon host local PTY, a long readline command initially renders correctly, but `Ctrl+A` cuts/corrupts the last visible line and `Ctrl+E` leaves the prompt/command rendering inconsistent.
- Report:
  The bug is visible in the host local PTY after a terminal window resize. A long command line renders correctly after typing; pressing `Ctrl+A` cuts the last line; pressing `Ctrl+E` does not restore the expected stable rendering.
- Repro:
  1. Start a host local PTY with a bash/readline prompt.
  2. Resize the local PTY terminal viewport.
  3. Type a long command that wraps onto the bottom prompt rows.
  4. Press `Ctrl+A`, then `Ctrl+E`.
  5. Observe the bottom line being cut/corrupted after cursor motion.
- Regression coverage:
  - Added `TestHostResizeReadlineLongLineCtrlACtrlEPreservesWrappedPrompt`, an isolated `ptytest` host/local-PTY regression that resizes a test-owned PTY, fills the viewport, types a wrapped readline command, and asserts `Ctrl+A`/`Ctrl+E` leave the wrapped command text stable.
- Verification:
  - Reproduced the corruption before the fix with the new regression: `Ctrl+A` shifted/truncated the visible wrapped command rows.
  - `go test -tags=integration -count=1 ./integration/pty/session -run 'TestHostResize(ReadlineLongLineCtrlACtrlEPreservesWrappedPrompt|TypingWhileShrunkAfterPsAuxKeepsPromptOnBottomRow)' -v`
  - `go test -tags=integration -count=1 ./integration/pty/session -run 'TestHostResize(ReadlineLongLineCtrlACtrlEPreservesWrappedPrompt|PreservesWideScreenWithoutInput|PreservesWideScreenWithBottomCursorWithoutInput|PreservesScrolledWideOutputWithoutInput|PreservesScrolledWideOutputWithTabBarVisible|TypingWhileShrunkKeepsPromptOnBottomRowWithTabBarVisible|TypingWhileShrunkAfterPsAuxKeepsPromptOnBottomRow|BashClearWhileShrunkThenExpandMatchesControl|LargeViewportClearWhileShrunkThenExpandMatchesControl|LargeViewportClearWhileShrunkThenExpandPsAuxMatchesControl|TypingWhileShrunkThenExpandPreservesCommandLine|TypingAfterExpandMatchesControl)' -v`
  - `go test -count=1 ./internal/session`

### B-065 Android integration runner starts expensive work when no tests are selected

- Status: `resolved`
- Area: `android`, `integration`, `test-harness`, `DX`
- Summary: The Android integration wrapper could start expensive harness/emulator/Gradle work even when `LINGON_IT_ONLY` selected no tests.
- Report:
  The engineer reported JVM/Gradle activity when there were no tests to run. That is wasteful and misleading; an empty or invalid test selection should fail before emulator, harness, or Gradle work starts.
- Repro:
  1. Run the Android integration wrapper with a `LINGON_IT_ONLY` value that does not match an `EndToEndTest` method.
  2. Observe setup work proceeding before the runner discovers there is nothing useful to run.
- Fix:
  - The runner now discovers and filters `EndToEndTest` methods before building the harness, starting the emulator, starting the harness, or invoking Gradle.
  - Invalid `LINGON_IT_ONLY` entries now fail immediately with a specific error.
  - `LINGON_IT_ONLY` now accepts a comma-separated list so related targeted instrumentation tests can run in one Gradle/emulator invocation instead of repeatedly starting and stopping the emulator.
- Verification:
  - `bash -n android/scripts/run-integration-tests.sh` passed.
  - Gradle daemons were stopped with `./gradlew --stop`; `pgrep -af 'Gradle|gradle'` showed no remaining Gradle process except the `pgrep` command itself.
  - Per engineer instruction, no tests or integration runs were executed for this fix.

### B-064 Android terminal viewport can show stale oversized content after initial/IME resize

- Status: `needs_verification`
- Area: `android`, `terminal`, `viewport`, `IME`, `render`
- Summary: When the IME is already visible before terminal content finishes loading, long session content can initially render as if the terminal viewport still extends below the visible app viewport. Hiding/showing the IME does not visibly correct the camera until the user pans.
- Report:
  The engineer reports that sufficiently long sessions consistently start with terminal content cropped below the bottom of the visible viewport. With the IME already up before session load, the terminal appears to load underneath the keyboard; toggling the keyboard does not move the viewport until a pan/touch forces another redraw.
  After the first IME overlay fix, screenshots `~/Desktop/crap1.png` and `~/Desktop/crap2.png` showed the remaining production bug: with the keyboard hidden, a zoomed tall session rendered below the phone's usable bottom/system navigation area; showing the keyboard made the content align to the keyboard top. The initial load camera also appeared nondeterministic instead of live-bottom oriented while the session was still loading.
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
  - A direct Android instrumentation harness reproduced that overlay condition by mounting `TerminalGridView` full-height behind a real soft IME. The first run failed because bottom-anchored partial-row accounting reported the last row outside the visible range (`expected 240, got 239`), which matches the user-visible bottom content being treated as below the camera.
  - The hidden-keyboard screenshot showed the same boundary bug against the system/navigation bottom, not only against the IME. Existing coverage incorrectly asserted that a hidden IME should always restore the full physical terminal viewport; that is false when the physical view extends under system bars.
  - The first-frame loading path also skipped initial live-bottom camera placement while `isLoading` was true. That let syncing/reconnect frames render from the default or stale camera until a later non-loading update.
  - Strengthening the multi-session tall-session test exposed a related policy conflict: default live mode could still choose cursor-follow anchoring when the cursor was far above trailing content. That is wrong for initial/reconnect/session-load display because passive reads with follow-on-read disabled should show the live content bottom, not chase an old cursor row.
- Fix:
  - `TerminalGridView.onSizeChanged()` now invalidates immediately after recomputing layout and applying any pending restore.
  - The terminal panel, terminal viewport box, embedded Android terminal view, and quick-key bar are now clipped to their Compose bounds.
  - Keyboard integration helpers now focus `TerminalFocus`, matching the production tap path instead of clicking the hidden input node.
  - `TerminalGridView` now derives its effective camera viewport height from the actual visible portion of the view above the current IME inset. If Compose has already resized the view above the IME, this is a no-op; if Android leaves the view behind an overlay keyboard, camera layout, live-bottom anchoring, panning, clipping, restore, and debug visibility use the reduced visible height.
  - Partial terminal rows now count as visible for camera/visible-range calculations, so a bottom-anchored viewport does not drop the final row when the visible height is not an exact multiple of cell height.
  - `TerminalGridView` now bounds the effective camera viewport by the lower of the IME and navigation/system-bar visible bottoms. Hidden-keyboard rendering no longer assumes the full physical view height when the physical view extends behind the bottom system inset.
  - Initial live-bottom camera placement now runs as soon as a snapshot and dimensions exist, even while the session is marked loading/syncing. Loading still suppresses read-driven cursor follow, but it no longer leaves the first content frame at an arbitrary camera offset.
  - Default live mode now bottom-anchors initial/passive display even when the cursor is above trailing content. Cursor-follow remains tied to keyboard input when follow-on-read is disabled, or to the explicit follow-on-read toggle when enabled.
  - Resetting zoom/pan now also clears any pending cached viewport restore, so a stale restore cannot override the requested live-bottom reset.
  - Refactored `TerminalGridView` camera behavior into explicit viewport modes: `LiveBottom` for initial/reconnect/passive content display, `Manual` for user pan/zoom/scrollback/restored manual camera, and `CursorFollow` for local input or the optional follow-on-read mode. Persisted zoom no longer implies manual camera and can fresh-load at the live bottom.
  - Removed dead viewport policy helpers for the old default-zoom cursor-follow decision so tests no longer preserve stale behavior that production no longer uses.
  - Lifecycle capture now records a pending keyboard-input cursor-follow frame as its durable return mode when follow-on-read is disabled, so a stop/dispose before the draw pass cannot persist transient cursor-follow as the session camera.
  - Restoring a live-bottom viewport clears scroll remainder instead of reviving stale manual scroll state.
  - Detached Android terminal views now preserve the last measured effective viewport height for lifecycle/dispose capture, so app-lock/unlock does not save the full physical view height after system-bar insets disappear.
- Regression coverage:
  - Added `terminal_resize_invalidates_after_live_bottom_reanchor`, which asserts a terminal resize both reanchors the live viewport to the terminal bottom and records a resize invalidation before any pan/touch path can redraw it.
  - Tagged the quick-key container and strengthened keyboard/tall-session instrumentation coverage to assert the terminal viewport bottom never overlaps the quick-key top when the IME controls are visible.
  - Added `soft_keyboard_inset_keeps_terminal_viewport_above_keyboard`, which forces a real soft IME on the emulator, loads a 240-row session, focuses the real terminal surface, and asserts the effective terminal camera viewport is bounded by the visible area above the IME, stays live-bottom anchored, and does not move during the settle window.
  - Added `terminal_view_reanchors_when_soft_keyboard_overlays_physical_view`, which mounts the real `TerminalGridView` behind a real soft IME, reproduces the overlay condition, and asserts keyboard show/hide/show changes the effective camera viewport and remains bottom-anchored without any pan.
  - The overlay regression now also samples for 1.2 seconds after each IME transition and fails on any flicker in physical height, effective viewport height, camera Y, visible start row, or visible end row.
  - Strengthened `terminal_view_reanchors_when_soft_keyboard_overlays_physical_view` so hidden-IME overlay state asserts the effective camera viewport is bounded by the system-visible bottom instead of the full physical view.
  - Added `loading_terminal_content_initializes_at_live_bottom_even_when_cursor_is_above_bottom`, which creates a zoomed tall terminal snapshot with `isLoading=true`, a cursor near the top, and asserts the first content frame is already live-bottom anchored and renders through the final row.
  - Added `lifecycle_capture_does_not_persist_transient_keyboard_cursor_follow`, which captures after local input arms cursor-follow but before draw consumes it, then restores across a height change with the cursor above the bottom and asserts live-bottom anchoring is preserved.
  - Added `loaded_tall_session_uses_system_visible_bottom_before_and_after_keyboard_toggle`, which loads a real tall host session, zooms it so content exceeds the camera, and asserts hidden keyboard, shown keyboard, and hidden-again states all use the system-visible bottom and remain live-bottom anchored without a pan.
  - Kept live-bottom behavior covered at the `TerminalGridView` boundary instead of the older policy helper boundary, so the regression asserts the observable camera mode rather than a removed implementation detail.
  - Reran `terminal_cursor_follow_defaults_to_keyboard_input_only` to ensure passive read-driven cursor movement still does not move the camera when follow-on-read is disabled.
  - Reran `app_lock_unlock_preserves_terminal_camera_viewport` to verify app-lock disposal/restoration keeps the manual camera after detachment.
- Verification:
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed after the clipping fix.
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed after the effective IME viewport fix.
  - `cd android && LINGON_IT_ONLY=soft_keyboard_inset_keeps_terminal_viewport_above_keyboard make integration-test` passed. Resource profile: `android/test-artifacts/resource-profile-20260512-015200-4064878/summary.txt`.
  - `cd android && LINGON_IT_ONLY=app_lock_unlock_preserves_terminal_camera_viewport make integration-test` passed after the detached viewport capture fix.
  - `cd android && LINGON_IT_ONLY=keyboard_hide_show_preserves_bottom_anchor_for_tall_sessions make integration-test` passed. Resource profile: `android/test-artifacts/resource-profile-20260512-015323-4070926/summary.txt`.
  - `cd android && LINGON_IT_ONLY=keyboard_visible_before_background_is_restored_after_resume make integration-test` passed. Resource profile: `android/test-artifacts/resource-profile-20260512-015506-4077816/summary.txt`.
  - `cd android && LINGON_IT_ONLY=terminal_view_reanchors_when_soft_keyboard_overlays_physical_view make integration-test` failed before the partial-row fix, then passed. Passing resource profile: `android/test-artifacts/resource-profile-20260512-022202-4170918/summary.txt`.
  - `cd android && LINGON_IT_ONLY=soft_keyboard_inset_keeps_terminal_viewport_above_keyboard make integration-test` passed after the partial-row fix. Resource profile: `android/test-artifacts/resource-profile-20260512-022343-4177623/summary.txt`.
  - `cd android && LINGON_IT_ONLY=terminal_view_reanchors_when_soft_keyboard_overlays_physical_view make integration-test` passed after adding the no-flicker settle-window assertions. Resource profile: `android/test-artifacts/resource-profile-20260512-070023-531523/summary.txt`.
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed after the hidden-system-bottom and loading-camera fixes.
  - `cd android && LINGON_IT_ONLY=loading_terminal_content_initializes_at_live_bottom make integration-test` passed. Resource profile: `android/test-artifacts/resource-profile-20260512-103522-1006654/summary.txt`.
  - `cd android && LINGON_IT_ONLY=loaded_tall_session_uses_system_visible_bottom_before_and_after_keyboard_toggle make integration-test` first failed because the fixture fit all 240 rows and did not reproduce the zoomed tall-session condition, then passed after forcing zoom. Passing resource profile: `android/test-artifacts/resource-profile-20260512-103926-1021585/summary.txt`.
  - `cd android && LINGON_IT_ONLY=terminal_view_reanchors_when_soft_keyboard_overlays_physical_view make integration-test` passed after replacing the hidden-IME full-height assertion. Resource profile: `android/test-artifacts/resource-profile-20260512-104407-1038833/summary.txt`.
  - `cd android && LINGON_IT_ONLY=keyboard_hide_show_preserves_bottom_anchor_for_tall_sessions make integration-test` failed after adding system-visible-bottom assertions, then passed after live-mode bottom anchoring and pending-restore reset fixes. Passing resource profile: `android/test-artifacts/resource-profile-20260512-110441-1106104/summary.txt`.
  - `cd android && LINGON_IT_ONLY=loaded_tall_session_uses_system_visible_bottom_before_and_after_keyboard_toggle make integration-test` passed again after the broader live-mode fix. Resource profile: `android/test-artifacts/resource-profile-20260512-110642-1113905/summary.txt`.
  - `cd android && LINGON_IT_ONLY=terminal_cursor_follow_defaults_to_keyboard_input_only make integration-test` failed when the first broader fix moved passive read camera state, then passed after limiting the live-bottom snap to initial live positioning. Passing resource profile: `android/test-artifacts/resource-profile-20260512-111118-1129933/summary.txt`.
  - `cd android && ./gradlew :app:compileDebugKotlin :app:compileDebugAndroidTestKotlin` passed after the explicit viewport-mode refactor.
  - `cd android && ./gradlew :app:testDebugUnitTest` passed after the explicit viewport-mode refactor.
  - `cd android && LINGON_IT_ONLY=loading_terminal_content_initializes_at_live_bottom_even_when_cursor_is_above_bottom,terminal_cursor_follow_defaults_to_keyboard_input_only,loaded_tall_session_uses_system_visible_bottom_before_and_after_keyboard_toggle,terminal_view_reanchors_when_soft_keyboard_overlays_physical_view make integration-test` passed in one instrumentation batch on one cgroup-contained emulator. Resource profile: `android/test-artifacts/resource-profile-20260512-161405-1851959/summary.txt`; peak CPU `2.09` cores, average CPU `1.33` cores, peak cgroup memory current `7226732544` bytes.
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed after removing unused legacy viewport-policy helpers.
  - `cd android && ./gradlew :app:testDebugUnitTest :app:compileDebugAndroidTestKotlin` passed after hardening transient cursor-follow capture.
  - `cd android && LINGON_IT_ONLY=lifecycle_capture_does_not_persist_transient_keyboard_cursor_follow,loading_terminal_content_initializes_at_live_bottom_even_when_cursor_is_above_bottom,terminal_cursor_follow_defaults_to_keyboard_input_only,loaded_tall_session_uses_system_visible_bottom_before_and_after_keyboard_toggle,terminal_view_reanchors_when_soft_keyboard_overlays_physical_view make integration-test` passed in one instrumentation batch on one cgroup-contained emulator. Resource profile: `android/test-artifacts/resource-profile-20260512-163727-1914329/summary.txt`; peak CPU `2.07` cores, average CPU `1.30` cores, peak cgroup memory current `6925447168` bytes.
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
  - `LINGON_IT_ONLY=logout_clears_viewport_cache_and_reused_session_id_does_not_restore_stale_camera make integration-test` passed. Resource profile: `android/test-artifacts/resource-profile-20260511-091517-1858597/summary.txt`.
  - `LINGON_IT_ONLY=app_lock_unlock_preserves_terminal_camera_viewport make integration-test` passed. Resource profile: `android/test-artifacts/resource-profile-20260511-091645-1864750/summary.txt`.
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
  - `LINGON_IT_ONLY=app_lock_unlock_preserves_terminal_camera_viewport make integration-test` passed. Resource profile: `android/test-artifacts/resource-profile-20260511-050339-1354744/summary.txt`.
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
