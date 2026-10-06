//go:build !windows

package main

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/relay"
	"pkt.systems/lingon/internal/testutil"
)

func TestUserCredentialCLIUsesLiveStateOwner(t *testing.T) {
	for _, action := range []string{"chpasswd", "rotate-totp", "delete"} {
		t.Run(action, func(t *testing.T) {
			testutil.SetLingonConfigEnv(t)
			dir := testutil.TempDir(t)
			owner, err := relay.NewStateOwner(dir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = owner.Close() })
			store, err := owner.LoadStore()
			if err != nil {
				t.Fatal(err)
			}
			usersPath := filepath.Join(dir, "users.json")
			users, err := owner.LoadUsers(usersPath)
			if err != nil {
				t.Fatal(err)
			}
			users.Upsert(relay.User{Username: "alice", PasswordHash: "previous-password-hash", TOTPSecret: "previous-totp-secret"})
			if err := users.Save(usersPath); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			for _, username := range []string{"alice", "bob"} {
				store.CreateSession(relay.Session{ID: username + "-session", Username: username, Status: "active"})
			}
			alice, err := store.CreateShareToken("alice-session", relay.ShareScopeView, time.Hour, now)
			if err != nil {
				t.Fatal(err)
			}
			bob, err := store.CreateShareToken("bob-session", relay.ShareScopeView, time.Hour, now)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Save(dir); err != nil {
				t.Fatal(err)
			}
			// The owner has newer authority and session state than the last disk
			// snapshot. A CLI operation must apply to this owner, not save a copy.
			if err := store.RevokeShareToken(bob.Token, now); err != nil {
				t.Fatal(err)
			}
			fresh, err := store.CreateAccessToken("bob", time.Hour, now)
			if err != nil {
				t.Fatal(err)
			}
			store.CreateSession(relay.Session{ID: "bob-new-session", Username: "bob", Status: "active"})
			server := relay.NewHTTPServer(store, users, nil, nil, nil)
			server.DataDir, server.UsersFile = dir, usersPath
			if err := owner.ListenUsers(usersPath, server); err != nil {
				t.Fatal(err)
			}
			loader := lingon.NewLoader()
			loader.Viper().Set("server.data_dir", dir)
			cmd := NewUsersCommand(loader)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{action, "alice", "--users-file", usersPath})
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			grant, _ := store.GetShareToken(alice.Token)
			if !grant.IsExpired(now) {
				t.Error("CLI returned without invalidating Alice's live authority")
			}
			persisted, err := relay.LoadStore(dir)
			if err != nil {
				t.Fatal(err)
			}
			grant, _ = persisted.GetShareToken(bob.Token)
			if !grant.IsExpired(now) {
				t.Error("CLI restored Bob's revoked share in persisted state")
			}
			if _, err := persisted.ValidateAccessToken(fresh.Token, now); err != nil {
				t.Error("CLI discarded Bob's newly issued token")
			}
			if _, ok := persisted.GetSession("bob-new-session"); !ok {
				t.Error("CLI discarded the owner's new session metadata")
			}
		})
	}
}

func TestUserCLIOutputAndLiveUserStoreStayConsistent(t *testing.T) {
	for _, live := range []bool{false, true} {
		t.Run(map[bool]string{false: "offline", true: "live"}[live], func(t *testing.T) {
			testutil.SetLingonConfigEnv(t)
			dir := testutil.TempDir(t)
			usersPath := filepath.Join(dir, "users.json")
			var users *relay.UserStore
			if live {
				owner, err := relay.NewStateOwner(dir)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = owner.Close() })
				store, err := owner.LoadStore()
				if err != nil {
					t.Fatal(err)
				}
				users, err = owner.LoadUsers(usersPath)
				if err != nil {
					t.Fatal(err)
				}
				server := relay.NewHTTPServer(store, users, nil, nil, nil)
				server.DataDir, server.UsersFile = dir, usersPath
				if err := owner.ListenUsers(usersPath, server); err != nil {
					t.Fatal(err)
				}
			}
			for _, tc := range []struct {
				action string
				fields []string
			}{
				{"add", []string{"username: alice", "password: ", "totp_secret: ", "otpauth_url: ", "totp_qr:"}},
				{"chpasswd", []string{"password: "}},
				{"rotate-totp", []string{"username: alice", "totp_secret: ", "otpauth_url: ", "totp_qr:"}},
				{"delete", []string{"user deleted"}},
			} {
				loader := lingon.NewLoader()
				loader.Viper().Set("server.data_dir", dir)
				cmd := NewUsersCommand(loader)
				var output bytes.Buffer
				cmd.SetOut(&output)
				cmd.SetErr(io.Discard)
				cmd.SetArgs([]string{tc.action, "alice", "--users-file", usersPath})
				if err := cmd.Execute(); err != nil {
					t.Fatalf("%s: %v", tc.action, err)
				}
				for _, field := range tc.fields {
					if !strings.Contains(output.String(), field) {
						t.Errorf("%s output omitted required field %s", tc.action, field)
					}
				}
				persisted, err := relay.LoadUserStore(usersPath)
				if err != nil {
					t.Fatal(err)
				}
				_, onDisk := persisted.Get("alice")
				if onDisk != (tc.action != "delete") {
					t.Fatal("CLI success did not match persisted user state")
				}
				if live {
					current, inMemory := users.Get("alice")
					if inMemory != onDisk {
						t.Fatal("CLI success did not update live user store")
					}
					if onDisk {
						disk, _ := persisted.Get("alice")
						if current.PasswordHash != disk.PasswordHash || current.TOTPSecret != disk.TOTPSecret {
							t.Fatal("live and persisted credentials diverged")
						}
					}
				}
			}
		})
	}
}
