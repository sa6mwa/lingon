## PRD: **Lingon** — Interactive terminal that is shareable across devices (Go binary + relay + web UI + native Android)

### 0) One-sentence summary

**Lingon** is a Go-based interactive terminal application that you run like a normal terminal (`lingon`) while it transparently publishes the same authoritative VT session to other UIs (web xterm.js and native Android) via a secure HTTPS/WSS relay (`lingon serve`), enabling seamless device switching with single-controller leasing, per-client viewport rendering (crop/pad), optional client auto-fit to authoritative winsize, and automatic token refresh.

---

## 1) Goals

### Primary goals

1. **Local-first interactive terminal**: Running `lingon` in a terminal is a full interactive shell experience, comparable to xterm/vterm/tmux, not a background daemon.
2. **Seamless device switching**: Start typing locally, then continue on phone, then back—same session state.
3. **Multi-attach synchronization**: Multiple clients may attach concurrently; all see consistent terminal state.
4. **Authoritative VT state**: One component is authoritative for screen state and can snapshot/resync other UIs.
5. **Secure-by-default**: HTTPS/WSS only; login via username + password + TOTP; long-lived refresh tokens; short-lived access tokens.
6. **Decent view across unequal window sizes**: Clients with smaller or larger windows must render a correct view without garbled wrapping; cropping/letterboxing is acceptable.
7. **Auto-fit UX for GUI clients**: Web and Android should be able to automatically fit their rendering to the authoritative canonical grid for readability, without changing the canonical PTY size.
8. **Small system**: Single Go binary provides both interactive mode and relay mode; web UI served by relay; native Android app.
9. **Terminal safety**: Exiting `lingon` must restore the user's terminal (cooked mode, echo, signal handling); no corruption or orphaned raw mode.

### Non-goals (v1)

* Simultaneous multi-writer collaboration (two people typing concurrently). Only one controller at a time.
* tmux feature parity (panes, splitting, session groups) **and no tmux-client implementation**.
* External IdP/OIDC integration.
* Pure P2P connectivity (relay required in v1).

---

## 2) Personas and user stories

### Personas

* **User**: runs `lingon` locally and occasionally attaches from phone/browser.
* **Admin**: runs `lingon serve` and manages users.

### Core user stories

1. I start `lingon` and immediately have a normal interactive shell in my terminal.
2. I attach from Android and take control without losing state.
3. I attach from web as a viewer while controlling locally.
4. I attach from another terminal client (`lingon attach`) to an already-running session (local or remote).
5. If a device disconnects, it can reconnect and recover the exact screen state.
6. As admin, I can create/delete users, set passwords, rotate TOTP, revoke tokens.
7. If my attach window is smaller than the authoritative session, I still see a correct view (cropped is OK, but no garbled wrapping).
8. If my attach window is larger than the authoritative session, the authoritative session is rendered smaller with padding/margins rather than being “stretched”.
9. In Android and web, I can enable **Auto-fit** so the entire authoritative screen fits within my device viewport (via font-size/zoom scaling), and if it becomes too small to read I can zoom and pan.

---

## 3) Deliverables

1. **Go binary** `lingon` providing:

   * `lingon` (interactive mode; local terminal UI + hosted session publisher)
   * `lingon serve` (relay/server mode; HTTPS/WSS + embedded web UI + user management)
   * `lingon attach` (terminal client; attaches to an existing session via relay)
   * `lingon login` (interactive login to relay; stores tokens locally)
   * `lingon tls …` (TLS/CA management utilities)
   * `lingon bootstrap` (initialize defaults: TLS + config)
   * `lingon users …` (admin-only; server-side user management)
2. **Web UI** (xterm.js) served by `lingon serve`
3. **Native Android app**

   * full-screen terminal UI + special keyboard
   * endpoint config + login
   * device-lock gating on resume
   * zoom + pan + auto-fit modes

---

## 4) System overview and roles

### 4.1 Interactive mode (`lingon`)

When user runs `lingon` in a local terminal:

* It behaves like a terminal emulator hosting a shell:

  * allocates a PTY
  * starts the user’s login shell attached to PTY slave
  * renders the resulting terminal session in the current terminal window
* It simultaneously connects to a relay endpoint (configured or provided) and publishes the same session to other clients.

This mode is **foreground**, interactive, and should be indistinguishable from a normal terminal experience in responsiveness and behavior.

**Clarification (important):** the local UI **must not** be implemented by attaching to the relay and re-rendering the session via the network path. The local terminal rendering must be driven directly from the local PTY + headless VT emulator, with the relay used only as a **side-channel for publishing** to remote clients. Local interactivity must continue even if the relay is down.

### 4.2 Relay/server mode (`lingon serve`)

The relay is responsible for:

* HTTPS/WSS termination
* authentication and token issuance/refresh
* brokering multi-client attachments to a published session
* enforcing or coordinating controller lease and resize policy
* serving the embedded web UI

### 4.3 Remote UIs

* Web UI and Android app are additional UIs that attach to a session published by an interactive `lingon` instance.

### 4.4 Remote CLI attach (`lingon attach`)

* `lingon attach` is a terminal client that connects to the relay and attaches to an existing session.
* It renders in a local terminal (not a web UI) and can request control under the controller lease rules.
* It may attach to:

  * the caller's own active session, or
  * another user's session if explicitly permitted by server policy (see sharing policy decisions).

---

## 5) Terminal session model

### 5.1 PTY-backed shell (hosted by interactive `lingon`)

* `lingon` allocates a PTY and launches the user’s login shell.
* The user’s login shell is resolved from OS account data (e.g., `/etc/passwd`) with an override flag/config.

### 5.2 Authoritative VT state (recommended authority location)

**Authoritative state lives with the interactive `lingon` process** (the one that owns the PTY), because:

* it is the source of truth for PTY output
* it can generate snapshots without round-tripping through the relay
* it can keep working even if relay briefly disconnects (buffering/reconnect)

The interactive `lingon` process runs a headless VT emulation engine and feeds it every PTY output byte:

* maintains main/alt screens, cursor, attributes, modes, scrollback (bounded)

**Local UI path:** the local terminal UI renders directly from this authoritative state; it is not an attach client and does not round-trip through the relay.

### 5.3 Canonical grid + per-client viewports (mandatory for “no garble”)

**A single canonical terminal grid (rows×cols) exists for the session at any point in time.** This grid corresponds to the PTY winsize and is the basis for all rendering.

* The authority maintains the canonical grid state via headless VT emulation.
* Attached clients do **not** independently wrap/reflow output based on their own window sizes.
* Instead, clients render a **viewport** of the canonical grid into their local window:

  * If the client window is **smaller** than the canonical grid: it crops (viewport/pan), never reflows.
  * If the client window is **larger** than the canonical grid: it pads/letterboxes with blank margins.

This ensures:

* no “double wrapping”
* no garbled output when multiple viewers differ in size
* deterministic rendering across clients

### 5.4 Auto-fit rendering for GUI clients (web + Android)

Web and Android may optionally support an **Auto-fit** mode that makes the entire canonical grid visible without cropping by adjusting rendering scale (font size / zoom factor) to fit:

* Auto-fit **does not** change canonical PTY/grid size (unless the client is controller and explicitly resizes the session).
* Auto-fit computes a scale factor so that `Cw×Ch` fits within the available device viewport in pixels.
* When Auto-fit is enabled:

  * the default behavior is “show the entire screen” (no cropping) at whatever scale is required
  * users may override with manual zoom for readability (see below)

### 5.5 Manual zoom + pan (primarily Android, optionally web)

Clients may implement manual zoom and pan as a UX layer over the canonical grid:

* Zoom changes local scale only; canonical grid is unchanged.
* When zoomed in such that the canonical grid no longer fits:

  * panning/scrolling moves the viewport over canonical cells (or pixels mapped to cells)
  * cursor-follow is recommended so the active cursor remains visible during output

This yields a clean UX on phones:

* Auto-fit for “overview”
* Zoom-in for readability
* Pan to inspect areas; output remains correct because it’s always a view of the canonical grid

### 5.6 Attach/resync rules

For any attaching client (web/Android/another terminal UI):

1. **SNAPSHOT**: authoritative endpoint sends full terminal state (including alt-screen/modes/cursor) and canonical grid dimensions.
2. **STREAM**: then sends incremental updates with sequence numbers (diffs against that canonical grid).

On reconnect:

* client provides `last_seq_seen`
* authority replays missing updates if available; otherwise issues a fresh snapshot

### 5.7 Multi-attach behavior

* Many clients may attach simultaneously.
* All receive consistent output/state updates.
* Exactly one client holds the controller lease (input + resize).

---

## 6) Controller lease and resize policy

### 6.1 Single controller lease (mandatory)

* Exactly one client may send:

  * keystrokes/input bytes
  * resize events (cols/rows) **for the canonical PTY/grid**
  * optional mouse events (if supported)
* Other clients are viewers (read-only).
* Clients can request control; authority grants deterministically and broadcasts the control holder.

### 6.2 Resize policy (v1) — canonical winsize only follows controller

* PTY has exactly one winsize at a time.
* The **controller’s** window size determines the canonical grid (`Cw×Ch`) and PTY winsize via `TIOCSWINSZ`.
* Viewer resize events **must not** change PTY winsize; they only change that viewer’s viewport mapping (cropping/padding/scale).

### 6.3 Viewport rendering rules (v1)

Each client has:

* `Vw×Vh` = client window cols×rows (local, if relevant)
* `Cw×Ch` = canonical cols×rows (session)

Rendering:

* If Auto-fit is **disabled**:

  * If `Vw < Cw` or `Vh < Ch`: client shows a **viewport** of canonical grid:

    * default: bottom-aligned / follow-cursor vertically
    * horizontally: default `x0=0` with **auto-pan** to keep cursor visible
    * cropping is permitted; garbling is not
  * If `Vw > Cw` or `Vh > Ch`: client shows canonical grid with **padding/margins**:

    * center or top-left alignment acceptable; padding is blank cells

* If Auto-fit is **enabled** (web/Android):

  * client scales rendering so the full `Cw×Ch` fits in the available pixel viewport
  * no cropping by default; manual zoom may re-enable cropping/pan behavior

### 6.4 Local interactive UI as a “client”

The local terminal UI inside interactive `lingon` is also treated as a client:

* it can be the default controller when `lingon` starts
* it can lose control if another client requests and is granted control
* it continues to display updates even when not controlling

**Clarification:** the local UI is a **direct** client of the local authoritative emulator, not a relay-attached client.

---

## 7) Authentication and token model (no API keys/PATs)

### 7.1 Login factors

* **Username + password + TOTP** for interactive login (CLI/web/Android).
* **Refresh token** is the durable credential.

### 7.2 Token strategy

* **Access token**: short-lived (minutes), used for REST/WSS authorization.
* **Refresh token**: long-lived (years), used to mint new access tokens; revocable and rotatable.

### 7.3 `lingon login` (CLI)

* `lingon login -e <endpoint>` prompts:

  * username
  * password (no echo; robust signal handling)
  * TOTP
* Uses a robust prompt helper (e.g., a passphrase prompt that handles Ctrl+C/signals cleanly).
* On success:

  * writes `~/.lingon/auth.json` (0600 perms) with refresh token + cached access token + expiry data
* All subsequent CLI interactions use access token; auto-refresh with refresh token.

### 7.4 Web UI auth

* Web uses Secure HTTPOnly cookies to store session/refresh token material.
* Auto-refresh occurs without user involvement.

### 7.5 Android auth

* Android stores refresh token in Keystore-backed secure storage.
* Auto-refresh access tokens.

### 7.6 Non-interactive usage

* `auth.json` can be supplied to commands/processes to avoid interactive login/TOTP (batch use).
* Refresh token must be treated as a highly sensitive long-lived secret.

---

## 8) Transport security

### TLS requirements

* Relay refuses HTTP; only HTTPS + WSS.
* Clients refuse HTTP by default.
* Support `--insecure/-k` to allow self-signed certs for testing.
* Recommendation: optional server identity pinning (SPKI hash) after first trust for CLI/Android, except when `--insecure` is used.
* TLS certs:

  * If no TLS bundle is provided, `lingon serve` should auto-use certs found under `~/.lingon/tls/`.
  * If no certs exist, it should auto-generate a local CA + server cert (non-interactive).
  * Support ACME via TLS-ALPN-01 with a configured hostname and cache dir.
  * CLI clients should trust `~/.lingon/tls/ca.pem` automatically (per-process trust).

---

## 9) Command-line interface requirements (Cobra + Viper)

### 9.1 Interactive mode

* `lingon`

  * flags:

    * `--endpoint/-e` relay endpoint (optional if configured)
    * `--insecure/-k`
    * `--shell` override login shell (optional)
    * local UI options (optional)
  * behavior:

    * starts interactive shell locally
    * publishes session to relay (if configured) as a background side-channel
    * local rendering/input never depends on the relay
    * remains usable even if relay disconnects (buffer and retry)
    * always restores terminal state on exit (raw mode, echo, signals)

### 9.2 Relay/server mode

* `lingon serve`

  * flags:

    * `--listen` (default localhost unless configured)
    * `--base` base path prefix for all HTTP routes (default `/v1`)
    * `--tls-cert`, `--tls-key`
    * `--data-dir` (DB/state)
    * relay policy options (limits, etc.)

### 9.3 Authentication

* `lingon login -e <endpoint>`

  * flags:

    * `--insecure/-k`
    * `--auth-file <path>` (default `~/.lingon/auth.json`)

### 9.4 User management (admin)

* `lingon users new <username>`

  * create user record
  * generate password (default) or prompt
  * generate TOTP secret and show QR + raw secret
* `lingon users delete <username>`
* `lingon users chpasswd <username>`
* `lingon users rotate-totp <username>`
* Token revocation commands are recommended:

  * `lingon users revoke-tokens <username>` (invalidate refresh tokens)

### 9.5 Remote CLI attach

* `lingon attach -e <endpoint>`

  * flags:

    * `--insecure/-k`
    * `--auth-file <path>` (default `~/.lingon/auth.json`)
    * `--session <id>` (optional; future-proof for multi-session selection)
    * `--request-control` (optional; request controller lease on connect)
  * behavior:

    * connects to relay and attaches to a session via WSS
    * renders in the local terminal
    * obeys controller lease (read-only unless granted control)
    * viewer resize does not resize session unless control is granted
    * renders canonical grid via viewport rules (crop/pad), never reflow

### 9.6 TLS management

* `lingon tls new`

  * generates new CA + server cert/key under `~/.lingon/tls/`
* `lingon tls new ca`

  * generates a new CA cert/key under `~/.lingon/tls/`
* `lingon tls new server`

  * generates a new server cert/key signed by the existing CA
* `lingon tls export`

  * exports CA cert to stdout (or to `-o/--output`)

### 9.7 Bootstrap

* `lingon bootstrap`

  * generates TLS assets (equivalent to `lingon tls new`)
  * writes default config to `~/.lingon/config.yaml`

### Config

* Viper configuration file + env var support.
* Secure defaults; avoid binding to public interfaces by default.

---

## 10) Relay and session brokering model

### 10.1 Session publishing

Interactive `lingon` connects to relay and registers:

* identity (user)
* session id
* capabilities (supports snapshots/diffs, supports auto-fit metadata, etc.)
* initial canonical size (cols/rows)
* whether it is controller by default

Relay maintains a mapping: `{user → active session → attached clients}`.
Future: support multiple sessions per user; session listing/selection will choose which session to attach.

### 10.2 Client attachment

Web/Android/CLI connect to relay, authenticate, then request attachment to the user’s active session (or selected session when multi-session is supported).
Relay routes the attachment to the interactive `lingon` authority.

### 10.3 Where authority logic lives

Authority logic (VT state, snapshots, diffs, replay, canonical grid) lives in interactive `lingon`.
Relay is a broker; it should not need to understand VT semantics in v1.

---

## 11) APIs (recommended; agent may refine)

### 11.1 Relay REST endpoints (examples)

* `POST /auth/login`
* `POST /auth/refresh`
* `POST /auth/logout`
* `GET /me` (whoami/session info)
* `GET /sessions` (list sessions; v1 may return single active session)
* `POST /sessions/attach` (optional helper for UI)

### 11.2 Relay WebSocket endpoints

* `WSS /ws/client` (web/Android client channel)
* `WSS /ws/host` (interactive `lingon` host channel)

Relay primarily forwards frames between host and clients.

### 11.3 Terminal streaming protocol (conceptual)

Messages should include `session_id` + `seq`.

**Mandatory for correctness across differing window sizes:**

* server-authoritative `SNAPSHOT` + `DIFF` against a canonical grid.

Suggested messages:

* `HELLO {client_id, client_cols, client_rows, wants_control, last_seq, client_type, client_px_w?, client_px_h?}`
* `WELCOME {granted_control, canonical_cols, canonical_rows, seq}`
* `SNAPSHOT {seq, canonical_cols, canonical_rows, payload}`  (grid + cursor + modes + scrollback bounds)
* `DIFF {seq, canonical_cols, canonical_rows, updates}`      (cell/line/rect updates, cursor/mode changes, scrollback append)
* `IN {bytes}` (controller only)
* `RESIZE {canonical_cols, canonical_rows}` (controller only)
* `CONTROL {holder_client_id}`

**Optional (not required for correctness):**

* `OUT {seq, bytes}` raw VT stream for debugging/diagnostics only.

Protocol notes:

* `canonical_cols/rows` must be present whenever they change.
* Clients may optionally report pixel viewport size for Auto-fit ergonomics, but it must not affect PTY unless they are controller.

---

## 12) Web UI requirements (xterm.js)

### Pages

* `/login`
* `/terminal`

### Behavior

* Authenticate via username/password/TOTP.
* Auto-refresh tokens with cookies.
* Attach to active session via relay WSS.
* Show controller status and request control.
* Apply snapshot then stream updates.
* Rendering modes:

  1. **Auto-fit (default recommended)**: adjust xterm.js font size (or CSS scale) so full canonical `Cw×Ch` fits in viewport.
  2. **Manual zoom**: user increases/decreases font size/scale for readability.
  3. **Pan/crop**: if manual zoom makes the grid larger than viewport, allow scrolling/panning while keeping cursor visible.

---

## 13) Android app requirements (native)

### Terminal UX

* Full-screen terminal
* Special keyboard row (Ctrl/Esc/Tab/arrows/etc.)
* Endpoint configuration and insecure/self-signed toggle (testing)

### Auth and tokens

* Login: username/password/TOTP
* Store refresh token in Keystore-backed storage
* Auto-refresh access token

### App lock gating

* On resume: prompt for device credential if available
* Grace window default: 5 minutes

### Rendering model

* Prefer server-authoritative snapshots + diffs (Android is renderer, not emulator) in v1.
* Rendering modes:

  1. **Auto-fit (default recommended)**: scale to fit entire canonical grid within device viewport.
  2. **Pinch-to-zoom**: manual zoom for readability.
  3. **Pan**: drag to pan when zoomed in; cursor-follow recommended during live output.
* Zoom/pan must never change canonical PTY size unless Android holds controller lease and explicitly resizes session.

---

## 14) Data model (relay/server)

Minimum entities:

* `User { id, username, password_hash, totp_secret_encrypted, created_at, disabled_at? }`
* `RefreshToken { user_id, token_hash, created_at, expires_at, revoked_at?, last_used_at, metadata }`
* `Session { id, user_id, name?, created_at, last_active_at, status }`
* `ActiveSession { session_id, host_connection_id, last_seen_at, controller_client_id, canonical_cols, canonical_rows }`

Storage recommendation: SQLite (v1). Store refresh tokens hashed only.

Notes:

* v1: at most one active session per user.
* future: multiple sessions per user; listing/selecting sessions will be supported by CLI/web/Android.

---

## 15) Operational requirements

* Structured logging; never log secrets.
* Rate limiting on login and refresh endpoints.
* Metrics: active sessions, attached clients, auth failures, relay forwarding rates, host connectivity.
* Graceful handling of relay disconnects:

  * interactive `lingon` should continue locally and buffer updates until relay reconnects (bounded).
  * clients should display “disconnected” and attempt reconnect with replay/snapshot.

---

## 16) Acceptance criteria (v1 “done”)

1. Running `lingon` opens an interactive local shell that behaves like a normal terminal session.
2. `lingon serve` provides HTTPS/WSS and serves web UI.
3. Admin can create user with `lingon users new alice` (password + TOTP QR output).
4. `lingon attach` attaches to the active session and renders in a local terminal.
5. Web login attaches to the active session published by local `lingon`.
6. Android login attaches to the same session; can request control and type.
7. Switching control between local terminal, CLI attach, web, and Android works; all stay in sync.
8. Attach/reconnect always yields correct screen state (snapshot + streaming; replay when possible).
9. Tokens refresh automatically; users are not interrupted during normal use.
10. Relay refuses HTTP; `--insecure/-k` enables self-signed TLS for testing.
11. When viewer window sizes differ from the controller:

    * no garbled wrapping occurs
    * viewers either crop/pad (viewport) or use Auto-fit scaling (web/Android)
    * only the controller can resize the canonical PTY/grid
12. Web and Android provide:

    * Auto-fit mode that fits full canonical grid in viewport
    * Manual zoom (font size / scale) and pan when zoomed

---

## 17) Decisions intentionally left to the agent

* Exact headless VT emulation library and feature scope.
* Snapshot and diff encoding format.
* Token format (JWT vs opaque); refresh rotation details (rotation recommended).
* Wire framing (JSON vs binary); compression choice.
* Local UI rendering strategy for `lingon` (e.g., termbox/tcell-based renderer vs ANSI passthrough with careful state).
* Exact viewport UX (cursor-follow rules, pan keys/gestures, indicators, auto-fit algorithm).
* Session naming/multiplicity policy (v1 recommended: one active session per user).
* Session sharing policy (who can attach to someone else's session and how access is granted).

---

## 18) Recommendations (opinionated constraints)

* **Authority should live with interactive `lingon`** (the PTY owner), not the relay.
* **Controller lease is mandatory** for sane typing + resizing semantics.
* **Canonical grid + server-side VT state + per-client viewport/scale is mandatory** to avoid garbling when clients differ in size.
* **Auto-fit should be the default for web/Android** to ensure usable overview on small screens.
* **Refresh tokens are long-lived credentials**: rotate on refresh, store hashed server-side, support revocation.
* **Android should be a renderer** driven by diffs, not a full emulator in v1.
* Keep v1 **one active session per user** to optimize “pick up and continue.”
