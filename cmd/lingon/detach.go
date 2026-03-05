package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
)

// NewDetachCommand builds the local headless detach command.
func NewDetachCommand(loader *lingon.Loader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "detach [session-id ...]",
		Short: "Force-stop a local headless PTY session",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := loader.Load(); err != nil {
				return err
			}
			cfgDir := configDirForLoader(loader)
			if len(args) == 0 {
				return fmt.Errorf("at least one session id is required (or use 'all')")
			}
			targets := make([]string, 0, len(args))
			for _, arg := range args {
				target := strings.TrimSpace(arg)
				if target == "" {
					continue
				}
				targets = append(targets, target)
			}
			if len(targets) == 0 {
				return fmt.Errorf("at least one session id is required (or use 'all')")
			}
			if strings.EqualFold(targets[0], "all") {
				if len(targets) != 1 {
					return fmt.Errorf("'all' cannot be combined with session ids")
				}
				return detachAllLocalHeadlessSessions(cfgDir)
			}
			for _, target := range targets[1:] {
				if strings.EqualFold(target, "all") {
					return fmt.Errorf("'all' cannot be combined with session ids")
				}
			}
			seen := make(map[string]struct{}, len(targets))
			var errs []error
			for _, target := range targets {
				if _, ok := seen[target]; ok {
					continue
				}
				seen[target] = struct{}{}
				if err := detachLocalHeadlessSession(cfgDir, target); err != nil {
					errs = append(errs, err)
				}
			}
			return errors.Join(errs...)
		},
	}
	cmd.ValidArgsFunction = detachSessionCompletion(loader)
	return cmd
}

func detachSessionCompletion(loader *lingon.Loader) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		for _, arg := range args {
			if strings.EqualFold(strings.TrimSpace(arg), "all") {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
		}
		if _, err := loader.Load(); err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		sessions, err := listLocalHeadlessSessions(configDirForLoader(loader))
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		taken := make(map[string]struct{}, len(args))
		for _, arg := range args {
			id := strings.TrimSpace(arg)
			if id != "" {
				taken[id] = struct{}{}
			}
		}
		suggestions := make([]string, 0, len(sessions)+1)
		if len(args) == 0 && strings.HasPrefix("all", toComplete) {
			suggestions = append(suggestions, "all")
		}
		for _, session := range sessions {
			if _, ok := taken[session.ID]; ok {
				continue
			}
			if strings.HasPrefix(session.ID, toComplete) {
				suggestions = append(suggestions, session.ID)
			}
		}
		sort.Strings(suggestions)
		return suggestions, cobra.ShellCompDirectiveNoFileComp
	}
}
