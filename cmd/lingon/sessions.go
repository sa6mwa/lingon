package main

import (
	"encoding/json"
	"fmt"
	"os"

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
			cfg, err := loader.Load()
			if err != nil {
				return err
			}
			endpointValue := endpoint
			if !cmd.Flags().Changed("endpoint") {
				endpointValue = cfg.Client.Endpoint
			}
			if endpointValue == "" {
				return fmt.Errorf("endpoint is required")
			}

			authPath := authFile
			if !cmd.Flags().Changed("auth-file") {
				authPath = cfg.Client.AuthFile
			}

			tokenValue := accessToken
			if !cmd.Flags().Changed("access-token") {
				resolved, err := resolveAccessToken(cmd.Context(), endpointValue, authPath)
				if err != nil {
					return err
				}
				tokenValue = resolved
			}
			if tokenValue == "" {
				return fmt.Errorf("access token is required")
			}

			sessions, err := lingon.ListSessions(cmd.Context(), endpointValue, tokenValue)
			if err != nil {
				return err
			}
			return json.NewEncoder(os.Stdout).Encode(sessions)
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&endpoint, "endpoint", "e", lingon.DefaultClientEndpoint, "relay endpoint (https base URL)")
	flags.StringVar(&accessToken, "access-token", "", "access token for authenticated request")
	flags.StringVar(&authFile, "auth-file", lingon.DefaultAuthPath(), "path to auth file")

	return cmd
}
