package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
)

// NewSessionsCommand builds the sessions management command.
func NewSessionsCommand(loader *lingon.Loader) *cobra.Command {
	var endpoint string
	var accessToken string
	var authFile string

	v := loader.Viper()
	v.SetDefault("client.endpoint", lingon.DefaultClientEndpoint)
	v.SetDefault("client.auth_file", lingon.DefaultAuthPath())

	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List and manage sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			headlessMode, err := cmd.Flags().GetBool("headless")
			if err != nil {
				return err
			}
			if headlessMode {
				if _, err := loader.Load(); err != nil {
					return err
				}
				sessions, err := listLocalHeadlessSessions(configDirForLoader(loader))
				if err != nil {
					return err
				}
				return printJSON(cmd.OutOrStdout(), sessions)
			}

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

			sessions, err := lingon.ListSessionsWithTLSDirInsecure(cmd.Context(), endpointValue, tokenValue, tlsDir, insecure)
			if err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), sessions)
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&endpoint, "endpoint", "e", lingon.DefaultClientEndpoint, "relay endpoint (https/wss base URL; assumes https:// if omitted)")
	flags.StringVar(&accessToken, "access-token", "", "access token for authenticated request")
	flags.StringVar(&authFile, "auth-file", lingon.DefaultAuthPath(), "path to auth file")
	registerEndpointFlagCompletion(cmd, loader)

	return cmd
}
