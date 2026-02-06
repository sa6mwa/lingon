package lingon

import (
	"context"

	"pkt.systems/lingon/internal/host"
	"pkt.systems/pslog"
)

// HostOptions configures an interactive host session.
type HostOptions struct {
	Endpoint        string
	Token           string
	SessionID       string
	Cols            int
	Rows            int
	ScrollbackLines int
	TLSDir          string
	Insecure        bool
	Logger          pslog.Logger
}

// Host starts an authoritative terminal host session.
func Host(ctx context.Context, opts HostOptions) error {
	return (&host.Host{
		Endpoint:        opts.Endpoint,
		Token:           opts.Token,
		SessionID:       opts.SessionID,
		Cols:            opts.Cols,
		Rows:            opts.Rows,
		ScrollbackLines: opts.ScrollbackLines,
		TLSDir:          opts.TLSDir,
		Insecure:        opts.Insecure,
		Logger:          opts.Logger,
	}).Run(ctx)
}
