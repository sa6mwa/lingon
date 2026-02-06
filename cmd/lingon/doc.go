// Command lingon is the Lingon CLI.
//
// Lingon is a secure, multi-client terminal relay. The CLI can:
//   - start interactive local sessions (the default command),
//   - attach to existing sessions (including view-only share tokens),
//   - manage relay users, sessions, and share tokens,
//   - bootstrap configs and TLS assets, and
//   - run the relay server.
//
// Quick start (local relay):
//
//  1. Initialize config and TLS:
//     lingon bootstrap -c /path/to/lingon/config.yaml
//
//  2. Create a relay user:
//     lingon users add alice --users-file /path/to/lingon/users.json
//
//  3. Run the relay:
//     lingon -c /path/to/lingon/config.yaml serve
//
//  4. Log in and start an interactive session:
//     lingon -c /path/to/lingon/config.yaml login
//     lingon -c /path/to/lingon/config.yaml
//
// Common commands:
//
//	Start an interactive session (default):
//	     lingon [-c CONFIG] [--endpoint URL] [--session ID]
//
//	Attach to a session (authenticated):
//	     lingon attach [session-id]
//	     lingon attach [session-id] --access-token TOKEN
//
//	Attach with a share token (view or control):
//	     lingon attach --token SHARE_TOKEN
//
//	Attach and request control explicitly:
//	     lingon attach [session-id] --request-control
//
//	Send input to a session:
//	     lingon send <session-id> -- ls -la
//
//	Send a wall message to your active sessions:
//	     lingon wall hello world
//
//	Start host session offline first, then toggle publish/connect with Ctrl+L o:
//	     lingon --offline
//
//	List sessions (authenticated):
//	     lingon sessions
//
//	Share a session:
//	     lingon share create [session-id] --scope view --ttl 1h
//
//	List share tokens (defaults to valid):
//	     lingon share list
//	     lingon share list --revoked --expired
//	     lingon share list --all
//
//	Revoke a share token:
//	     lingon share revoke <token>
//	     lingon share revoke all
//	     lingon share revoke --all
//
//	List available themes:
//	     lingon themes
//
//	Print version:
//	     lingon version
//
//	Generate shell completion:
//	     lingon completion bash
//
// Configuration:
//   - Use -c/--config to point at a config file.
//   - bootstrap writes a config file (default: ~/.lingon/config.yaml) and
//     TLS assets relative to the config file location. Use -s/--set to override
//     config values, -f/--force to overwrite the config, and
//     --regenerate-certificates to regenerate TLS assets.
//   - Example (networked TLS): lingon bootstrap -s server.tls.hostname=lingon.my.domain.tld -s server.listen=:8443
//     generates a CA and server cert for lingon.my.domain.tld (CN/SAN) and
//     listens on all interfaces at port 8443 (use a non-127.0.0.1 listen
//     address to serve over the network).
//   - The relay (serve) binds its flags into config via Viper, so you can
//     override config values with CLI flags.
//
// TLS:
//   - The client loads system CAs and the TLS dir configured in the config
//     file (or defaults if not set).
//   - Use -k/--insecure to skip TLS verification.
//   - Use lingon tls new to generate a CA + server cert bundle, or
//     lingon tls export to export the CA certificate.
//
// Rendering architecture:
//   - Host/attach terminal UI rendering is centralized in the internal
//     full-framebuffer MVU runtime (internal/mvu).
//   - Runtime transitions are action-driven (ApplyAction) and frame output
//     is composed snapshot + delta.
//   - The legacy internal/compositor package has been removed.
//
// Parity caveat:
//   - Web UI and Android clients share protocol semantics but keep independent
//     renderers, so visual timing/details can differ from terminal CLI in
//     edge cases.
//
// Subcommands (overview):
//
//	attach   Attach to a session (auth or share token).
//	send     Send input tokens to a session.
//	wall     Broadcast a message to your active sessions.
//	share    Create/list/revoke share tokens.
//	sessions List sessions visible to the authenticated user.
//	login    Store auth tokens locally via username/password/TOTP.
//	logout   Revoke endpoint auth and remove stored local tokens.
//	users    Manage relay users (local, from users file).
//	serve    Run the relay server.
//	tls      Manage TLS assets (CA/server/export).
//	bootstrap Initialize config and TLS assets.
//	themes   List available terminal themes.
//	test     Diagnostics (e.g. grapheme rendering test patterns).
//	completion Generate shell completion scripts.
//	version  Print CLI version/build metadata.
//
// Notes:
//   - Many commands require authentication; use lingon login to store
//     tokens and --access-token to override. Use lingon logout to revoke
//     and remove tokens for the selected endpoint.
//   - lingon attach --pick lets you choose a session interactively.
//   - lingon send expects input tokens after --.
//   - Ctrl+L is the command introducer in host/attach TUI (Ctrl+L h help,
//     Ctrl+L b toggle tab bar, Ctrl+L o toggle offline publish/connect).
//   - WebSocket endpoints (.../ws/...) must be proxied to the relay over
//     HTTP/1.1. If a reverse proxy negotiates HTTP/2 to the upstream, force
//     HTTP/1.1 for those routes (or disable h2 to the relay).
package main
