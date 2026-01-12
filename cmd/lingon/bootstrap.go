package main

import (
	"github.com/spf13/cobra"

	"pkt.systems/lingon"
	"pkt.systems/pslog"
)

// NewBootstrapCommand builds the bootstrap command.
func NewBootstrapCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Initialize Lingon config and TLS assets",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := pslog.Ctx(cmd.Context()).With("component", "bootstrap")
			cfg := lingon.DefaultConfig()
			_, err := lingon.Bootstrap(cmd.Context(), cfg, logger)
			return err
		},
	}

	return cmd
}
