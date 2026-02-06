// Package lingon provides the Lingon server and client SDK.
//
// # Overview
//
// Lingon is a secure terminal relay. The SDK mirrors the CLI and is centered on
// four roles:
//
//   - Serve: run the relay server (HTTPS + WSS + Web UI).
//   - Host: publish a terminal session to the relay.
//   - Attach: connect to a session (or share token) and render it locally.
//   - Clients: manage auth, users, sessions, and share tokens.
//
// # Rendering architecture
//
// CLI host/attach TUI rendering runs through the internal full-framebuffer
// MVU runtime (internal/mvu), which centralizes:
//
//   - typed state transitions via actions,
//   - tab/help/wall/connectivity overlay policies, and
//   - composed-snapshot frame output + delta emission.
//
// This is an internal implementation detail (not a public API contract), but
// it is the canonical renderer for terminal CLI behavior.
//
// # Parity caveat
//
// Web UI and Android clients share relay/session protocol semantics but use
// independent renderers. Functional behavior should match, while exact visual
// timing/details may differ from terminal CLI rendering in edge cases.
//
// # Configuration
//
// Use the Config/Loader helpers to load configuration files and defaults:
//
//	loader := lingon.NewLoader()
//	cfg, err := loader.Load()
//	if err != nil { /* handle */ }
//
// Example: Serve
//
//	ctx := context.Background()
//	cfg := lingon.DefaultConfig()
//	cfg.Server.Listen = "0.0.0.0:12843"
//	cfg.Server.BasePath = "/v1"
//	_ = lingon.Serve(ctx, lingon.ServeOptions{Config: cfg})
//
// Example: Login + ensure access token
//
//	ctx := context.Background()
//	state, err := lingon.Login(ctx, lingon.LoginOptions{
//	    Endpoint: "https://relay.example.com/v1",
//	    Username: "alice",
//	    Password: "...",
//	    TOTP:     "123456",
//	})
//	if err != nil { /* handle */ }
//	_ = lingon.SaveAuth("/path/to/auth.json", state)
//	// auth.json stores entries by normalized endpoint key.
//
//	state, err = lingon.EnsureAccessToken(ctx, "https://relay.example.com/v1", "/path/to/auth.json")
//	if err != nil { /* handle */ }
//
// Example: Host
//
//	ctx := context.Background()
//	_ = lingon.Host(ctx, lingon.HostOptions{
//	    Endpoint:  "https://relay.example.com/v1",
//	    Token:     state.AccessToken,
//	    SessionID: "prod-shell",
//	    Cols:      120,
//	    Rows:      40,
//	})
//
// Example: Attach
//
//	ctx := context.Background()
//	_ = lingon.Attach(ctx, lingon.AttachOptions{
//	    Endpoint:    "https://relay.example.com/v1",
//	    SessionID:   "prod-shell",
//	    AccessToken: state.AccessToken,
//	    RequestControl: true,
//	})
//
// Example: Share token
//
//	ctx := context.Background()
//	share, err := lingon.ShareCreate(ctx, lingon.ShareCreateOptions{
//	    Endpoint:    "https://relay.example.com/v1",
//	    AccessToken: state.AccessToken,
//	    SessionID:   "prod-shell",
//	    Scope:       lingon.ShareScopeView,
//	})
//	if err != nil { /* handle */ }
//	_ = share.Token
//
// # TLS
//
// Most client methods accept TLSDir and Insecure fields. TLSDir points at a
// directory that contains a trusted CA bundle. Insecure disables verification
// and should only be used for local testing.
package lingon
