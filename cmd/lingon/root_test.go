package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/testutil"
)

func TestRootCommandSuppressesUsageOnError(t *testing.T) {
	testutil.SetLingonConfigEnv(t)

	loader := lingon.NewLoader()
	cmd := NewRootCommand(loader)

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"attach", "--pick", "session1"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error")
	}
	if strings.Contains(out.String(), "Usage:") || strings.Contains(errOut.String(), "Usage:") {
		t.Fatalf("unexpected usage output: out=%q err=%q", out.String(), errOut.String())
	}
	if errOut.String() != "" {
		t.Fatalf("expected no stderr output, got %q", errOut.String())
	}
}

func TestRootDefaultAuthFileIgnoresXDGConfigHome(t *testing.T) {
	home := testutil.TempDir(t)
	xdg := testutil.TempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv(lingon.ConfigDirEnv, "")

	loader := lingon.NewLoader()
	cmd := NewRootCommand(loader)

	authFlag := cmd.Flags().Lookup("auth-file")
	if authFlag == nil {
		t.Fatalf("missing auth-file flag")
	}
	want := filepath.Join(home, lingon.DefaultConfigDirName, lingon.DefaultAuthFileName)
	if authFlag.DefValue != want {
		t.Fatalf("auth-file default = %q, want %q", authFlag.DefValue, want)
	}
	if strings.Contains(authFlag.DefValue, filepath.Join(".config", lingon.DefaultConfigDirName)) {
		t.Fatalf("auth-file default used XDG config path: %q", authFlag.DefValue)
	}
}

func TestRootConfigDirFlagRebasesDerivedDefaults(t *testing.T) {
	oldConfigDir, hadConfigDir := os.LookupEnv(lingon.ConfigDirEnv)
	t.Cleanup(func() {
		if hadConfigDir {
			_ = os.Setenv(lingon.ConfigDirEnv, oldConfigDir)
		} else {
			_ = os.Unsetenv(lingon.ConfigDirEnv)
		}
	})
	cfgDir := filepath.Join(testutil.TempDir(t), "cfg")

	loader := lingon.NewLoader()
	cmd := NewRootCommand(loader)
	if err := applyConfigRoot(cfgDir, loader, cmd); err != nil {
		t.Fatalf("applyConfigRoot: %v", err)
	}

	if got := loader.Viper().GetString("client.auth_file"); got != filepath.Join(cfgDir, "auth.json") {
		t.Fatalf("client.auth_file = %q, want config-root auth", got)
	}
	if got := loader.Viper().GetString("server.data_dir"); got != cfgDir {
		t.Fatalf("server.data_dir = %q, want %q", got, cfgDir)
	}
	if got := loader.Viper().GetString("server.users_file"); got != filepath.Join(cfgDir, "users.json") {
		t.Fatalf("server.users_file = %q, want config-root users", got)
	}
	if got := loader.Viper().GetString("server.tls.dir"); got != filepath.Join(cfgDir, "tls") {
		t.Fatalf("server.tls.dir = %q, want config-root tls", got)
	}
	if got := loader.Viper().GetString("server.tls.cache_dir"); got != filepath.Join(cfgDir, "tls", "cache") {
		t.Fatalf("server.tls.cache_dir = %q, want config-root tls cache", got)
	}

	if got := cmd.Flags().Lookup("auth-file").DefValue; got != filepath.Join(cfgDir, "auth.json") {
		t.Fatalf("auth-file default = %q, want config-root auth", got)
	}
	serveCmd, _, err := cmd.Find([]string{"serve"})
	if err != nil {
		t.Fatalf("find serve: %v", err)
	}
	for name, want := range map[string]string{
		"data-dir":      cfgDir,
		"users-file":    filepath.Join(cfgDir, "users.json"),
		"tls-dir":       filepath.Join(cfgDir, "tls"),
		"tls-cache-dir": filepath.Join(cfgDir, "tls", "cache"),
	} {
		flag := serveCmd.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("missing serve flag %s", name)
		}
		if flag.DefValue != want {
			t.Fatalf("serve --%s default = %q, want %q", name, flag.DefValue, want)
		}
	}

	for _, args := range [][]string{
		{"tls", "new"},
		{"tls", "new", "ca"},
		{"tls", "new", "server"},
		{"tls", "export"},
	} {
		tlsCmd, _, err := cmd.Find(args)
		if err != nil {
			t.Fatalf("find %v: %v", args, err)
		}
		flag := tlsCmd.Flag("dir")
		if flag == nil {
			t.Fatalf("missing %v --dir flag", args)
		}
		if want := filepath.Join(cfgDir, "tls"); flag.DefValue != want {
			t.Fatalf("%v --dir default = %q, want %q", args, flag.DefValue, want)
		}
		if want := filepath.Join(cfgDir, "tls"); flag.Value.String() != want {
			t.Fatalf("%v --dir value = %q, want %q", args, flag.Value.String(), want)
		}
	}
}

func TestRootOfflineFlag(t *testing.T) {
	loader := lingon.NewLoader()
	cmd := NewRootCommand(loader)

	if err := cmd.Flags().Parse([]string{"--offline"}); err != nil {
		t.Fatalf("parse --offline: %v", err)
	}
	offline, err := cmd.Flags().GetBool("offline")
	if err != nil {
		t.Fatalf("get offline flag: %v", err)
	}
	if !offline {
		t.Fatalf("expected --offline to set flag")
	}

	cmd = NewRootCommand(loader)
	if err := cmd.Flags().Parse([]string{"-o"}); err != nil {
		t.Fatalf("parse -o: %v", err)
	}
	offline, err = cmd.Flags().GetBool("offline")
	if err != nil {
		t.Fatalf("get offline flag: %v", err)
	}
	if !offline {
		t.Fatalf("expected -o to set flag")
	}
}

func TestRootHostnameOnlyFlag(t *testing.T) {
	loader := lingon.NewLoader()
	cmd := NewRootCommand(loader)

	if err := cmd.Flags().Parse([]string{"--hostname-only"}); err != nil {
		t.Fatalf("parse --hostname-only: %v", err)
	}
	hostnameOnly, err := cmd.Flags().GetBool("hostname-only")
	if err != nil {
		t.Fatalf("get hostname-only flag: %v", err)
	}
	if !hostnameOnly {
		t.Fatalf("expected --hostname-only to set flag")
	}
}
