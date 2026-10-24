# Lingon Android app

The Lingon Android app is a native companion to the web UI, built for fast
session work on the go while staying consistent with the web and CLI flows. It
supports session switching, sharing, certificate trust, per-session zoom,
smooth terminal panning through scrollback, and optional background wall
notifications.

## Renderer architecture note

CLI host/attach TUIs use a full-framebuffer MVU runtime (`internal/mvu`). The
Android app does not embed that renderer; it
shares Lingon protocol/session semantics but keeps its own Android-native UI
rendering pipeline.

## Terminal keyboard and selection

- Typing reclaims terminal control automatically for account sessions and
  control-enabled shares, preserving the first key. View-only shares cannot
  send input or take control. No separate control button is needed.
- Tap **ALT** once for the next Alt chord. Tap it again to enter **FN** mode;
  tap **FN** to return to ALT off. FN stays on after a key. In FN mode, the
  existing CTRL button cycles off → CTRL → ALT → C+A → off for function-key
  modifiers. The portrait quick-key `/` button becomes a persistent SHFT toggle.
- In FN mode, `1`–`9` send F1–F9 and `0` sends F10. The portrait quick-key
  PGUP and PGDN positions send F11 and F12. Physical F1–F12 keys also work,
  including Ctrl+Alt chords. Other IME text retains its Unicode characters,
  including emoji and supplementary characters, while FN remains enabled.
- Entering FN also enables text selection. Drag one finger over terminal cells
  to select; selected cells have a white background. Drag two fingers to pan or pinch to
  zoom while selection is active. Tap the terminal to show the keyboard again.
  Leaving FN clears the selection.
- FN+C copies the selected text to Android's clipboard. FN+V pastes the current
  Android clipboard contents into the active terminal, including after switching
  sessions.

## Prereqs

- JDK 21, with `java` and `keytool` on `PATH`.
- Go 1.26 or newer and Make; the Make targets query the root Go CLI/lifecycle
  for version information, and the integration harness also builds Go tools.
- Linux amd64 for the installer and x86_64 emulator image, with hardware
  virtualization available for emulator tests.
- `curl` and `unzip` for SDK installation; `openssl` for release signer checks.
- A working systemd user manager with cgroup v2 for contained verification.

Use the checked-in Gradle wrapper. Compilation uses SDK platform 37.0;
`targetSdk` is 35 and `minSdk` is 26. The test emulator runs API 35. These are
separate settings: an Android 16 phone can run the app without matching the
compile SDK.

## Quick start (debug)

```bash
cd android
make sdk          # install SDK platform 37.0, Build Tools 36.0.0 + API 35 emulator
make avd PRESET=medium
make emulator PRESET=medium
```

The emulator runs in the foreground. In a second terminal, enter `android/` and
run:

```bash
make build        # assemble debug APK (signed with debug key)
make install      # install debug APK
make run          # launch the app
```

`make emulator` changes the normal development adb/emulator profile and sets up
keyboard, IME and adb reverse. Automated verification uses the isolated
`integration-test` runner below instead.

Output APK:

- Debug: `android/app/build/outputs/apk/debug/app-debug.apk`

## Release APK (signed)

`make release` builds a local signed APK and verifies its signer against
`android/lingon-release-cert.pem`. It preserves an existing
`android/signing.properties`. If that file is absent, its helper generates
local development signing configuration and, if necessary, a development key.
A newly generated key will not match the pinned release certificate, so this
does not provision a fresh checkout for an official release.

For official builds, configure the existing private key matching that public
certificate. Preserve that signing identity so installed apps can accept
updates; keep the private keystore and passwords backed up outside Git. The
checked-in PEM contains only the public certificate.

```bash
cd android
make release
```

Output APK:

- Release: `android/app/build/outputs/apk/release/app-release.apk`

Files created (git-ignored):

- `android/.keystore/lingon-release.jks`
- `android/signing.properties`

### Configure the release key

Create ignored `android/signing.properties`, pointing at the existing keystore:

```properties
storeFile=/absolute/path/to/lingon-release.jks
storePassword=your-pass
keyAlias=lingon
keyPassword=your-pass
```

For PKCS12 keystores, use the same store and key password. Then build:

```bash
cd android
make release
```

The Make target obtains version name/code from the lifecycle's exact lightweight
tag policy. Untagged local builds use `v0.0.0` and code `1`; they are rehearsals.
Raw Gradle defaults are not release identity. Official squash/tag, verification,
packaging and publication use the root [release lifecycle](../RELEASE.md).

Install a specific verified APK without rebuilding it:

```bash
make install-release APK=/absolute/path/to/lingon-X.Y.Z-android.apk ADB_SERIAL=PHONE
```

## UI tests

`make ui-test` is a developer Gradle Managed Device entry point (API 35), not the
release gate. It requires a Lingon backend and the relevant credentials, trusted
CA and session runner arguments. Use the fully automated integration runner for
reproducible verification; it supplies those inputs and owns device cleanup.

Runner inputs use the `-Plingon.it.*` Gradle properties declared in
`app/build.gradle.kts` and can be passed through `UI_TEST_FLAGS`. The managed
device also needs a network route to that endpoint; `localhost` requires an
adb reverse mapping. Starting an ordinary relay alone does not populate the
test fixtures.

## Integration tests (fully automated)

The harness-driven integration suite spins up an in-process Lingon server with
two host sessions, wires the emulator, injects the CA cert, and runs the
`EndToEndTest` instrumentation class by default. Other instrumentation classes
require explicit selection. Root `make prerelease` discovers and runs all
classes plus the separate cleartext case as part of the full release gate.

```bash
cd android
make integration-test          # uses PRESET=medium by default
make integration-test PRESET=pixel7
```

Artifacts (screenshots + debug info) are pulled to `android/test-artifacts/`.
The integration runner keeps its AVD, emulator settings, adb key, and snapshots
under the ignored `android/build/integration-emulator/` directory. It sets an
isolated Android user home and adb server port so the run does not read or
change the normal `~/.android` profile. The SDK installation and existing
Gradle/Go dependency caches remain shared. The runner chooses a free isolated
adb server port; set `LINGON_IT_ADB_SERVER_PORT` to choose one explicitly.

The runner enforces the shared 4 CPU / 4 GiB ceiling with zero swap. It builds
APKs before starting an owned emulator (2 vCPUs, 1024 MiB guest RAM,
720×1600 at 280 dpi, host GPU without a host window or Vulkan). Verification
Gradle tasks use a 512 MiB heap; APK assembly uses 1536 MiB and runs separately
from the emulator. Run all verification serially.

The runner resets app state between instrumentation batches of at most ten
methods and stops its emulator and adb server on exit. Existing devices cannot
be reused, and `LINGON_IT_KEEP_EMULATOR=1` is ignored in contained runs.

Run a single instrumentation test with:

```bash
LINGON_IT_ONLY=foreground_manual_wall_delivery_does_not_post_system_notification make integration-test
LINGON_IT_CLASS=systems.pkt.lingon.terminal.TerminalGridViewInstrumentedTest#selectionDragCopiesCellsWithWhiteHighlightAndTwoFingerPanPreservesSelection make integration-test
```

Verify the debug/local HTTP share fallback with an isolated cleartext relay:

```bash
LINGON_IT_CLEARTEXT=1 LINGON_IT_ONLY=cleartext_share_authentication_uses_token_fallback make integration-test
```

The harness uses HTTPS by default. This HTTP-specific case is skipped in the
default HTTPS sweep and must run with the explicit cleartext setting.

## Emulator presets

Use `PRESET` to select a device profile:

```bash
make PRESET=small emulator
make PRESET=medium emulator
make PRESET=pixel7 emulator
make PRESET=pixel9 emulator
```

List presets:

```bash
make list-presets
```

## Notes

- SDK installs to `~/Android/Sdk` by default (override with `ANDROID_SDK_ROOT`).
- The installer fetches the Android 15 AOSP system image (API 35), SDK platform
  37.0 for compilation, and Build Tools 36.0.0. The app's minimum Android API
  remains 26; the phone does not need to match the compile SDK.
- The app default endpoint is `https://localhost:12843/v1` and can be changed in-app.
- HTTPS is required; endpoints must start with `https://`.
- Shared-session authentication retries network errors and HTTP 408, 429, and
  5xx responses automatically, using reconnect backoff and the server's
  Retry-After interval. Invalid-token responses remain errors requiring a valid
  token.
- `make emulator` configures `adb reverse tcp:12843` so `localhost` hits the host backend.
- Manage certificates in-app via the top-right menu. Certificates are stored per endpoint.
- Zoom is stored per endpoint/session and terminal panning remains pixel-based
  across live and scrollback content.
- Foreground wall messages are shown in-app and do not post Android system
  notifications.
- Background wall notifications are optional. When enabled, the app keeps a
  foreground service active while backgrounded and polls the relay for wall
  messages; polling is skipped while the app is foregrounded.
- Debug builds accept a broadcast to add a PEM cert (useful for integration tests):
  - Action: `systems.pkt.lingon.DEBUG_ADD_CERT`
  - Extras: `pem` (string, required), `endpoint` (string, optional)

## Parity caveats

- Functional parity (connect, attach, control, share, cert trust) is the
  target; exact row/overlay timing parity with CLI TUI is not guaranteed.
- CLI anti-flicker invariants are guarded by Go integration tests in
  `internal/session` and `internal/attach`; Android has its own UI test suite
  and may differ in visual behavior while still being protocol-correct.
