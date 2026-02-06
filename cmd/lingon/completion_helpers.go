package main

import (
	"errors"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/authstore"
)

func registerEndpointFlagCompletion(cmd *cobra.Command, loader *lingon.Loader) {
	if cmd == nil {
		return
	}
	_ = cmd.RegisterFlagCompletionFunc("endpoint", endpointFlagCompletion(loader))
}

func endpointFlagCompletion(loader *lingon.Loader) cobra.CompletionFunc {
	return func(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		authPath := completionAuthPath(cmd, loader)
		if strings.TrimSpace(authPath) == "" {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		endpoints, err := authstore.Endpoints(authPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		out := make([]string, 0, len(endpoints))
		for _, endpoint := range endpoints {
			if strings.HasPrefix(endpoint, toComplete) {
				out = append(out, endpoint)
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

func completionAuthPath(cmd *cobra.Command, loader *lingon.Loader) string {
	if cmd != nil {
		if cmd.Flags().Lookup("auth-file") != nil && cmd.Flags().Changed("auth-file") {
			if authPath, err := cmd.Flags().GetString("auth-file"); err == nil && strings.TrimSpace(authPath) != "" {
				return authPath
			}
		}
	}
	if loader != nil {
		cfg, err := loader.Load()
		if err == nil && strings.TrimSpace(cfg.Client.AuthFile) != "" {
			return cfg.Client.AuthFile
		}
	}
	return lingon.DefaultAuthPath()
}
