package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mdp/qrterminal/v3"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/relay"
	"pkt.systems/prettyx"
)

// NewUsersCommand builds the users management command.
func NewUsersCommand(loader *lingon.Loader) *cobra.Command {
	var usersFile string

	v := loader.Viper()
	v.SetDefault("server.users_file", lingon.DefaultUsersPath())

	cmd := &cobra.Command{
		Use:   "users",
		Short: "Manage users",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.SetUsageTemplate(usersUsageTemplate)

	flags := cmd.PersistentFlags()
	flags.StringVar(&usersFile, "users-file", lingon.DefaultUsersPath(), "path to users file")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List users",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, _, err := loadUserStore(cmd, loader, usersFile)
			if err != nil {
				return err
			}
			users := store.List()
			resp := make([]userSummary, 0, len(users))
			for _, user := range users {
				resp = append(resp, userSummary{
					Username:  user.Username,
					CreatedAt: user.CreatedAt,
				})
			}
			data, err := json.Marshal(resp)
			if err != nil {
				return err
			}
			return prettyx.PrettyTo(cmd.OutOrStdout(), data, prettyx.DefaultOptions)
		},
	}

	var addPrompt bool
	addCmd := &cobra.Command{
		Use:   "add <username>",
		Short: "Add a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, path, err := loadUserStore(cmd, loader, usersFile)
			if err != nil {
				return err
			}
			username := strings.TrimSpace(args[0])
			if username == "" {
				return fmt.Errorf("username is required")
			}
			password := ""
			if addPrompt {
				value, err := promptPassword("Password: ")
				if err != nil {
					return err
				}
				password = value
			}
			resp, err := relay.CreateUser(store, username, password, time.Now().UTC())
			if err != nil {
				return formatUserError(err)
			}
			if err := store.Save(path); err != nil {
				return err
			}
			printUserCreate(cmd.OutOrStdout(), resp)
			return nil
		},
	}
	addCmd.Flags().BoolVar(&addPrompt, "prompt", false, "prompt for password")

	var chpasswdPrompt bool
	chpasswdCmd := &cobra.Command{
		Use:   "chpasswd <username>",
		Short: "Change a user's password",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, path, err := loadUserStore(cmd, loader, usersFile)
			if err != nil {
				return err
			}
			username := strings.TrimSpace(args[0])
			if username == "" {
				return fmt.Errorf("username is required")
			}
			password := ""
			if chpasswdPrompt {
				value, err := promptPassword("Password: ")
				if err != nil {
					return err
				}
				password = value
			}
			resp, err := relay.ChangeUserPassword(store, username, password)
			if err != nil {
				return formatUserError(err)
			}
			if err := store.Save(path); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "password: %s\n", resp.Password)
			return nil
		},
	}
	chpasswdCmd.Flags().BoolVar(&chpasswdPrompt, "prompt", false, "prompt for password")

	rotateCmd := &cobra.Command{
		Use:   "rotate-totp <username>",
		Short: "Rotate a user's TOTP secret",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, path, err := loadUserStore(cmd, loader, usersFile)
			if err != nil {
				return err
			}
			username := strings.TrimSpace(args[0])
			if username == "" {
				return fmt.Errorf("username is required")
			}
			resp, err := relay.RotateUserTOTP(store, username)
			if err != nil {
				return formatUserError(err)
			}
			if err := store.Save(path); err != nil {
				return err
			}
			printUserTOTP(cmd.OutOrStdout(), resp)
			return nil
		},
	}

	deleteCmd := &cobra.Command{
		Use:   "delete <username>",
		Short: "Delete a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, path, err := loadUserStore(cmd, loader, usersFile)
			if err != nil {
				return err
			}
			username := strings.TrimSpace(args[0])
			if username == "" {
				return fmt.Errorf("username is required")
			}
			user, err := relay.DeleteUser(store, username)
			if err != nil {
				return formatUserError(err)
			}
			if err := store.Save(path); err != nil {
				return err
			}
			cfg, err := loader.Load()
			if err != nil {
				return err
			}
			stateDir := strings.TrimSpace(cfg.Server.DataDir)
			if stateDir != "" {
				stateStore, err := relay.LoadStore(stateDir)
				if err != nil {
					return err
				}
				stateStore.RevokeTokensForUsername(user.Username)
				if err := stateStore.Save(stateDir); err != nil {
					return err
				}
			}
			fmt.Fprintln(cmd.OutOrStdout(), "user deleted")
			return nil
		},
	}

	cmd.AddCommand(listCmd)
	cmd.AddCommand(addCmd)
	cmd.AddCommand(chpasswdCmd)
	cmd.AddCommand(rotateCmd)
	cmd.AddCommand(deleteCmd)

	return cmd
}

type userSummary struct {
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

func loadUserStore(cmd *cobra.Command, loader *lingon.Loader, usersFileFlag string) (*relay.UserStore, string, error) {
	cfg, err := loader.Load()
	if err != nil {
		return nil, "", err
	}
	usersFile := usersFileFlag
	if !cmd.Flags().Changed("users-file") {
		usersFile = cfg.Server.UsersFile
	}
	usersFile = strings.TrimSpace(usersFile)
	if usersFile == "" {
		return nil, "", fmt.Errorf("users file is required")
	}
	store, err := relay.LoadUserStore(usersFile)
	if err != nil {
		return nil, "", err
	}
	return store, usersFile, nil
}

func formatUserError(err error) error {
	switch {
	case errors.Is(err, relay.ErrUserExists):
		return fmt.Errorf("user already exists")
	case errors.Is(err, relay.ErrUserNotFound):
		return fmt.Errorf("user not found")
	case errors.Is(err, relay.ErrUsernameRequired):
		return fmt.Errorf("username is required")
	default:
		return err
	}
}

func promptPassword(label string) (string, error) {
	fmt.Fprint(os.Stdout, label)
	passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stdout)
	if err != nil {
		return "", err
	}
	return string(passwordBytes), nil
}

func printUserCreate(w io.Writer, resp relay.UserCreateResult) {
	_, _ = fmt.Fprintf(w, "username: %s\n", resp.User.Username)
	_, _ = fmt.Fprintf(w, "password: %s\n", resp.Password)
	if resp.TOTPSecret != "" {
		_, _ = fmt.Fprintf(w, "totp_secret: %s\n", resp.TOTPSecret)
	}
	if resp.TOTPURL != "" {
		_, _ = fmt.Fprintf(w, "otpauth_url: %s\n", resp.TOTPURL)
		printQR(w, resp.TOTPURL)
	}
}

func printUserTOTP(w io.Writer, resp relay.UserTOTPResult) {
	_, _ = fmt.Fprintf(w, "username: %s\n", resp.User.Username)
	if resp.TOTPSecret != "" {
		_, _ = fmt.Fprintf(w, "totp_secret: %s\n", resp.TOTPSecret)
	}
	if resp.TOTPURL != "" {
		_, _ = fmt.Fprintf(w, "otpauth_url: %s\n", resp.TOTPURL)
		printQR(w, resp.TOTPURL)
	}
}

func printQR(w io.Writer, url string) {
	if strings.TrimSpace(url) == "" {
		return
	}
	_, _ = fmt.Fprintln(w, "totp_qr:")
	qrterminal.GenerateHalfBlock(url, qrterminal.L, w)
}

const usersUsageTemplate = `Usage:
  {{.CommandPath}} [command] [flags]

{{if .HasAvailableSubCommands}}Available Commands:
{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}{{rpad .Name .NamePadding }} {{.Short}}
{{end}}{{end}}{{end}}

{{if .HasAvailableLocalFlags}}Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}

{{if .HasAvailableInheritedFlags}}Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}

{{if .HasHelpSubCommands}}Additional help topics:
{{range .Commands}}{{if .IsHelpCommand}}{{rpad .CommandPath .CommandPathPadding}} {{.Short}}
{{end}}{{end}}{{end}}

{{if .HasAvailableSubCommands}}Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`
