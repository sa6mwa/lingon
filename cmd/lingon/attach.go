package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

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

	v := loader.Viper()
	v.SetDefault("client.endpoint", lingon.DefaultClientEndpoint)
	v.SetDefault("client.auth_file", lingon.DefaultAuthPath())
	v.SetDefault("client.log_file", lingon.DefaultLogPath())

	cmd := &cobra.Command{
		Use:   "attach [session-id]",
		Short: "Attach to a Lingon session",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loader.Load()
			if err != nil {
				return err
			}
			logPath := cfg.Client.LogFile
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
			logger = logger.With("component", "attach")
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
			var sessionID string
			if len(args) > 0 {
				sessionID = args[0]
			}

			if pick && sessionID != "" {
				return fmt.Errorf("cannot use --pick with an explicit session id")
			}
			if pick && shareToken != "" {
				return fmt.Errorf("cannot use --pick with a share token")
			}

			tokenValue := accessToken
			if shareToken == "" && !cmd.Flags().Changed("access-token") {
				resolved, err := resolveAccessToken(cmd.Context(), endpointValue, authPath)
				if err != nil {
					return err
				}
				tokenValue = resolved
			}
			if shareToken == "" && tokenValue == "" {
				return fmt.Errorf("access token is required")
			}

			if pick {
				sessions, err := lingon.ListSessions(cmd.Context(), endpointValue, tokenValue)
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
				sessions, err := lingon.ListSessions(cmd.Context(), endpointValue, tokenValue)
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

			return lingon.Attach(cmd.Context(), lingon.AttachOptions{
				Endpoint:       endpointValue,
				SessionID:      sessionID,
				AccessToken:    tokenValue,
				ShareToken:     shareToken,
				RequestControl: requestControl,
				Logger:         logger,
			})
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&endpoint, "endpoint", "e", lingon.DefaultClientEndpoint, "relay endpoint (https/wss base URL)")
	flags.StringVarP(&shareToken, "token", "t", "", "share token for anonymous attach")
	flags.StringVar(&accessToken, "access-token", "", "access token for authenticated attach")
	flags.BoolVarP(&requestControl, "request-control", "", false, "request controller lease on connect")
	flags.BoolVar(&pick, "pick", false, "interactively pick a session")
	flags.StringVar(&authFile, "auth-file", lingon.DefaultAuthPath(), "path to auth file")

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

		cfg, err := loader.Load()
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
			resolved, err := lingon.EnsureAccessToken(cmd.Context(), endpointValue, authPath)
			if err != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			tokenValue = resolved.AccessToken
		}
		if tokenValue == "" {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		sessions, err := lingon.ListSessions(cmd.Context(), endpointValue, tokenValue)
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
