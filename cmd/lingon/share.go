package main

import (
	"fmt"
	"strings"
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
	cmd.AddCommand(newShareListCommand(loader))
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
			tlsDir := cfg.Server.TLS.Dir
			insecure, err := cmd.Flags().GetBool("insecure")
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
				resolved, err := resolveAccessToken(cmd.Context(), endpointValue, authPath, tlsDir, insecure)
				if err != nil {
					return err
				}
				tokenValue = resolved
			}
			if tokenValue == "" {
				return fmt.Errorf("access token is required")
			}

			var sessionID string
			if len(args) > 0 {
				sessionID = args[0]
			}
			if sessionID == "" {
				sessions, err := lingon.ListSessionsWithTLSDirInsecure(cmd.Context(), endpointValue, tokenValue, tlsDir, insecure)
				if err != nil {
					return err
				}
				if len(sessions) == 0 {
					return fmt.Errorf("no sessions available")
				}
				if len(sessions) > 1 {
					return fmt.Errorf("multiple sessions found; pass a session id or run `lingon sessions`")
				}
				sessionID = sessions[0].ID
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
				TLSDir:      tlsDir,
				Insecure:    insecure,
			})
			if err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), resp)
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&endpoint, "endpoint", "e", lingon.DefaultClientEndpoint, "relay endpoint (https/wss base URL; assumes https:// if omitted)")
	flags.StringVar(&accessToken, "access-token", "", "access token for authenticated request")
	flags.StringVar(&authFile, "auth-file", lingon.DefaultAuthPath(), "path to auth file")
	flags.StringVarP(&scope, "scope", "s", string(lingon.ShareScopeView), "share scope: view or control")
	flags.StringVar(&ttl, "ttl", "", "token ttl (e.g. 1h, 30m)")
	registerEndpointFlagCompletion(cmd, loader)
	cmd.ValidArgsFunction = attachSessionCompletion(loader, &endpoint, &accessToken, &authFile)

	return cmd
}

func newShareListCommand(loader *lingon.Loader) *cobra.Command {
	var endpoint string
	var accessToken string
	var authFile string
	var listAll bool
	var listValid bool
	var listRevoked bool
	var listExpired bool

	v := loader.Viper()
	v.SetDefault("client.endpoint", lingon.DefaultClientEndpoint)
	v.SetDefault("client.auth_file", lingon.DefaultAuthPath())

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List share tokens",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = pslog.Ctx(cmd.Context())

			cfg, err := loader.Load()
			if err != nil {
				return err
			}
			tlsDir := cfg.Server.TLS.Dir
			insecure, err := cmd.Flags().GetBool("insecure")
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
				resolved, err := resolveAccessToken(cmd.Context(), endpointValue, authPath, tlsDir, insecure)
				if err != nil {
					return err
				}
				tokenValue = resolved
			}
			if tokenValue == "" {
				return fmt.Errorf("access token is required")
			}

			statuses := make([]lingon.ShareListStatus, 0, 3)
			if listAll {
				statuses = append(statuses, lingon.ShareListStatusValid, lingon.ShareListStatusRevoked, lingon.ShareListStatusExpired)
			} else {
				if !listValid && !listRevoked && !listExpired {
					listValid = true
				}
				if listValid {
					statuses = append(statuses, lingon.ShareListStatusValid)
				}
				if listRevoked {
					statuses = append(statuses, lingon.ShareListStatusRevoked)
				}
				if listExpired {
					statuses = append(statuses, lingon.ShareListStatusExpired)
				}
			}

			tokens, err := lingon.ShareList(cmd.Context(), lingon.ShareListOptions{
				Endpoint:    endpointValue,
				AccessToken: tokenValue,
				Statuses:    statuses,
				TLSDir:      tlsDir,
				Insecure:    insecure,
			})
			if err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), tokens)
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&endpoint, "endpoint", "e", lingon.DefaultClientEndpoint, "relay endpoint (https/wss base URL; assumes https:// if omitted)")
	flags.StringVar(&accessToken, "access-token", "", "access token for authenticated request")
	flags.StringVar(&authFile, "auth-file", lingon.DefaultAuthPath(), "path to auth file")
	flags.BoolVarP(&listAll, "all", "a", false, "list all share tokens")
	flags.BoolVar(&listValid, "valid", false, "include valid share tokens")
	flags.BoolVar(&listRevoked, "revoked", false, "include revoked share tokens")
	flags.BoolVar(&listExpired, "expired", false, "include expired share tokens")
	registerEndpointFlagCompletion(cmd, loader)

	return cmd
}

func newShareRevokeCommand(loader *lingon.Loader) *cobra.Command {
	var endpoint string
	var accessToken string
	var authFile string
	var revokeAll bool

	v := loader.Viper()
	v.SetDefault("client.endpoint", lingon.DefaultClientEndpoint)
	v.SetDefault("client.auth_file", lingon.DefaultAuthPath())

	cmd := &cobra.Command{
		Use:   "revoke <token|all>",
		Short: "Revoke a share token",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = pslog.Ctx(cmd.Context())

			cfg, err := loader.Load()
			if err != nil {
				return err
			}
			tlsDir := cfg.Server.TLS.Dir
			insecure, err := cmd.Flags().GetBool("insecure")
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
				resolved, err := resolveAccessToken(cmd.Context(), endpointValue, authPath, tlsDir, insecure)
				if err != nil {
					return err
				}
				tokenValue = resolved
			}
			if tokenValue == "" {
				return fmt.Errorf("access token is required")
			}

			var token string
			if len(args) > 0 {
				token = args[0]
			}
			if revokeAll || token == "all" {
				if token != "" && token != "all" {
					return fmt.Errorf("cannot pass a token when using --all")
				}
				resp, err := lingon.ShareRevokeAll(cmd.Context(), lingon.ShareRevokeAllOptions{
					Endpoint:    endpointValue,
					AccessToken: tokenValue,
					TLSDir:      tlsDir,
					Insecure:    insecure,
				})
				if err != nil {
					return err
				}
				return printJSON(cmd.OutOrStdout(), resp)
			}
			if token == "" {
				return fmt.Errorf("token is required")
			}
			resp, err := lingon.ShareRevoke(cmd.Context(), lingon.ShareRevokeOptions{
				Endpoint:    endpointValue,
				AccessToken: tokenValue,
				Token:       token,
				TLSDir:      tlsDir,
				Insecure:    insecure,
			})
			if err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), resp)
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&endpoint, "endpoint", "e", lingon.DefaultClientEndpoint, "relay endpoint (https/wss base URL; assumes https:// if omitted)")
	flags.StringVar(&accessToken, "access-token", "", "access token for authenticated request")
	flags.StringVar(&authFile, "auth-file", lingon.DefaultAuthPath(), "path to auth file")
	flags.BoolVarP(&revokeAll, "all", "a", false, "revoke all share tokens")
	registerEndpointFlagCompletion(cmd, loader)
	cmd.ValidArgsFunction = shareRevokeCompletion(loader, &endpoint, &accessToken, &authFile)

	return cmd
}

func shareRevokeCompletion(loader *lingon.Loader, endpoint, accessToken, authFile *string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		cfg, err := loader.Load()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		tlsDir := cfg.Server.TLS.Dir
		insecure, err := cmd.Flags().GetBool("insecure")
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		endpointValue := *endpoint
		if !cmd.Flags().Changed("endpoint") {
			endpointValue = cfg.Client.Endpoint
		}
		if endpointValue == "" {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		authPath := *authFile
		if !cmd.Flags().Changed("auth-file") {
			authPath = cfg.Client.AuthFile
		}

		tokenValue := *accessToken
		if !cmd.Flags().Changed("access-token") {
			resolved, err := lingon.EnsureAccessTokenWithTLSDirInsecure(cmd.Context(), endpointValue, authPath, tlsDir, insecure)
			if err != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			tokenValue = resolved.AccessToken
		}
		if tokenValue == "" {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		tokens, err := lingon.ShareList(cmd.Context(), lingon.ShareListOptions{
			Endpoint:    endpointValue,
			AccessToken: tokenValue,
			Statuses:    []lingon.ShareListStatus{lingon.ShareListStatusValid},
			TLSDir:      tlsDir,
			Insecure:    insecure,
		})
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		out := make([]string, 0, len(tokens)+1)
		if strings.HasPrefix("all", toComplete) {
			out = append(out, "all\tall share tokens")
		}
		seen := make(map[string]struct{}, len(tokens))
		for _, tok := range tokens {
			if tok.Token == "" {
				continue
			}
			if _, ok := seen[tok.Token]; ok {
				continue
			}
			seen[tok.Token] = struct{}{}
			if !strings.HasPrefix(tok.Token, toComplete) {
				continue
			}
			if tok.SessionID != "" {
				out = append(out, tok.Token+"\t"+tok.SessionID)
				continue
			}
			out = append(out, tok.Token)
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}
