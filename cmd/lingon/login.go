package main

import (
	"bufio"
	"fmt"
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

	v := loader.Viper()
	v.SetDefault("client.endpoint", lingon.DefaultClientEndpoint)
	v.SetDefault("client.auth_file", lingon.DefaultAuthPath())
	v.SetDefault("client.log_file", lingon.DefaultLogPath())

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate and store tokens locally",
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			ctx := pslog.ContextWithLogger(cmd.Context(), logger.With("component", "login"))
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

			reader := bufio.NewReader(os.Stdin)
			fmt.Fprint(os.Stdout, "Username: ")
			username, err := reader.ReadString('\n')
			if err != nil {
				return err
			}
			username = strings.TrimSpace(username)
			if username == "" {
				return fmt.Errorf("username is required")
			}

			fmt.Fprint(os.Stdout, "Password: ")
			passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stdout)
			if err != nil {
				return err
			}

			fmt.Fprint(os.Stdout, "TOTP: ")
			totpBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stdout)
			if err != nil {
				return err
			}

			state, err := lingon.Login(ctx, lingon.LoginOptions{
				Endpoint: endpointValue,
				Username: username,
				Password: string(passwordBytes),
				TOTP:     string(totpBytes),
			})
			if err != nil {
				return err
			}
			if err := lingon.SaveAuth(authPath, state); err != nil {
				return err
			}
			pslog.Ctx(cmd.Context()).Info("login succeeded", "auth_file", authPath)
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&endpoint, "endpoint", "e", lingon.DefaultClientEndpoint, "relay endpoint (https base URL)")
	flags.StringVar(&authFile, "auth-file", lingon.DefaultAuthPath(), "path to auth file")

	return cmd
}
