package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"pkt.systems/version"
)

// NewVersionCommand builds the version command.
func NewVersionCommand() *cobra.Command {
	var semverOnly bool
	var versionOnly bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print Lingon version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if semverOnly && versionOnly {
				return fmt.Errorf("cannot use --semver and --version together")
			}
			if semverOnly {
				fmt.Fprintln(cmd.OutOrStdout(), version.CurrentSemver())
				return nil
			}
			if versionOnly {
				fmt.Fprintln(cmd.OutOrStdout(), version.Current())
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "pkt.systems/lingon %s\n", version.Current())
			return nil
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&semverOnly, "semver", false, "print semver only")
	flags.BoolVar(&versionOnly, "version", false, "print version only")

	return cmd
}
