package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"pkt.systems/lingon"
)

// NewAttachCommand builds the attach command.
func NewAttachCommand(loader *lingon.Loader) *cobra.Command {
	var endpoint string
	var shareToken string
	var accessToken string
	var requestControl bool
	var pick bool
	var authFile string
	var logFile string
	var hostnameOnly bool
	var disableDesktopNotifications bool
	var themeName string
	var traceEnabled bool
	var traceFile string

	v := loader.Viper()
	v.SetDefault("client.endpoint", lingon.DefaultClientEndpoint)
	v.SetDefault("client.auth_file", lingon.DefaultAuthPath())
	v.SetDefault("client.log_file", lingon.DefaultLogPath())
	v.SetDefault("terminal.theme", lingon.DefaultTerminalTheme)
	v.SetDefault("terminal.hostname_only", lingon.DefaultTerminalHostnameOnly)
	v.SetDefault("terminal.disable_desktop_notifications", lingon.DefaultTerminalDisableDesktopNotifications)

	cmd := &cobra.Command{
		Use:   "attach [session-id]",
		Short: "Attach to a Lingon session",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			headlessMode, err := cmd.Flags().GetBool("headless")
			if err != nil {
				return err
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

			var sessionID string
			if len(args) > 0 {
				sessionID = args[0]
			}
			if pick && sessionID != "" {
				return fmt.Errorf("cannot use --pick with an explicit session id")
			}

			themeValue := themeName
			if !cmd.Flags().Changed("theme") {
				themeValue = cfg.Terminal.Theme
			}
			hostnameOnlyValue := hostnameOnly
			if !cmd.Flags().Changed("hostname-only") {
				hostnameOnlyValue = cfg.Terminal.HostnameOnly
			}
			disableDesktopNotificationsValue := disableDesktopNotifications
			if !cmd.Flags().Changed("disable-desktop-notifications") {
				disableDesktopNotificationsValue = cfg.Terminal.DisableDesktopNotifications
			}
			resolvedTheme, err := resolveTheme(themeValue)
			if err != nil {
				return err
			}

			startedAt := time.Now()
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

			if headlessMode {
				if shareToken != "" {
					return fmt.Errorf("share token mode is unavailable for --headless attach")
				}
				if accessToken != "" {
					return fmt.Errorf("access token mode is unavailable for --headless attach")
				}
				sessions, err := listLocalHeadlessSessions(configDirForLoader(loader))
				if err != nil {
					return err
				}
				if pick {
					selected, err := chooseSession(localSessionsAsRelaySessions(sessions))
					if err != nil {
						return err
					}
					if selected == "" {
						return fmt.Errorf("no local headless sessions available")
					}
					sessionID = selected
				}
				if len(sessions) == 0 {
					return fmt.Errorf("no local headless sessions available")
				}
				if sessionID != "" {
					if _, err := findLocalHeadlessSession(sessions, sessionID); err != nil {
						return err
					}
				}
				err = lingon.Attach(cmd.Context(), lingon.AttachOptions{
					Endpoint:                    "local://headless",
					SessionID:                   sessionID,
					HeadlessConfigDir:           configDirForLoader(loader),
					RequestControl:              requestControl,
					HostnameOnly:                hostnameOnlyValue,
					DisableDesktopNotifications: disableDesktopNotificationsValue,
					Theme:                       resolvedTheme,
					Logger:                      logger,
					Trace:                       traceWriter,
				})
				if err != nil {
					return err
				}
				elapsed := time.Since(startedAt).Round(time.Second)
				fmt.Fprintln(cmd.OutOrStdout(), formatItalicGray(fmt.Sprintf("-- detached (attached for %s) --", elapsed)))
				return nil
			}

			authPath := authFile
			if !cmd.Flags().Changed("auth-file") {
				authPath = cfg.Client.AuthFile
			}
			endpointValue := ""
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
			if endpointValue == "" {
				if shareToken != "" || cmd.Flags().Changed("access-token") {
					endpointValue = resolveConfiguredEndpointValue(cmd, loader, cfg.Client.Endpoint, endpoint)
				} else {
					endpointValue, err = resolveEndpointValue(cmd, loader, cfg.Client.Endpoint, endpoint, authPath)
					if err != nil {
						return err
					}
				}
			}
			if endpointValue == "" {
				return fmt.Errorf("endpoint is required")
			}
			if pick && shareToken != "" {
				return fmt.Errorf("cannot use --pick with a share token")
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
			authPathValue := authPath
			if shareToken != "" {
				authPathValue = ""
			} else if cmd.Flags().Changed("access-token") && !cmd.Flags().Changed("auth-file") {
				authPathValue = ""
			}
			if pick {
				sessions, err := lingon.ListSessionsWithTLSDirInsecure(cmd.Context(), endpointValue, tokenValue, tlsDir, insecure)
				if err != nil {
					return err
				}
				selected, err := chooseSession(sessions)
				if err != nil {
					return err
				}
				if selected == "" {
					return fmt.Errorf("no sessions available")
				}
				sessionID = selected
			}

			if sessionID == "" && shareToken == "" {
				sessions, err := lingon.ListSessionsWithTLSDirInsecure(cmd.Context(), endpointValue, tokenValue, tlsDir, insecure)
				if err != nil {
					return err
				}
				if len(sessions) == 0 {
					return fmt.Errorf("no sessions available")
				}
				sessionID = sessions[0].ID
			}
			err = lingon.Attach(cmd.Context(), lingon.AttachOptions{
				Endpoint:                    endpointValue,
				SessionID:                   sessionID,
				AccessToken:                 tokenValue,
				ShareToken:                  shareToken,
				RequestControl:              requestControl,
				HostnameOnly:                hostnameOnlyValue,
				DisableDesktopNotifications: disableDesktopNotificationsValue,
				AuthFile:                    authPathValue,
				TLSDir:                      tlsDir,
				Insecure:                    insecure,
				Theme:                       resolvedTheme,
				Logger:                      logger,
				Trace:                       traceWriter,
			})
			if err != nil {
				return err
			}
			elapsed := time.Since(startedAt).Round(time.Second)
			fmt.Fprintln(cmd.OutOrStdout(), formatItalicGray(fmt.Sprintf("-- detached (attached for %s) --", elapsed)))
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&endpoint, "endpoint", "e", lingon.DefaultClientEndpoint, "relay endpoint (https/wss base URL; assumes https:// if omitted)")
	flags.StringVarP(&shareToken, "token", "t", "", "share token for anonymous attach")
	flags.StringVar(&accessToken, "access-token", "", "access token for authenticated attach")
	flags.BoolVarP(&requestControl, "request-control", "", false, "request controller lease on connect")
	flags.BoolVar(&pick, "pick", false, "interactively pick a session")
	flags.StringVar(&authFile, "auth-file", lingon.DefaultAuthPath(), "path to auth file")
	flags.StringVar(&logFile, "log-file", "", "path to client log file (disabled if empty)")
	flags.BoolVar(&hostnameOnly, "hostname-only", lingon.DefaultTerminalHostnameOnly, "show only hostname in connect/disconnect banners")
	flags.BoolVar(&disableDesktopNotifications, "disable-desktop-notifications", lingon.DefaultTerminalDisableDesktopNotifications, "disable best-effort desktop notifications for inactivity walls")
	flags.StringVar(&themeName, "theme", lingon.DefaultTerminalTheme, "theme for TUI chrome (use `lingon themes` to list)")
	flags.BoolVar(&traceEnabled, "trace", false, "write a JSONL trace for host/attach TUIs")
	flags.StringVar(&traceFile, "trace-file", "", "path to trace output file (implies --trace)")
	registerEndpointFlagCompletion(cmd, loader)

	cmd.ValidArgsFunction = attachSessionCompletion(loader, &endpoint, &accessToken, &authFile)

	return cmd
}

func chooseSession(sessions []lingon.Session) (string, error) {
	if len(sessions) == 0 {
		return "", nil
	}
	if len(sessions) == 1 {
		return sessions[0].ID, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("interactive selection requires a TTY")
	}

	fmt.Fprintln(os.Stdout, "Available sessions:")
	for i, session := range sessions {
		label := session.ID
		if session.Name != "" {
			label = fmt.Sprintf("%s (%s)", session.ID, session.Name)
		}
		fmt.Fprintf(os.Stdout, "  [%d] %s\n", i+1, label)
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Fprintf(os.Stdout, "Select session [1-%d]: ", len(sessions))
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return sessions[0].ID, nil
		}
		choice, err := strconv.Atoi(line)
		if err != nil || choice < 1 || choice > len(sessions) {
			fmt.Fprintln(os.Stdout, "Invalid selection")
			continue
		}
		return sessions[choice-1].ID, nil
	}
}

func attachSessionCompletion(loader *lingon.Loader, endpoint, accessToken, authFile *string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		headlessMode, err := cmd.Flags().GetBool("headless")
		if err == nil && headlessMode {
			if _, loadErr := loader.Load(); loadErr != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			sessions, listErr := listLocalHeadlessSessions(configDirForLoader(loader))
			if listErr != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			suggestions := make([]string, 0, len(sessions))
			for _, session := range sessions {
				if strings.HasPrefix(session.ID, toComplete) {
					suggestions = append(suggestions, session.ID)
				}
			}
			return suggestions, cobra.ShellCompDirectiveNoFileComp
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

		authPath := *authFile
		if !cmd.Flags().Changed("auth-file") {
			authPath = cfg.Client.AuthFile
		}
		endpointValue, err := resolveEndpointValue(cmd, loader, cfg.Client.Endpoint, *endpoint, authPath)
		if err != nil || endpointValue == "" {
			return nil, cobra.ShellCompDirectiveNoFileComp
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

		sessions, err := lingon.ListSessionsWithTLSDirInsecure(cmd.Context(), endpointValue, tokenValue, tlsDir, insecure)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var out []string
		for _, session := range sessions {
			if strings.HasPrefix(session.ID, toComplete) {
				out = append(out, session.ID)
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}
