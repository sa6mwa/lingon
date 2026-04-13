package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
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
			cfg, err := loader.Load()
			if err != nil {
				return err
			}
			tlsDir := cfg.Server.TLS.Dir
			insecure, err := cmd.Flags().GetBool("insecure")
			if err != nil {
				return err
			}

			authPath := authFile
			if !cmd.Flags().Changed("auth-file") {
				authPath = cfg.Client.AuthFile
			}
			endpointValue, err := resolveEndpointValue(cmd, loader, cfg.Client.Endpoint, endpoint, authPath)
			if err != nil {
				return err
			}
			if endpointValue == "" {
				return fmt.Errorf("endpoint is required")
			}

			tokenValue := accessToken
			if !cmd.Flags().Changed("access-token") {
				resolved, err := resolveAccessToken(cmd.Context(), endpointValue, authPath, tlsDir, insecure)
				if err != nil {
					return err
				}
				tokenValue = resolved
			}
			if tokenValue == "" {
				return fmt.Errorf("access token is required")
			}

			resp, err := lingon.Wall(cmd.Context(), lingon.WallOptions{
				Endpoint:    endpointValue,
				AccessToken: tokenValue,
				Message:     strings.TrimSpace(strings.Join(args, " ")),
				TLSDir:      tlsDir,
				Insecure:    insecure,
			})
			if err != nil {
				return err
			}
			if quiet {
				return nil
			}
			return printJSON(cmd.OutOrStdout(), resp)
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
