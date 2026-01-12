package main

import (
	"github.com/spf13/cobra"

	"pkt.systems/lingon"
	"pkt.systems/pslog"
)

// NewServeCommand builds the relay/server command.
func NewServeCommand(loader *lingon.Loader) *cobra.Command {
	v := loader.Viper()
	v.SetDefault("server.listen", lingon.DefaultListenAddr)
	v.SetDefault("server.base", lingon.DefaultBasePath)
	v.SetDefault("server.data_dir", lingon.DefaultConfigDir())
	v.SetDefault("server.users_file", lingon.DefaultUsersPath())
	v.SetDefault("server.tls.mode", lingon.DefaultTLSMode)
	v.SetDefault("server.tls.dir", lingon.DefaultTLSDir())
	v.SetDefault("server.tls.cache_dir", lingon.DefaultTLSCacheDir())

	var bindErr error

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the Lingon relay server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if bindErr != nil {
				return bindErr
			}
			cfg, err := loader.Load()
			if err != nil {
				return err
			}

			logger := pslog.Ctx(cmd.Context()).With("component", "serve")
			return lingon.Serve(cmd.Context(), lingon.ServeOptions{
				Config: cfg,
				Logger: logger,
			})
		},
	}

	flags := cmd.Flags()
	flags.String("listen", lingon.DefaultListenAddr, "listen address for HTTPS server")
	flags.String("data-dir", lingon.DefaultConfigDir(), "path to data directory")
	flags.String("users-file", lingon.DefaultUsersPath(), "path to users file")
	flags.String("base", lingon.DefaultBasePath, "base path prefix for all HTTP routes")
	flags.String("tls-mode", lingon.DefaultTLSMode, "tls mode: auto, bundle, or acme")
	flags.StringArray("tls-bundle", nil, "path to PEM bundle file (repeatable)")
	flags.String("tls-dir", lingon.DefaultTLSDir(), "tls directory")
	flags.String("tls-cache-dir", lingon.DefaultTLSCacheDir(), "tls cache directory for acme")
	flags.String("tls-hostname", "", "hostname for acme or server cert")

	bind := func(key, name string) {
		if bindErr != nil {
			return
		}
		if err := v.BindPFlag(key, flags.Lookup(name)); err != nil {
			bindErr = err
		}
	}

	bind("server.listen", "listen")
	bind("server.data_dir", "data-dir")
	bind("server.users_file", "users-file")
	bind("server.base", "base")
	bind("server.tls.mode", "tls-mode")
	bind("server.tls.bundle", "tls-bundle")
	bind("server.tls.dir", "tls-dir")
	bind("server.tls.cache_dir", "tls-cache-dir")
	bind("server.tls.hostname", "tls-hostname")

	return cmd
}
