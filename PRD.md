## PRD: **Lingon** — Interactive terminal that is shareable across devices (Go binary + relay + web UI + native Android)

### 0) One-sentence summary

**Lingon** is a Go-based interactive terminal application that you run like a normal terminal (`lingon`) while it transparently publishes the same authoritative VT session to other UIs (web xterm.js and native Android) via a secure HTTPS/WSS relay (`lingon serve`), enabling seamless device switching with single-controller leasing and automatic token refresh.

---

## 1) Goals

### Primary goals

1. **Local-first interactive terminal**: Running `lingon` in a terminal is a full interactive shell experience, comparable to xterm/vterm/tmux, not a background daemon.
2. **Seamless device switching**: Start typing locally, then continue on phone, then back—same session state.
3. **Multi-attach synchronization**: Multiple clients may attach concurrently; all see consistent terminal state.
4. **Authoritative VT state**: One component is authoritative for screen state and can snapshot/resync other UIs.
5. **Secure-by-default**: HTTPS/WSS only; login via username + password + TOTP; long-lived refresh tokens; short-lived access tokens.
6. **Small system**: Single Go binary provides both interactive mode and relay mode; web UI served by relay; native Android app.

### Non-goals (v1)

* Simultaneous multi-writer collaboration (two people typing concurrently). Only one controller at a time.
* tmux feature parity (panes, splitting, session groups).
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
4. If a device disconnects, it can reconnect and recover the exact screen state.
5. As admin, I can create/delete users, set passwords, rotate TOTP, revoke tokens.

---

## 3) Deliverables

1. **Go binary** `lingon` providing:

   * `lingon` (interactive mode; local terminal UI + hosted session publisher)
   * `lingon serve` (relay/server mode; HTTPS/WSS + embedded web UI + user management)
   * `lingon login` (interactive login to relay; stores tokens locally)
   * `lingon users …` (admin-only; server-side user management)
2. **Web UI** (xterm.js) served by `lingon serve`
3. **Native Android app**

   * full-screen terminal UI + special keyboard
   * endpoint config + login
   * device-lock gating on resume

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

### 4.2 Relay/server mode (`lingon serve`)

The relay is responsible for:

* HTTPS/WSS termination
* authentication and token issuance/refresh
* brokering multi-client attachments to a published session
* enforcing or coordinating controller lease and resize policy
* serving the embedded web UI

### 4.3 Remote UIs

* Web UI and Android app are additional UIs that attach to a session published by an interactive `lingon` instance.

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

### 5.3 Attach/resync rules

For any attaching client (web/Android/another terminal UI):

1. **SNAPSHOT**: authoritative endpoint sends full terminal state (including alt-screen/modes/cursor)
2. **STREAM**: then sends incremental updates with sequence numbers

On reconnect:

* client provides `last_seq_seen`
* authority replays missing updates if available; otherwise issues a fresh snapshot

### 5.4 Multi-attach behavior

* Many clients may attach simultaneously.
* All receive consistent output/state updates.
* Exactly one client holds the controller lease (input + resize).

---

## 6) Controller lease and resize policy

### 6.1 Single controller lease (mandatory)

* Exactly one client may send:

  * keystrokes/input bytes
  * resize events (cols/rows)
  * optional mouse events (if supported)
* Other clients are viewers (read-only).
* Clients can request control; authority grants deterministically and broadcasts the control holder.

### 6.2 Resize policy (v1)

* PTY has one size; authority uses the controller’s size.
* Viewers accept the resulting layout.

### 6.3 Local interactive UI as a “client”

The local terminal UI inside interactive `lingon` is also treated as a client:

* it can be the default controller when `lingon` starts
* it can lose control if another client requests and is granted control
* it continues to display updates even when not controlling

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
    * publishes session to relay (if configured)
    * remains usable even if relay disconnects (buffer and retry)

### 9.2 Relay/server mode

* `lingon serve`

  * flags:

    * `--listen` (default localhost unless configured)
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

### Config

* Viper configuration file + env var support.
* Secure defaults; avoid binding to public interfaces by default.

---

## 10) Relay and session brokering model

### 10.1 Session publishing

Interactive `lingon` connects to relay and registers:

* identity (user)
* session id
* capabilities (supports snapshots/diffs, etc.)
* initial size (cols/rows)
* whether it is controller by default

Relay maintains a mapping: `{user → active session → attached clients}`.

### 10.2 Client attachment

Web/Android connect to relay, authenticate, then request attachment to the user’s active session.
Relay routes the attachment to the interactive `lingon` authority.

### 10.3 Where authority logic lives

Authority logic (VT state, snapshots, diffs, replay) lives in interactive `lingon`.
Relay is a broker; it should not need to understand VT semantics in v1.

---

## 11) APIs (recommended; agent may refine)

### 11.1 Relay REST endpoints (examples)

* `POST /auth/login`
* `POST /auth/refresh`
* `POST /auth/logout`
* `GET /me` (whoami/session info)
* `GET /sessions` (optional: active session presence)
* `POST /sessions/attach` (optional helper for UI)

### 11.2 Relay WebSocket endpoints

* `WSS /ws/client` (web/Android client channel)
* `WSS /ws/host` (interactive `lingon` host channel)

Relay primarily forwards frames between host and clients.

### 11.3 Terminal streaming protocol (conceptual)

Messages should include `session_id` + `seq`:

* `HELLO {client_id, cols, rows, wants_control, last_seq, client_type}`
* `WELCOME {granted_control, server_cols, server_rows, seq}`
* `SNAPSHOT {seq, payload}`
* `DIFF {seq, updates}` (preferred for Android; optional for web)
* `OUT {seq, bytes}` (optional raw stream for xterm.js)
* `IN {bytes}` (controller only)
* `RESIZE {cols, rows}` (controller only)
* `CONTROL {holder_client_id}`

Hybrid approach is acceptable:

* Web: snapshot as redraw stream + subsequent `OUT`
* Android: snapshot as grid + `DIFF`

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

* Prefer server-authoritative diffs (Android is renderer, not emulator) in v1.

---

## 14) Data model (relay/server)

Minimum entities:

* `User { id, username, password_hash, totp_secret_encrypted, created_at, disabled_at? }`
* `RefreshToken { user_id, token_hash, created_at, expires_at, revoked_at?, last_used_at, metadata }`
* `ActiveSession { user_id, session_id, host_connection_id, last_seen_at, controller_client_id, cols, rows }`

Storage recommendation: SQLite (v1). Store refresh tokens hashed only.

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
4. Web login attaches to the active session published by local `lingon`.
5. Android login attaches to the same session; can request control and type.
6. Switching control between local terminal, web, and Android works; all stay in sync.
7. Attach/reconnect always yields correct screen state (snapshot + streaming; replay when possible).
8. Tokens refresh automatically; users are not interrupted during normal use.
9. Relay refuses HTTP; `--insecure/-k` enables self-signed TLS for testing.

---

## 17) Decisions intentionally left to the agent

* Exact headless VT emulation library and feature scope.
* Snapshot and diff encoding format.
* Token format (JWT vs opaque); refresh rotation details (rotation recommended).
* Wire framing (JSON vs binary); compression choice.
* Local UI rendering strategy for `lingon` (e.g., termbox/tcell-based renderer vs ANSI passthrough with careful state).
* Session naming/multiplicity policy (v1 recommended: one active session per user).

---

## 18) Recommendations (opinionated constraints)

* **Authority should live with interactive `lingon`** (the PTY owner), not the relay.
* **Controller lease is mandatory** for sane typing + resizing semantics.
* **Refresh tokens are long-lived credentials**: rotate on refresh, store hashed server-side, support revocation.
* **Android should be a renderer** driven by diffs, not a full emulator in v1.
* Keep v1 **one active session per user** to optimize “pick up and continue.”

