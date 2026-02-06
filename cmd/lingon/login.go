package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"pkt.systems/lingon"
	"pkt.systems/pslog"
)

// NewLoginCommand builds the login command.
func NewLoginCommand(loader *lingon.Loader) *cobra.Command {
	var endpoint string
	var authFile string
	var logFile string
	var nonInteractive bool

	v := loader.Viper()
	v.SetDefault("client.endpoint", lingon.DefaultClientEndpoint)
	v.SetDefault("client.auth_file", lingon.DefaultAuthPath())
	v.SetDefault("client.log_file", lingon.DefaultLogPath())
	v.SetDefault("client.login_non_interactive", false)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate and store tokens locally",
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
			ctx := pslog.ContextWithLogger(cmd.Context(), logger)
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

			nonInteractiveValue := nonInteractive
			if !cmd.Flags().Changed("non-interactive") {
				nonInteractiveValue = cfg.Client.LoginNonInteractive
			}
			stdinIsTerminal := term.IsTerminal(int(os.Stdin.Fd()))
			input, useEnv, err := resolveLoginInput(stdinIsTerminal, nonInteractiveValue, os.Getenv)
			if err != nil {
				return err
			}
			if !useEnv {
				reader := bufio.NewReader(os.Stdin)
				if err := writePrompt(os.Stdout, "Username: "); err != nil {
					return err
				}
				username, err := readLine(reader)
				if err != nil {
					return err
				}
				input.Username = strings.TrimSpace(username)
				if input.Username == "" {
					return fmt.Errorf("username is required")
				}

				password, err := promptPassword("Password: ")
				if err != nil {
					return err
				}
				input.Password = password
				if input.Password == "" {
					return fmt.Errorf("password is required")
				}

				if err := writePrompt(os.Stdout, "TOTP: "); err != nil {
					return err
				}
				totp, err := readLine(reader)
				if err != nil {
					return err
				}
				input.TOTP = strings.TrimSpace(totp)
				if input.TOTP == "" {
					return fmt.Errorf("totp is required")
				}
			}

			state, err := lingon.Login(ctx, lingon.LoginOptions{
				Endpoint: endpointValue,
				Username: input.Username,
				Password: input.Password,
				TOTP:     input.TOTP,
				TLSDir:   tlsDir,
				Insecure: insecure,
			})
			if err != nil {
				return err
			}
			if err := lingon.SaveAuth(authPath, state); err != nil {
				return err
			}
			pslog.Ctx(ctx).Info("cli.auth.login.done", "auth_file", authPath)
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&endpoint, "endpoint", "e", lingon.DefaultClientEndpoint, "relay endpoint (https/wss base URL; assumes https:// if omitted)")
	flags.StringVar(&authFile, "auth-file", lingon.DefaultAuthPath(), "path to auth file")
	flags.StringVar(&logFile, "log-file", "", "path to client log file (disabled if empty)")
	flags.BoolVar(&nonInteractive, "non-interactive", false, "use environment variables for login")
	registerEndpointFlagCompletion(cmd, loader)

	return cmd
}

type loginInput struct {
	Username string
	Password string
	TOTP     string
}

func resolveLoginInput(stdinIsTerminal bool, nonInteractive bool, getenv func(string) string) (loginInput, bool, error) {
	if !stdinIsTerminal && !nonInteractive {
		return loginInput{}, false, fmt.Errorf("stdin is not a terminal; use --non-interactive with LINGON_USERNAME, LINGON_PASSWORD, and LINGON_TOTP or run in a terminal")
	}
	if !nonInteractive {
		return loginInput{}, false, nil
	}
	input := loginInput{
		Username: strings.TrimSpace(getenv("LINGON_USERNAME")),
		Password: getenv("LINGON_PASSWORD"),
		TOTP:     getenv("LINGON_TOTP"),
	}
	if input.Username == "" || input.Password == "" || input.TOTP == "" {
		return loginInput{}, true, fmt.Errorf("non-interactive login requires LINGON_USERNAME, LINGON_PASSWORD, and LINGON_TOTP")
	}
	return input, true, nil
}

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

type promptFlusher interface {
	Flush() error
}

func writePrompt(out io.Writer, msg string) error {
	if _, err := fmt.Fprint(out, msg); err != nil {
		return err
	}
	if flusher, ok := out.(promptFlusher); ok {
		_ = flusher.Flush()
	}
	return nil
}
