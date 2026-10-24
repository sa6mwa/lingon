# Session creation and display names

## Lifecycle contract

Owner clients can create a detached terminal on a connected session's machine
or rename that session. Local headless clients use the existing owner-permissioned
Unix socket; remote clients use authenticated `POST /sessions/manage`. Share
credentials and other accounts cannot manage the session, regardless of their
terminal control lease. The relay does not execute programs on its own machine.

Creation reexecutes the host's Lingon executable with `-x`, using Go's `os/exec`
and a separate Unix process session. Standard input/output/error point to
`/dev/null`; a child-owned pipe reports successful socket readiness. The child
survives the creating client and original host. Startup failure or cancellation
before readiness terminates the child. No in-process fork is needed.

The host constructs all executable arguments. It forwards the selected config
root/file, shell, terminal type, current geometry and publishing state,
respawn/scrollback settings, relay authentication and TLS trust directory.
The child inherits the host process's startup working directory,
not the selected shell's current directory. Embedded hosts can specify
`InteractiveOptions.HeadlessExecutable` when their own executable is not the
Lingon CLI; `ConfigDir` and `ConfigFile` select the child configuration.

Child creation reuses the host's selected auth file. Token-only hosts pass the
current credential through a private inherited pipe; the child consumes and
closes it before starting the terminal. Generated child arguments and environment
contain no access token. Explicit user-supplied CLI token flags retain their
existing semantics.

Ordinary headful `lingon` retains its in-process `Ctrl+l c` tabs. Attached clients
create detached sessions with `Ctrl+l c`. CLI rename is `Ctrl+l e`; Android puts
both actions in Settings, and the Web UI puts them in its session menu. Only the
creating client selects the new session; other clients discover it normally.

## Identity and retries

Display names are independent of immutable session IDs and socket filenames.
Renaming changes neither connections nor share tokens. Headless names persist
in local state; interactive names last for the running host process. Publishers
register their current name on reconnect. Names contain 1–80 Unicode code points,
with surrounding whitespace trimmed by UIs and control/format characters rejected.
Duplicate display names are allowed; IDs disambiguate sessions.

Each operation has a client-generated request ID. A host serializes lifecycle
work, coalesces matching duplicates and retains the last 256 completed receipts.
Conflicting reuse of an ID is rejected. Creation derives a stable child ID from
the source session and request ID and checks existing live headless records.
This is bounded deduplication, not an indefinitely durable transaction journal.
Clients never automatically retry an uncertain mutation. A missing response
requires refreshing session discovery before another user action.
An explicit HTTP 401 precedes execution: the Web UI may refresh owner credentials
and retry once with the same request ID and payload. Failed refresh, a second
401, other HTTP errors, and missing or malformed responses do not retry mutations.
Attach gives refresh and mutation one 15-second budget using its injected clock;
operation timeout does not cancel the terminal session.

Management messages use a host-only protocol envelope and never enter terminal
replay history. Historical rename receipts acknowledge the old request without
overwriting newer display metadata. Host disconnect/replacement releases pending
relay waiters with an uncertain-outcome result. Queues, waiter counts and response
sizes are bounded, and a separate publisher worker keeps process startup outside
the terminal input reader.

## Verification

The feature has unit coverage for validation, busy/conflicting requests, bounded
receipts, duplicate coalescing, interrupted requests, stale host responses,
historical rename acknowledgements, owner authorization and uncertain-response
handling. Name editing never renders pasted control/format characters. Local
selection sorts display names with stable IDs as tie-breakers. Host registration
installs initial metadata before reading runtime name updates, and remains able
to answer WebSocket pings while disk persistence is blocked. A real HTTP/WebSocket
test holds the lifecycle worker blocked and proves
terminal input reaches the host before that worker is released.

Actual CLI tests use `ptytest`-owned child PTYs to exercise local/relay creation,
Unicode rename with stable sockets, duplicate creation, reconnect metadata,
selected-child input and child survival after parent exit. Browser tests run the
real detached executable and relay, exercise menu/dialog actions, and verify
selection and readiness. Android targeted instrumentation exercises both actions,
Unicode rename, terminal input after creation/rename and hidden share-session
actions. Existing no-flicker regression assertions remain intact.

### Focused checks

Run the lifecycle regression tests, replay benchmark and UI checks serially:

```sh
bash scripts/verify-contained.sh go test -tags=integration -p 1 ./integration/pty/attach -run '^TestSessionManagement' -count=1 -v
bash scripts/verify-contained.sh go test ./internal/relay -run '^$' -bench '^BenchmarkWSReplay$' -benchmem -count=3 -benchtime=1s
bash scripts/verify-contained.sh go test -tags=integration -p 1 ./integration/webui -run '^TestWebUISessionManagement$' -count=1
LINGON_IT_ONLY=settings_create_terminal_and_rename_session,settings_session_management_hidden_for_share bash scripts/verify-contained.sh bash android/scripts/run-integration-tests.sh
```

PTY interaction is isolated inside test-owned terminals. The Android runner owns
and cleans up its harness, detached terminals, emulator and adb server; the above
commands share the enforced aggregate ceiling of 4 CPUs / 4 GiB RAM and zero swap.

`TestSessionManagementStartupPerformance` compares five alternating direct
`lingon -x` launches and routed creations of the same binary. The optional
`TestSessionManagementStartupBaselineComparison` requires a separate baseline
executable through `LINGON_TEST_STARTUP_BASELINE_BINARY`. Benchmark samples
describe the current host and load, not a latency guarantee. Keep measurements
and run results under ignored `build/verification/`, rather than in this document.

These focused commands do not replace the full [release gate](RELEASE.md).
