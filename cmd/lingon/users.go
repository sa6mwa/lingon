package main

import (
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
)

// NewUsersCommand builds the users management command.
func NewUsersCommand(loader *lingon.Loader) *cobra.Command {
	var usersFile string

	v := loader.Viper()
	v.SetDefault("server.users_file", lingon.DefaultUsersPath())

	cmd := &cobra.Command{
		Use:   "users [flags] [command]",
		Short: "Manage users",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	flags := cmd.PersistentFlags()
	flags.StringVar(&usersFile, "users-file", lingon.DefaultUsersPath(), "path to users file")

	listCmd := &cobra.Command{
		Use:   "list [flags]",
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
			return printJSON(cmd.OutOrStdout(), resp)
		},
	}

	var addPrompt bool
	addCmd := &cobra.Command{
		Use:   "add [flags] <username>",
		Short: "Add a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(args[0]) == "" {
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
			result, err := mutateUser(cmd, loader, usersFile, relay.UserMutationRequest{Action: "add", Username: args[0], Password: password})
			if err != nil {
				return formatUserError(err)
			}
			printUserCreate(cmd.OutOrStdout(), relay.UserCreateResult{User: relay.User{Username: result.Username, CreatedAt: result.CreatedAt}, Password: result.Password, TOTPSecret: result.TOTPSecret, TOTPURL: result.TOTPURL})
			return nil
		},
	}
	addCmd.Flags().BoolVar(&addPrompt, "prompt", false, "prompt for password")

	var chpasswdPrompt bool
	chpasswdCmd := &cobra.Command{
		Use:   "chpasswd [flags] <username>",
		Short: "Change a user's password",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(args[0]) == "" {
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
			result, err := mutateUser(cmd, loader, usersFile, relay.UserMutationRequest{Action: "chpasswd", Username: args[0], Password: password})
			if err != nil {
				return formatUserError(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "password: %s\n", result.Password)
			return nil
		},
	}
	chpasswdCmd.Flags().BoolVar(&chpasswdPrompt, "prompt", false, "prompt for password")

	rotateCmd := &cobra.Command{
		Use:   "rotate-totp [flags] <username>",
		Short: "Rotate a user's TOTP secret",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := mutateUser(cmd, loader, usersFile, relay.UserMutationRequest{Action: "rotate-totp", Username: args[0]})
			if err != nil {
				return formatUserError(err)
			}
			printUserTOTP(cmd.OutOrStdout(), relay.UserTOTPResult{User: relay.User{Username: result.Username, CreatedAt: result.CreatedAt}, TOTPSecret: result.TOTPSecret, TOTPURL: result.TOTPURL})
			return nil
		},
	}
	deleteCmd := &cobra.Command{
		Use:     "delete [flags] <username>",
		Aliases: []string{"rm"},
		Short:   "Delete a user",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := mutateUser(cmd, loader, usersFile, relay.UserMutationRequest{Action: "delete", Username: args[0]})
			if err != nil {
				return formatUserError(err)
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

func mutateUser(cmd *cobra.Command, loader *lingon.Loader, usersFileFlag string, req relay.UserMutationRequest) (relay.UserMutationResult, error) {
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		return relay.UserMutationResult{}, relay.ErrUsernameRequired
	}
	cfg, err := loader.Load()
	if err != nil {
		return relay.UserMutationResult{}, err
	}
	usersFile := cfg.Server.UsersFile
	if cmd.Flags().Changed("users-file") {
		usersFile = usersFileFlag
	}
	if strings.TrimSpace(usersFile) == "" {
		return relay.UserMutationResult{}, fmt.Errorf("users file is required")
	}
	admin, err := relay.NewUserAdmin(relay.UserAdminOptions{DataDir: cfg.Server.DataDir, UsersFile: usersFile})
	if err != nil {
		return relay.UserMutationResult{}, err
	}
	defer func() { _ = admin.Close() }()
	return admin.MutateUser(cmd.Context(), req)
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
