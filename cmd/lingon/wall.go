package main

import (
	"strings"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/cliwall"
)

// NewWallCommand builds the wall broadcast command.
func NewWallCommand(loader *lingon.Loader) *cobra.Command {
	var endpoint string
	var accessToken string
	var authFile string
	var quiet bool

	v := loader.Viper()
	v.SetDefault("client.endpoint", lingon.DefaultClientEndpoint)
	v.SetDefault("client.auth_file", lingon.DefaultAuthPath())

	cmd := &cobra.Command{
		Use:   "wall [flags] <message...>",
		Short: "Broadcast a message to your active sessions",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			insecure, err := cmd.Flags().GetBool("insecure")
			if err != nil {
				return err
			}
			return cliwall.Execute(cmd.Context(), cliwall.Request{
				Loader:             loader,
				Endpoint:           endpoint,
				EndpointChanged:    cmd.Flags().Changed("endpoint"),
				AccessToken:        accessToken,
				AccessTokenChanged: cmd.Flags().Changed("access-token"),
				AuthFile:           authFile,
				AuthFileChanged:    cmd.Flags().Changed("auth-file"),
				Message:            strings.TrimSpace(strings.Join(args, " ")),
				Insecure:           insecure,
				Quiet:              quiet,
				Stdout:             cmd.OutOrStdout(),
			})
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&endpoint, "endpoint", "e", lingon.DefaultClientEndpoint, "relay endpoint (https/wss base URL; assumes https:// if omitted)")
	flags.StringVar(&accessToken, "access-token", "", "access token for authenticated request")
	flags.StringVar(&authFile, "auth-file", lingon.DefaultAuthPath(), "path to auth file")
	flags.BoolVarP(&quiet, "quiet", "q", false, "suppress success output")
	registerEndpointFlagCompletion(cmd, loader)

	return cmd
}
