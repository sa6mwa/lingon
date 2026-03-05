package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/authstore"
	"pkt.systems/lingon/internal/testutil"
)

func TestEndpointFlagCompletionFromAuthFile(t *testing.T) {
	dir := testutil.TempDir(t)
	path := filepath.Join(dir, "auth.json")
	now := time.Now().UTC()
	if err := authstore.Save(path, authstore.State{
		Endpoint:         "https://beta.example.com/v1",
		AccessToken:      "a1",
		AccessExpiresAt:  now.Add(10 * time.Minute),
		RefreshToken:     "r1",
		RefreshExpiresAt: now.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("save beta endpoint: %v", err)
	}
	if err := authstore.Save(path, authstore.State{
		Endpoint:         "https://alpha.example.com/v1/",
		AccessToken:      "a2",
		AccessExpiresAt:  now.Add(10 * time.Minute),
		RefreshToken:     "r2",
		RefreshExpiresAt: now.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("save alpha endpoint: %v", err)
	}

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("endpoint", "", "")
	cmd.Flags().String("auth-file", lingon.DefaultAuthPath(), "")
	if err := cmd.Flags().Set("auth-file", path); err != nil {
		t.Fatalf("set auth-file: %v", err)
	}
	registerEndpointFlagCompletion(cmd, lingon.NewLoader())
	fn, ok := cmd.GetFlagCompletionFunc("endpoint")
	if !ok {
		t.Fatalf("expected endpoint completion function")
	}

	all, directive := fn(cmd, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v, want %v", directive, cobra.ShellCompDirectiveNoFileComp)
	}
	if len(all) != 2 {
		t.Fatalf("len(all) = %d, want 2 (%v)", len(all), all)
	}
	if all[0] != "https://alpha.example.com/v1" || all[1] != "https://beta.example.com/v1" {
		t.Fatalf("all endpoints = %v, want sorted normalized endpoints", all)
	}

	filtered, _ := fn(cmd, nil, "https://beta")
	if len(filtered) != 1 || filtered[0] != "https://beta.example.com/v1" {
		t.Fatalf("filtered endpoints = %v, want [https://beta.example.com/v1]", filtered)
	}
}

func TestEndpointCompletionsRegisteredOnAllEndpointCommands(t *testing.T) {
	loader := lingon.NewLoader()

	mustHaveEndpointCompletion := func(cmd *cobra.Command, name string) {
		t.Helper()
		if _, ok := cmd.GetFlagCompletionFunc("endpoint"); !ok {
			t.Fatalf("%s missing endpoint completion function", name)
		}
	}

	root := NewRootCommand(loader)
	mustHaveEndpointCompletion(root, "root")
	mustHaveEndpointCompletion(NewAttachCommand(loader), "attach")
	mustHaveEndpointCompletion(NewSendCommand(loader), "send")
	mustHaveEndpointCompletion(NewWallCommand(loader), "wall")
	mustHaveEndpointCompletion(NewLoginCommand(loader), "login")
	mustHaveEndpointCompletion(NewLogoutCommand(loader), "logout")
	mustHaveEndpointCompletion(NewSessionsCommand(loader), "sessions")

	share := NewShareCommand(loader)
	mustHaveEndpointCompletion(findSubcommandByName(t, share, "create"), "share create")
	mustHaveEndpointCompletion(findSubcommandByName(t, share, "list"), "share list")
	mustHaveEndpointCompletion(findSubcommandByName(t, share, "revoke"), "share revoke")
}

func TestSessionIDCompletionRegisteredOnRequestedCommands(t *testing.T) {
	loader := lingon.NewLoader()
	send := NewSendCommand(loader)
	if send.ValidArgsFunction == nil {
		t.Fatalf("send command missing ValidArgsFunction")
	}
	detach := NewDetachCommand(loader)
	if detach.ValidArgsFunction == nil {
		t.Fatalf("detach command missing ValidArgsFunction")
	}
	share := NewShareCommand(loader)
	create := findSubcommandByName(t, share, "create")
	if create.ValidArgsFunction == nil {
		t.Fatalf("share create command missing ValidArgsFunction")
	}
	revoke := findSubcommandByName(t, share, "revoke")
	if revoke.ValidArgsFunction == nil {
		t.Fatalf("share revoke command missing ValidArgsFunction")
	}
}

func findSubcommandByName(t *testing.T, root *cobra.Command, name string) *cobra.Command {
	t.Helper()
	for _, sub := range root.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	t.Fatalf("subcommand %q not found", name)
	return nil
}
