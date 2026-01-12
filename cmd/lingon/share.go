package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
	"pkt.systems/pslog"
)

// NewShareCommand builds the share management command.
func NewShareCommand(loader *lingon.Loader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "share",
		Short: "Manage share tokens",
	}

	cmd.AddCommand(newShareCreateCommand(loader))
	cmd.AddCommand(newShareRevokeCommand(loader))

	return cmd
}

func newShareCreateCommand(loader *lingon.Loader) *cobra.Command {
	var endpoint string
	var accessToken string
	var authFile string
	var scope string
	var ttl string

	v := loader.Viper()
	v.SetDefault("client.endpoint", lingon.DefaultClientEndpoint)
	v.SetDefault("client.auth_file", lingon.DefaultAuthPath())

	cmd := &cobra.Command{
		Use:   "create [session-id]",
		Short: "Create a share token",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = pslog.Ctx(cmd.Context())

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

			sessionID := lingon.DefaultSessionID
			if len(args) > 0 {
				sessionID = args[0]
			}
			if sessionID == "" {
				return fmt.Errorf("session id is required")
			}

			var ttlValue time.Duration
			if ttl != "" {
				parsed, err := time.ParseDuration(ttl)
				if err != nil {
					return fmt.Errorf("invalid ttl: %w", err)
				}
				ttlValue = parsed
			}

			resp, err := lingon.ShareCreate(cmd.Context(), lingon.ShareCreateOptions{
				Endpoint:    endpointValue,
				AccessToken: tokenValue,
				SessionID:   sessionID,
				Scope:       lingon.ShareScope(scope),
				TTL:         ttlValue,
			})
			if err != nil {
				return err
			}
			return json.NewEncoder(os.Stdout).Encode(resp)
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&endpoint, "endpoint", "e", lingon.DefaultClientEndpoint, "relay endpoint (https base URL)")
	flags.StringVar(&accessToken, "access-token", "", "access token for authenticated request")
	flags.StringVar(&authFile, "auth-file", lingon.DefaultAuthPath(), "path to auth file")
	flags.StringVar(&scope, "scope", string(lingon.ShareScopeView), "share scope: view or control")
	flags.StringVar(&ttl, "ttl", "", "token ttl (e.g. 1h, 30m)")

	return cmd
}

func newShareRevokeCommand(loader *lingon.Loader) *cobra.Command {
	var endpoint string
	var accessToken string
	var authFile string

	v := loader.Viper()
	v.SetDefault("client.endpoint", lingon.DefaultClientEndpoint)
	v.SetDefault("client.auth_file", lingon.DefaultAuthPath())

	cmd := &cobra.Command{
		Use:   "revoke <token>",
		Short: "Revoke a share token",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = pslog.Ctx(cmd.Context())

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

			resp, err := lingon.ShareRevoke(cmd.Context(), lingon.ShareRevokeOptions{
				Endpoint:    endpointValue,
				AccessToken: tokenValue,
				Token:       args[0],
			})
			if err != nil {
				return err
			}
			return json.NewEncoder(os.Stdout).Encode(resp)
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&endpoint, "endpoint", "e", lingon.DefaultClientEndpoint, "relay endpoint (https base URL)")
	flags.StringVar(&accessToken, "access-token", "", "access token for authenticated request")
	flags.StringVar(&authFile, "auth-file", lingon.DefaultAuthPath(), "path to auth file")

	return cmd
}
