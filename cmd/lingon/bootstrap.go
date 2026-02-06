package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
	"pkt.systems/pslog"
)

// NewBootstrapCommand builds the bootstrap command.
func NewBootstrapCommand() *cobra.Command {
	var force bool
	var regenerate bool
	var setValues []string

	cmd := &cobra.Command{
		Use:   "bootstrap [config-path]",
		Short: "Initialize Lingon config and TLS assets",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, err := cmd.Flags().GetString("config")
			if err != nil {
				return err
			}
			if len(args) > 0 {
				if strings.TrimSpace(configPath) != "" {
					return fmt.Errorf("config path provided both as argument and --config")
				}
				configPath = args[0]
			}
			if strings.TrimSpace(configPath) == "" {
				configPath = lingon.DefaultConfigPath()
			}
			if stat, err := os.Stat(configPath); err == nil && stat.IsDir() {
				return fmt.Errorf("config path %q is a directory", configPath)
			} else if err != nil && !os.IsNotExist(err) {
				return err
			}

			overrides, err := parseSetValues(setValues)
			if err != nil {
				return err
			}

			cfg := lingon.BootstrapConfig()
			if len(overrides) > 0 {
				cfg, err = lingon.ApplyConfigOverrides(cfg, overrides)
				if err != nil {
					return err
				}
			}

			logger := pslog.Ctx(cmd.Context())
			_, err = lingon.BootstrapWithOptions(cmd.Context(), lingon.BootstrapOptions{
				ConfigPath:             configPath,
				Config:                 cfg,
				Force:                  force,
				RegenerateCertificates: regenerate,
				Logger:                 logger,
			})
			return err
		},
	}

	flags := cmd.Flags()
	flags.BoolVarP(&force, "force", "f", false, "overwrite config if it already exists")
	flags.BoolVar(&regenerate, "regenerate-certificates", false, "regenerate TLS certificates even if they exist")
	flags.StringArrayVarP(&setValues, "set", "s", nil, "override config values (repeatable, key=value)")

	return cmd
}

func parseSetValues(values []string) (map[string]any, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]any, len(values))
	for _, raw := range values {
		key, value, ok := strings.Cut(raw, "=")
		if !ok {
			return nil, fmt.Errorf("invalid --set %q: expected key=value", raw)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("invalid --set %q: empty key", raw)
		}
		parsed, err := parseScalar(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("invalid --set %q: %w", raw, err)
		}
		out[key] = parsed
	}
	return out, nil
}

func parseScalar(value string) (any, error) {
	if value == "" {
		return "", nil
	}
	if quoted, ok := unquoteValue(value); ok {
		return quoted, nil
	}
	switch strings.ToLower(value) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	if i, err := strconv.Atoi(value); err == nil {
		return i, nil
	}
	if hasLetters(value) {
		if d, err := time.ParseDuration(value); err == nil {
			return d, nil
		}
	}
	return value, nil
}

func unquoteValue(value string) (string, bool) {
	if len(value) < 2 {
		return "", false
	}
	if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
		if value[0] == '"' {
			unquoted, err := strconv.Unquote(value)
			if err != nil {
				return "", false
			}
			return unquoted, true
		}
		return value[1 : len(value)-1], true
	}
	return "", false
}

func hasLetters(value string) bool {
	for _, r := range value {
		if r >= 'A' && r <= 'Z' {
			return true
		}
		if r >= 'a' && r <= 'z' {
			return true
		}
	}
	return false
}
