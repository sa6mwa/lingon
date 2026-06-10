package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/headless"
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
	var wallInactiveAfter string
	var termName string
	var respawn bool
	var offline bool
	var disableDesktopNotifications bool
	var hostnameOnly bool
	var themeName string
	var logFile string
	var traceEnabled bool
	var traceFile string
	var configDir string
	headlessDefault := headlessAliasEnabled(os.Args[0])
	var headlessMode bool

	v := loader.Viper()
	setConfigRootDefaults(v)
	v.SetDefault("client.endpoint", lingon.DefaultClientEndpoint)
	v.SetDefault("terminal.term", lingon.DefaultTerminalTermValue())
	v.SetDefault("terminal.scrollback_lines", lingon.DefaultScrollbackLines)
	v.SetDefault("terminal.respawn", lingon.DefaultTerminalRespawn)
	v.SetDefault("terminal.theme", lingon.DefaultTerminalTheme)
	v.SetDefault("terminal.hostname_only", lingon.DefaultTerminalHostnameOnly)
	v.SetDefault("terminal.disable_desktop_notifications", lingon.DefaultTerminalDisableDesktopNotifications)
	v.SetDefault("terminal.wall_inactive_after", lingon.DefaultWallInactiveAfterCSV)

	cmd := &cobra.Command{
		Use:           "lingon",
		Short:         "Lingon interactive terminal and relay https://pkt.systems/lingon",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if err := startPprofServer(); err != nil {
				return err
			}
			if err := applyConfigRoot(configDir, loader, cmd.Root()); err != nil {
				return err
			}
			if configFile != "" {
				loader.SetConfigFile(configFile)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			envForeground := headless.IsForegroundEnv(os.Getenv(headless.EnvForeground))
			headlessRequested := headlessMode || envForeground
			if headlessRequested {
				cfg, err := loader.Load()
				if err != nil {
					return err
				}
				cfgDir := configDirForLoader(loader)
				if !envForeground {
					return startHeadlessReexec(cmd, cfgDir)
				}
				if err := os.Unsetenv(headless.EnvForeground); err != nil {
					return err
				}
				return runHeadlessForeground(cmd, loader, cfgDir, cfg)
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
			disableDesktopNotificationsValue := disableDesktopNotifications
			if !cmd.Flags().Changed("disable-desktop-notifications") {
				disableDesktopNotificationsValue = cfg.Terminal.DisableDesktopNotifications
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
			colsValue, rowsValue, err := resolveRootHostSize(cmd, cols, rows)
			if err != nil {
				return err
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
				Endpoint:                    endpointValue,
				Token:                       tokenValue,
				AuthFile:                    authPathValue,
				SessionID:                   sessionID,
				Cols:                        colsValue,
				Rows:                        rowsValue,
				Shell:                       shellPath,
				Term:                        termValue,
				Respawn:                     respawnValue,
				Offline:                     offline,
				Theme:                       resolvedTheme,
				Publish:                     endpointValue != "",
				PublishControl:              true,
				HostnameOnly:                hostnameOnlyValue,
				DisableDesktopNotifications: disableDesktopNotificationsValue,
				ScrollbackLines:             scrollbackValue,
				TLSDir:                      tlsDir,
				Insecure:                    insecure,
				Logger:                      logger,
				Trace:                       traceWriter,
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
	cmd.PersistentFlags().StringVarP(&configDir, "config-dir", "C", lingon.DefaultConfigDir(), "Lingon config root directory")
	cmd.PersistentFlags().BoolP("insecure", "k", false, "skip TLS verification")
	cmd.PersistentFlags().BoolVarP(&headlessMode, "headless", "x", headlessDefault, "use local headless session mode")

	flags := cmd.Flags()
	flags.StringVarP(&endpoint, "endpoint", "e", lingon.DefaultClientEndpoint, "relay endpoint (https/wss base URL; assumes https:// if omitted)")
	flags.StringVarP(&sessionID, "session", "s", "", "session id (default: auto-generated)")
	flags.StringVar(&token, "token", "", "access token (overrides stored auth)")
	flags.IntVar(&cols, "cols", lingon.DefaultTerminalCols, "initial columns")
	flags.IntVar(&rows, "rows", lingon.DefaultTerminalRows, "initial rows")
	flags.StringP(geometryFlagName, "g", "", "initial terminal geometry as COLSxROWS, for example 80x24 (overrides --cols/--rows)")
	flags.StringVar(&authFile, "auth-file", lingon.DefaultAuthPath(), "path to auth file")
	flags.StringVar(&shellPath, "shell", "", "override login shell path")
	flags.IntVar(&scrollbackLines, "scrollback-lines", lingon.DefaultScrollbackLines, "max scrollback lines to buffer")
	flags.StringVar(&wallInactiveAfter, "wall-inactive-after", lingon.DefaultWallInactiveAfterCSV, "CSV inactivity levels for Ctrl+L w cycling in local headless mode; empty uses defaults")
	flags.StringVar(&termName, "term", lingon.DefaultTerminalTermValue(), "TERM for the PTY session")
	flags.BoolVarP(&respawn, "respawn", "r", lingon.DefaultTerminalRespawn, "respawn shell on exit (host sessions only)")
	flags.BoolVarP(&offline, "offline", "o", false, "start host sessions offline (no relay publish/connect until Ctrl+L o)")
	flags.BoolVar(&hostnameOnly, "hostname-only", lingon.DefaultTerminalHostnameOnly, "show only hostname in connect/disconnect banners")
	flags.BoolVar(&disableDesktopNotifications, "disable-desktop-notifications", lingon.DefaultTerminalDisableDesktopNotifications, "disable best-effort desktop notifications for inactivity walls")
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
	cmd.AddCommand(NewDetachCommand(loader))
	cmd.AddCommand(NewThemesCommand())
	cmd.AddCommand(NewServeCommand(loader))
	cmd.AddCommand(NewTLSCommand())
	cmd.AddCommand(NewBootstrapCommand())
	cmd.AddCommand(NewTestCommand())
	cmd.AddCommand(NewVersionCommand())
	cmd.SetUsageTemplate(rootUsageTemplate)

	return cmd
}
