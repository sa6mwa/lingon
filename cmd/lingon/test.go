package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"pkt.systems/lingon/internal/grapheme"
)

// NewTestCommand builds the test helper command.
func NewTestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Test helpers and diagnostics",
	}
	cmd.AddCommand(newTestGraphemeCommand())
	return cmd
}

func newTestGraphemeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "grapheme",
		Short: "Print Unicode/grapheme test patterns",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Lingon Grapheme Test")
			fmt.Fprintln(out, "Compare output across terminals and clients. UTF-8 required.")
			fmt.Fprintln(out, "------------------------------------------------------------")
			samples := grapheme.Samples()
			for i, sample := range samples {
				fmt.Fprintln(out)
				fmt.Fprintf(out, "[%02d] %s\n", i+1, sample.Name)
				if sample.Description != "" {
					fmt.Fprintf(out, "  %s\n", sample.Description)
				}
				for _, line := range sample.Lines {
					fmt.Fprintf(out, "  %s\x1b[0m\n", line)
				}
			}
			fmt.Fprintln(out)
			return nil
		},
	}
}
