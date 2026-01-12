package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
	"pkt.systems/pslog"
)

// NewRootCommand builds the root CLI command.
func NewRootCommand(loader *lingon.Loader) *cobra.Command {
	var configFile string
	var endpoint string
	var sessionID string
	var token string
	var cols int
	var rows int
	var authFile string
	var shellPath string
	var bufferLines int
	var termName string

	v := loader.Viper()
	v.SetDefault("client.endpoint", lingon.DefaultClientEndpoint)
	v.SetDefault("client.auth_file", lingon.DefaultAuthPath())
	v.SetDefault("client.log_file", lingon.DefaultLogPath())
	v.SetDefault("client.buffer_lines", lingon.DefaultBufferLines)
	v.SetDefault("terminal.term", lingon.DefaultTerminalTerm)

	cmd := &cobra.Command{
		Use:   "lingon",
		Short: "Lingon interactive terminal and relay",
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			if configFile != "" {
				loader.SetConfigFile(configFile)
			}
		},
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
			logPath := cfg.Client.LogFile
			if logPath == "" {
				logPath = lingon.DefaultLogPath()
			}
			termValue := termName
			if !cmd.Flags().Changed("term") {
				termValue = cfg.Terminal.Term
			}

			tokenValue := token
			if endpointValue != "" {
				if !cmd.Flags().Changed("token") {
					resolved, err := resolveAccessToken(cmd.Context(), endpointValue, authPath)
					if err != nil {
						return err
					}
					tokenValue = resolved
				}
				if tokenValue == "" {
					return fmt.Errorf("access token is required")
				}
			}

			logger, closer, err := openClientLogger(logPath)
			if err != nil {
				return err
			}
			defer func() {
				_ = closer.Close()
			}()
			logger = logger.With("component", "interactive")
			ctx := pslog.ContextWithLogger(cmd.Context(), logger)
			colsValue := cols
			if !cmd.Flags().Changed("cols") {
				colsValue = 0
			}
			rowsValue := rows
			if !cmd.Flags().Changed("rows") {
				rowsValue = 0
			}
			bufferValue := cfg.Client.BufferLines
			if cmd.Flags().Changed("buffer-lines") {
				bufferValue = bufferLines
			}
			return lingon.Interactive(ctx, lingon.InteractiveOptions{
				Endpoint:       endpointValue,
				Token:          tokenValue,
				SessionID:      sessionID,
				Cols:           colsValue,
				Rows:           rowsValue,
				Shell:          shellPath,
				Term:           termValue,
				Publish:        endpointValue != "",
				PublishControl: true,
				BufferLines:    bufferValue,
				Logger:         logger,
			})
		},
	}

	cmd.PersistentFlags().StringVar(&configFile, "config", "", "config file path")

	flags := cmd.Flags()
	flags.StringVarP(&endpoint, "endpoint", "e", lingon.DefaultClientEndpoint, "relay endpoint (https/wss base URL)")
	flags.StringVar(&sessionID, "session", lingon.DefaultSessionID, "session id")
	flags.StringVar(&token, "token", "", "access token (overrides stored auth)")
	flags.IntVar(&cols, "cols", lingon.DefaultTerminalCols, "initial columns")
	flags.IntVar(&rows, "rows", lingon.DefaultTerminalRows, "initial rows")
	flags.StringVar(&authFile, "auth-file", lingon.DefaultAuthPath(), "path to auth file")
	flags.StringVar(&shellPath, "shell", "", "override login shell path")
	flags.IntVar(&bufferLines, "buffer-lines", lingon.DefaultBufferLines, "max lines to buffer while offline")
	flags.StringVar(&termName, "term", lingon.DefaultTerminalTerm, "TERM for the PTY session")

	cmd.AddCommand(NewAttachCommand(loader))
	cmd.AddCommand(NewShareCommand(loader))
	cmd.AddCommand(NewLoginCommand(loader))
	cmd.AddCommand(NewUsersCommand(loader))
	cmd.AddCommand(NewSessionsCommand(loader))
	cmd.AddCommand(NewServeCommand(loader))
	cmd.AddCommand(NewTLSCommand())
	cmd.AddCommand(NewBootstrapCommand())

	return cmd
}
