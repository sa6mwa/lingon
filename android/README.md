# Lingon Android app

The Lingon Android app is a native companion to the web UI, built for fast
session work on the go while staying consistent with the web and SSH flows.

## Renderer architecture note

CLI host/attach TUIs use a full-framebuffer MVU runtime (`internal/mvu`). The
Android app does not embed that renderer; it
shares Lingon protocol/session semantics but keeps its own Android-native UI
rendering pipeline.

## Prereqs
- Java 21+
- Linux amd64 (SDK installer script is Linux-only at this time)

## Quick start (debug)
```bash
cd android
make sdk          # install Android SDK + emulator (API 35 target + API 36 compile)
make avd          # create an AVD under ~/.android/avd (host keyboard enabled)
make emulator     # start the emulator (forces keyboard + IME settings + adb reverse)
make build        # assemble debug APK (signed with debug key)
make install      # install debug APK
make run          # launch the app
```

Output APK:
- Debug: `android/app/build/outputs/apk/debug/app-debug.apk`

## Release APK (signed)
`make release` generates a local development keystore (if one does not exist),
creates `android/signing.properties`, and builds a signed release APK. This is
intended for local installs only; replace it with your own keystore for any
distribution.

```bash
cd android
make release
```

Output APK:
- Release: `android/app/build/outputs/apk/release/app-release.apk`

Files created (git-ignored):
- `android/.keystore/lingon-release.jks`
- `android/signing.properties`

### Use your own signing key
Generate a keystore:
```bash
keytool -genkeypair -v \
  -keystore /absolute/path/to/lingon-release.jks \
  -alias lingon \
  -keyalg RSA -keysize 2048 -validity 10000 \
  -storepass "your-store-pass" -keypass "your-key-pass" \
  -dname "CN=Lingon, OU=Dev, O=Lingon, L=Local, S=Local, C=US"
```

Create `android/signing.properties`:
```properties
storeFile=/absolute/path/to/lingon-release.jks
storePassword=your-store-pass
keyAlias=lingon
keyPassword=your-key-pass
```

Then build:
```bash
cd android
make release
```

## UI tests
UI tests run on a Gradle Managed Device (API 35) and require a Lingon backend
reachable from the emulator.

```bash
cd android
make ui-test
```

Notes:
- Start the backend on the host before running UI tests (default `lingon serve` on `:12843`).
- The emulator reaches the host via `https://localhost:12843/v1` (via `adb reverse`).

## Integration tests (fully automated)
The harness-driven integration suite spins up an in-process Lingon server with
two host sessions, wires the emulator, injects the CA cert, and runs the
instrumentation suite end-to-end.

```bash
cd android
make integration-test          # uses PRESET=medium by default
make integration-test PRESET=pixel7
```

Artifacts (screenshots + debug info) are pulled to `android/test-artifacts/`.

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
- The installer fetches the Android 15 AOSP system image (API 35) plus the API 36 platform for compileSdk.
- The app default endpoint is `https://localhost:12843/v1` and can be changed in-app.
- HTTPS is required; endpoints must start with `https://`.
- `make emulator` configures `adb reverse tcp:12843` so `localhost` hits the host backend.
- Manage certificates in-app via the top-right menu. Certificates are stored per endpoint.
- Debug builds accept a broadcast to add a PEM cert (useful for integration tests):
  - Action: `systems.pkt.lingon.DEBUG_ADD_CERT`
  - Extras: `pem` (string, required), `endpoint` (string, optional)

## Parity caveats

- Functional parity (connect, attach, control, share, cert trust) is the
  target; exact row/overlay timing parity with CLI TUI is not guaranteed.
- CLI anti-flicker invariants are guarded by Go integration tests in
  `internal/session` and `internal/attach`; Android has its own UI test suite
  and may differ in visual behavior while still being protocol-correct.
