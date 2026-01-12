package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
	"pkt.systems/pslog"
)

// NewTLSCommand builds the TLS management command.
func NewTLSCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tls",
		Short: "Manage Lingon TLS assets",
	}

	cmd.AddCommand(newTLSNewCommand())
	cmd.AddCommand(newTLSExportCommand())

	return cmd
}

func newTLSNewCommand() *cobra.Command {
	var dir string
	var hostname string

	cmd := &cobra.Command{
		Use:   "new",
		Short: "Generate TLS assets",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := pslog.Ctx(cmd.Context()).With("component", "tls")
			if dir == "" {
				dir = lingon.DefaultTLSDir()
			}
			return lingon.TLSNew(cmd.Context(), dir, hostname, logger)
		},
	}

	cmd.PersistentFlags().StringVar(&dir, "dir", lingon.DefaultTLSDir(), "tls directory")
	cmd.PersistentFlags().StringVar(&hostname, "hostname", "", "server certificate hostname (overrides localhost SAN)")

	cmd.AddCommand(newTLSNewCACommand(&dir))
	cmd.AddCommand(newTLSNewServerCommand(&dir, &hostname))

	return cmd
}

func newTLSNewCACommand(dir *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ca",
		Short: "Generate a new CA certificate",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := pslog.Ctx(cmd.Context()).With("component", "tls")
			if *dir == "" {
				*dir = lingon.DefaultTLSDir()
			}
			return lingon.TLSNewCA(cmd.Context(), *dir, logger)
		},
	}

	return cmd
}

func newTLSNewServerCommand(dir, hostname *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Generate a new server certificate",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := pslog.Ctx(cmd.Context()).With("component", "tls")
			if *dir == "" {
				*dir = lingon.DefaultTLSDir()
			}
			return lingon.TLSNewServer(cmd.Context(), *dir, *hostname, logger)
		},
	}

	return cmd
}

func newTLSExportCommand() *cobra.Command {
	var dir string
	var output string

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export the CA certificate",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dir == "" {
				dir = lingon.DefaultTLSDir()
			}
			out := os.Stdout
			if output != "" {
				file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
				if err != nil {
					return err
				}
				out = file
				defer func() {
					if cerr := file.Close(); cerr != nil {
						pslog.Ctx(cmd.Context()).Error("failed to close output file", "err", cerr)
					}
				}()
			}
			if err := lingon.TLSExportCA(dir, out); err != nil {
				return err
			}
			if output != "" {
				fmt.Fprintf(os.Stdout, "exported CA to %s\n", output)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", lingon.DefaultTLSDir(), "tls directory")
	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (defaults to stdout)")

	return cmd
}
