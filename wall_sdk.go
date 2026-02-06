package lingon

import (
	"context"

	"pkt.systems/lingon/internal/relayclient"
)

// WallOptions contains inputs for sending a wall message.
type WallOptions struct {
	Endpoint    string
	AccessToken string
	Message     string
	TLSDir      string
	Insecure    bool
}

// WallResponse captures relay wall dispatch status.
type WallResponse = relayclient.WallResponse

// WallInactivityOptions contains inputs for toggling inactivity wall notifications.
type WallInactivityOptions struct {
	Endpoint    string
	AccessToken string
	SessionID   string
	Enabled     bool
	TLSDir      string
	Insecure    bool
}

// WallInactivityResponse captures inactivity wall toggle status.
type WallInactivityResponse = relayclient.WallInactivityResponse

// Wall sends a wall message to sessions owned by the authenticated user.
func Wall(ctx context.Context, opts WallOptions) (WallResponse, error) {
	return relayclient.SendWall(ctx, opts.Endpoint, opts.AccessToken, opts.Message, opts.TLSDir, opts.Insecure)
}

// WallInactivity toggles inactivity wall monitoring for one session.
func WallInactivity(ctx context.Context, opts WallInactivityOptions) (WallInactivityResponse, error) {
	return relayclient.SetWallInactivity(
		ctx,
		opts.Endpoint,
		opts.AccessToken,
		opts.SessionID,
		opts.Enabled,
		opts.TLSDir,
		opts.Insecure,
	)
}
