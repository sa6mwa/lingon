package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"pkt.systems/lingon/internal/buildmeta"
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
			current := buildmeta.ReleaseVersion()
			if current == "" {
				current = version.Current()
			}
			if semverOnly {
				fmt.Fprintln(cmd.OutOrStdout(), strings.TrimPrefix(current, "v"))
				return nil
			}
			if versionOnly {
				fmt.Fprintln(cmd.OutOrStdout(), current)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "pkt.systems/lingon %s\n", current)
			return nil
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&semverOnly, "semver", false, "print semver only")
	flags.BoolVar(&versionOnly, "version", false, "print version only")

	return cmd
}
