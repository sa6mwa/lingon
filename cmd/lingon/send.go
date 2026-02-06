package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
)

// NewSendCommand builds the send command.
func NewSendCommand(loader *lingon.Loader) *cobra.Command {
	var endpoint string
	var shareToken string
	var accessToken string
	var requestControl bool
	var authFile string
	var noNewline bool
	var logFile string

	v := loader.Viper()
	v.SetDefault("client.endpoint", lingon.DefaultClientEndpoint)
	v.SetDefault("client.auth_file", lingon.DefaultAuthPath())
	v.SetDefault("client.log_file", lingon.DefaultLogPath())

	cmd := &cobra.Command{
		Use:   "send <session-id> -- <input tokens...>",
		Short: "Send input to a Lingon session",
		Args:  cobra.ArbitraryArgs,
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
			logPath := logFile
			if !cmd.Flags().Changed("log-file") {
				logPath = cfg.Client.LogFile
			}
			if logPath == "" {
				logPath = lingon.DefaultLogPath()
			}
			logger, closer, err := openClientLogger(logPath)
			if err != nil {
				return err
			}
			defer func() {
				_ = closer.Close()
			}()

			endpointValue := endpoint
			if !cmd.Flags().Changed("endpoint") {
				endpointValue = cfg.Client.Endpoint
			}
			if endpointValue == "" {
				return fmt.Errorf("endpoint is required")
			}

			if shareToken != "" {
				resolvedToken, tokenEndpoint, err := resolveShareToken(shareToken)
				if err != nil {
					return err
				}
				shareToken = resolvedToken
				if tokenEndpoint != "" && !cmd.Flags().Changed("endpoint") {
					endpointValue = tokenEndpoint
				}
			}

			authPath := authFile
			if !cmd.Flags().Changed("auth-file") {
				authPath = cfg.Client.AuthFile
			}

			if len(args) == 0 {
				return fmt.Errorf("session id is required")
			}
			sessionID := args[0]
			inputTokens := args[1:]
			if len(inputTokens) == 0 {
				return fmt.Errorf("input tokens are required; pass them after --")
			}

			tokenValue := accessToken
			if shareToken == "" && !cmd.Flags().Changed("access-token") {
				resolved, err := resolveAccessToken(cmd.Context(), endpointValue, authPath, tlsDir, insecure)
				if err != nil {
					return err
				}
				tokenValue = resolved
			}
			if shareToken == "" && tokenValue == "" {
				return fmt.Errorf("access token is required")
			}

			return lingon.SendInput(cmd.Context(), lingon.SendInputOptions{
				Endpoint:       endpointValue,
				SessionID:      sessionID,
				AccessToken:    tokenValue,
				ShareToken:     shareToken,
				RequestControl: requestControl,
				Tokens:         inputTokens,
				NoNewline:      noNewline,
				TLSDir:         tlsDir,
				Insecure:       insecure,
				Logger:         logger,
			})
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&endpoint, "endpoint", "e", lingon.DefaultClientEndpoint, "relay endpoint (https/wss base URL; assumes https:// if omitted)")
	flags.StringVarP(&shareToken, "token", "t", "", "share token for anonymous attach")
	flags.StringVar(&accessToken, "access-token", "", "access token for authenticated attach")
	flags.BoolVarP(&requestControl, "request-control", "", false, "request controller lease on connect")
	flags.StringVar(&authFile, "auth-file", lingon.DefaultAuthPath(), "path to auth file")
	flags.StringVar(&logFile, "log-file", "", "path to client log file (disabled if empty)")
	flags.BoolVar(&noNewline, "no-newline", false, "disable auto-newline when sending input tokens")
	registerEndpointFlagCompletion(cmd, loader)

	cmd.ValidArgsFunction = attachSessionCompletion(loader, &endpoint, &accessToken, &authFile)

	return cmd
}
