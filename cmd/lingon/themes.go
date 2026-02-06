package main

import (
	"github.com/spf13/cobra"

	"pkt.systems/lingon"
)

// NewThemesCommand builds the themes list command.
func NewThemesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "themes",
		Short: "List available themes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, name := range lingon.ThemeNames() {
				_, _ = cmd.OutOrStdout().Write([]byte(name + "\n"))
			}
			return nil
		},
	}
}
