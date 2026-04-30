package main

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"pkt.systems/lingon"
)

func applyConfigRoot(configDir string, loader *lingon.Loader, root *cobra.Command) error {
	dir := strings.TrimSpace(configDir)
	if dir != "" {
		if err := os.Setenv(lingon.ConfigDirEnv, dir); err != nil {
			return err
		}
	}
	if loader != nil {
		setConfigRootDefaults(loader.Viper())
	}
	refreshConfigRootFlagDefaults(root)
	return nil
}

func setConfigRootDefaults(v *viper.Viper) {
	if v == nil {
		return
	}
	v.SetDefault("client.auth_file", lingon.DefaultAuthPath())
	v.SetDefault("client.log_file", lingon.DefaultLogPath())
	v.SetDefault("server.data_dir", lingon.DefaultConfigDir())
	v.SetDefault("server.users_file", lingon.DefaultUsersPath())
	v.SetDefault("server.tls.dir", lingon.DefaultTLSDir())
	v.SetDefault("server.tls.cache_dir", lingon.DefaultTLSCacheDir())
}

func refreshConfigRootFlagDefaults(cmd *cobra.Command) {
	if cmd == nil {
		return
	}
	rewriteDefaultFlag(cmd, "auth-file", lingon.DefaultAuthPath())
	rewriteDefaultFlag(cmd, "log-file", lingon.DefaultLogPath())
	rewriteDefaultFlag(cmd, "data-dir", lingon.DefaultConfigDir())
	rewriteDefaultFlag(cmd, "users-file", lingon.DefaultUsersPath())
	rewriteDefaultFlag(cmd, "tls-dir", lingon.DefaultTLSDir())
	rewriteDefaultFlag(cmd, "tls-cache-dir", lingon.DefaultTLSCacheDir())
	rewriteDefaultFlag(cmd, "dir", lingon.DefaultTLSDir())
	for _, child := range cmd.Commands() {
		refreshConfigRootFlagDefaults(child)
	}
}

func rewriteDefaultFlag(cmd *cobra.Command, name, value string) {
	flag := cmd.Flags().Lookup(name)
	if flag == nil {
		flag = cmd.PersistentFlags().Lookup(name)
	}
	if flag == nil || flag.Changed {
		return
	}
	flag.DefValue = value
	_ = flag.Value.Set(value)
}
