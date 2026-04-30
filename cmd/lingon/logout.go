package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
)

// NewLogoutCommand builds the logout command.
func NewLogoutCommand(loader *lingon.Loader) *cobra.Command {
	var endpoint string
	var authFile string
	var accessToken string
	var refreshToken string

	v := loader.Viper()
	v.SetDefault("client.endpoint", lingon.DefaultClientEndpoint)
	v.SetDefault("client.auth_file", lingon.DefaultAuthPath())

	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Log out and remove stored auth for an endpoint",
		RunE: func(cmd *cobra.Command, _ []string) error {
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

			return lingon.Logout(cmd.Context(), lingon.LogoutOptions{
				Endpoint:     endpointValue,
				AuthFile:     authPath,
				RefreshToken: refreshToken,
				AccessToken:  accessToken,
				TLSDir:       tlsDir,
				Insecure:     insecure,
			})
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&endpoint, "endpoint", "e", lingon.DefaultClientEndpoint, "relay endpoint (https/wss base URL; assumes https:// if omitted)")
	flags.StringVar(&authFile, "auth-file", lingon.DefaultAuthPath(), "path to auth file")
	flags.StringVar(&accessToken, "access-token", "", "access token override for remote logout")
	flags.StringVar(&refreshToken, "refresh-token", "", "refresh token override for remote logout")
	registerEndpointFlagCompletion(cmd, loader)

	return cmd
}
