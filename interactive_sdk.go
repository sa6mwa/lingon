package lingon

import (
	"context"
	"time"

	"pkt.systems/lingon/internal/session"
	"pkt.systems/lingon/internal/trace"
	"pkt.systems/pslog"
)

// InteractiveOptions configures a local interactive Lingon session.
type InteractiveOptions struct {
	Endpoint         string
	Token            string
	AuthFile         string
	SessionID        string
	Cols             int
	Rows             int
	Shell            string
	Term             string
	Respawn          bool
	Offline          bool
	Theme            string
	Publish          bool
	PublishControl   bool
	HostnameOnly     bool
	ScrollbackLines  int
	MaxReplayScreens int
	TLSDir           string
	Insecure         bool
	Logger           pslog.Logger
	Trace            *trace.Writer
	OnExit           func(sessionID string, startedAt time.Time, err error)
}

// Interactive starts a local interactive session and optionally publishes to the relay.
func Interactive(ctx context.Context, opts InteractiveOptions) error {
	startedAt := time.Now()
	runner := session.New(session.Options{
		Endpoint:         opts.Endpoint,
		Token:            opts.Token,
		AuthFile:         opts.AuthFile,
		SessionID:        opts.SessionID,
		Cols:             opts.Cols,
		Rows:             opts.Rows,
		Shell:            opts.Shell,
		Term:             opts.Term,
		Respawn:          opts.Respawn,
		Offline:          opts.Offline,
		Theme:            opts.Theme,
		Publish:          opts.Publish,
		PublishControl:   opts.PublishControl,
		HostnameOnly:     opts.HostnameOnly,
		ScrollbackLines:  opts.ScrollbackLines,
		MaxReplayScreens: opts.MaxReplayScreens,
		TLSDir:           opts.TLSDir,
		Insecure:         opts.Insecure,
		Logger:           opts.Logger,
		Trace:            opts.Trace,
	})
	err := runner.Run(ctx)
	if opts.OnExit != nil {
		opts.OnExit(runner.SessionID(), startedAt, err)
	}
	return err
}
