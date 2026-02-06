package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
	"pkt.systems/pslog"
)

const rootUsageTemplate = `Usage:{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{else if .Runnable}}
  {{.UseLine}}{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`

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
	var scrollbackLines int
	var termName string
	var respawn bool
	var offline bool
	var hostnameOnly bool
	var themeName string
	var logFile string
	var traceEnabled bool
	var traceFile string

	v := loader.Viper()
	v.SetDefault("client.endpoint", lingon.DefaultClientEndpoint)
	v.SetDefault("client.auth_file", lingon.DefaultAuthPath())
	v.SetDefault("client.log_file", lingon.DefaultLogPath())
	v.SetDefault("terminal.term", lingon.DefaultTerminalTermValue())
	v.SetDefault("terminal.scrollback_lines", lingon.DefaultScrollbackLines)
	v.SetDefault("terminal.respawn", lingon.DefaultTerminalRespawn)
	v.SetDefault("terminal.theme", lingon.DefaultTerminalTheme)
	v.SetDefault("terminal.hostname_only", lingon.DefaultTerminalHostnameOnly)

	cmd := &cobra.Command{
		Use:           "lingon",
		Short:         "Lingon interactive terminal and relay https://pkt.systems/lingon",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if err := startPprofServer(); err != nil {
				return err
			}
			if configFile != "" {
				loader.SetConfigFile(configFile)
			}
			return nil
		},
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
			logPath := logFile
			if !cmd.Flags().Changed("log-file") {
				logPath = cfg.Client.LogFile
			}
			if logPath == "" {
				logPath = lingon.DefaultLogPath()
			}
			termValue := termName
			if !cmd.Flags().Changed("term") {
				termValue = cfg.Terminal.Term
			}
			respawnValue := respawn
			if !cmd.Flags().Changed("respawn") {
				respawnValue = cfg.Terminal.Respawn
			}
			hostnameOnlyValue := hostnameOnly
			if !cmd.Flags().Changed("hostname-only") {
				hostnameOnlyValue = cfg.Terminal.HostnameOnly
			}
			themeValue := themeName
			if !cmd.Flags().Changed("theme") {
				themeValue = cfg.Terminal.Theme
			}
			resolvedTheme, err := resolveTheme(themeValue)
			if err != nil {
				return err
			}

			tokenValue := token
			if endpointValue != "" {
				if !cmd.Flags().Changed("token") {
					resolved, err := resolveAccessToken(cmd.Context(), endpointValue, authPath, tlsDir, insecure)
					if err != nil {
						ok, refreshErr := hasValidRefreshToken(endpointValue, authPath, time.Now())
						if refreshErr != nil || !ok {
							return err
						}
					} else {
						tokenValue = resolved
					}
				}
				if tokenValue == "" && authPath == "" {
					return fmt.Errorf("access token is required")
				}
			}
			authPathValue := authPath
			if cmd.Flags().Changed("token") && !cmd.Flags().Changed("auth-file") {
				authPathValue = ""
			}

			logger, closer, err := openClientLogger(logPath)
			if err != nil {
				return err
			}
			defer func() {
				_ = closer.Close()
			}()
			ctx := pslog.ContextWithLogger(cmd.Context(), logger)
			colsValue := cols
			if !cmd.Flags().Changed("cols") {
				colsValue = 0
			}
			rowsValue := rows
			if !cmd.Flags().Changed("rows") {
				rowsValue = 0
			}
			scrollbackValue := cfg.Terminal.ScrollbackLines
			if cmd.Flags().Changed("scrollback-lines") {
				scrollbackValue = scrollbackLines
			}
			traceWriter, tracePath, err := setupTrace(traceEnabled, traceFile)
			if err != nil {
				return err
			}
			if traceWriter != nil {
				defer func() {
					_ = traceWriter.Close()
					fmt.Fprintln(cmd.OutOrStdout(), formatItalicGray(fmt.Sprintf("-- trace saved to %s --", tracePath)))
				}()
			}
			return lingon.Interactive(ctx, lingon.InteractiveOptions{
				Endpoint:        endpointValue,
				Token:           tokenValue,
				AuthFile:        authPathValue,
				SessionID:       sessionID,
				Cols:            colsValue,
				Rows:            rowsValue,
				Shell:           shellPath,
				Term:            termValue,
				Respawn:         respawnValue,
				Offline:         offline,
				Theme:           resolvedTheme,
				Publish:         endpointValue != "",
				PublishControl:  true,
				HostnameOnly:    hostnameOnlyValue,
				ScrollbackLines: scrollbackValue,
				TLSDir:          tlsDir,
				Insecure:        insecure,
				Logger:          logger,
				Trace:           traceWriter,
				OnExit: func(id string, startedAt time.Time, err error) {
					if err != nil {
						return
					}
					if id == "" {
						return
					}
					elapsed := time.Since(startedAt).Round(time.Second)
					timestamp := time.Now().UTC().Format(time.RFC3339)
					fmt.Fprintln(cmd.OutOrStdout(), formatItalicGray(fmt.Sprintf("-- %s exited %s lasted %s --", id, timestamp, elapsed)))
				},
			})
		},
	}

	cmd.PersistentFlags().StringVarP(&configFile, "config", "c", "", "config file path")
	cmd.PersistentFlags().BoolP("insecure", "k", false, "skip TLS verification")

	flags := cmd.Flags()
	flags.StringVarP(&endpoint, "endpoint", "e", lingon.DefaultClientEndpoint, "relay endpoint (https/wss base URL; assumes https:// if omitted)")
	flags.StringVarP(&sessionID, "session", "s", "", "session id (default: auto-generated)")
	flags.StringVar(&token, "token", "", "access token (overrides stored auth)")
	flags.IntVar(&cols, "cols", lingon.DefaultTerminalCols, "initial columns")
	flags.IntVar(&rows, "rows", lingon.DefaultTerminalRows, "initial rows")
	flags.StringVar(&authFile, "auth-file", lingon.DefaultAuthPath(), "path to auth file")
	flags.StringVar(&shellPath, "shell", "", "override login shell path")
	flags.IntVar(&scrollbackLines, "scrollback-lines", lingon.DefaultScrollbackLines, "max scrollback lines to buffer")
	flags.StringVar(&termName, "term", lingon.DefaultTerminalTermValue(), "TERM for the PTY session")
	flags.BoolVarP(&respawn, "respawn", "r", lingon.DefaultTerminalRespawn, "respawn shell on exit (host sessions only)")
	flags.BoolVarP(&offline, "offline", "o", false, "start host sessions offline (no relay publish/connect until Ctrl+L o)")
	flags.BoolVar(&hostnameOnly, "hostname-only", lingon.DefaultTerminalHostnameOnly, "show only hostname in connect/disconnect banners")
	flags.StringVar(&themeName, "theme", lingon.DefaultTerminalTheme, "theme for TUI chrome (use `lingon themes` to list)")
	flags.StringVar(&logFile, "log-file", "", "path to client log file (disabled if empty)")
	flags.BoolVar(&traceEnabled, "trace", false, "write a JSONL trace for host/attach TUIs")
	flags.StringVar(&traceFile, "trace-file", "", "path to trace output file (implies --trace)")
	registerEndpointFlagCompletion(cmd, loader)

	cmd.AddCommand(NewAttachCommand(loader))
	cmd.AddCommand(NewSendCommand(loader))
	cmd.AddCommand(NewWallCommand(loader))
	cmd.AddCommand(NewShareCommand(loader))
	cmd.AddCommand(NewLoginCommand(loader))
	cmd.AddCommand(NewLogoutCommand(loader))
	cmd.AddCommand(NewUsersCommand(loader))
	cmd.AddCommand(NewSessionsCommand(loader))
	cmd.AddCommand(NewThemesCommand())
	cmd.AddCommand(NewServeCommand(loader))
	cmd.AddCommand(NewTLSCommand())
	cmd.AddCommand(NewBootstrapCommand())
	cmd.AddCommand(NewTestCommand())
	cmd.AddCommand(NewVersionCommand())
	cmd.SetUsageTemplate(rootUsageTemplate)

	return cmd
}
