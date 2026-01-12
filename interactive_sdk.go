package lingon

import (
	"context"

	"pkt.systems/lingon/internal/session"
	"pkt.systems/pslog"
)

// InteractiveOptions configures a local interactive Lingon session.
type InteractiveOptions struct {
	Endpoint       string
	Token          string
	SessionID      string
	Cols           int
	Rows           int
	Shell          string
	Term           string
	Publish        bool
	PublishControl bool
	BufferLines    int
	Logger         pslog.Logger
}

// Interactive starts a local interactive session and optionally publishes to the relay.
func Interactive(ctx context.Context, opts InteractiveOptions) error {
	return session.New(session.Options{
		Endpoint:       opts.Endpoint,
		Token:          opts.Token,
		SessionID:      opts.SessionID,
		Cols:           opts.Cols,
		Rows:           opts.Rows,
		Shell:          opts.Shell,
		Term:           opts.Term,
		Publish:        opts.Publish,
		PublishControl: opts.PublishControl,
		BufferLines:    opts.BufferLines,
		Logger:         opts.Logger,
	}).Run(ctx)
}
