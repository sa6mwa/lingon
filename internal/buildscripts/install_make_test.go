package buildscripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallCopiesExistingBinaryWithoutBuildOrVerification(t *testing.T) {
	for _, present := range []bool{true, false} {
		t.Run(map[bool]string{true: "existing binary", false: "missing binary"}[present], func(t *testing.T) {
			work := lifecycleWork(t)
			makefile, err := os.ReadFile(filepath.Join(repoRoot(t), "Makefile"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(work, "Makefile"), makefile, 0o600); err != nil {
				t.Fatal(err)
			}
			forbidden := filepath.Join(work, "forbidden-tool")
			if err := os.WriteFile(forbidden, []byte("#!/bin/sh\necho unexpected-build-or-verification >&2\nexit 99\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			payload := []byte("#!/bin/sh\nprintf 'installed-fixture\\n'\n")
			if present {
				if err := os.MkdirAll(filepath.Join(work, "bin"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(work, "bin/lingon"), payload, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			prefix := filepath.Join(work, "install prefix")
			cmd := exec.Command("make", "install", "GO="+forbidden, "VERIFY="+forbidden, "PREFIX="+prefix)
			cmd.Dir = work
			for _, item := range os.Environ() {
				if !strings.HasPrefix(item, "DBUS_SESSION_BUS_ADDRESS=") && !strings.HasPrefix(item, "XDG_RUNTIME_DIR=") {
					cmd.Env = append(cmd.Env, item)
				}
			}
			out, err := cmd.CombinedOutput()
			if strings.Contains(string(out), "unexpected-build-or-verification") {
				t.Fatalf("install invoked a build or verification tool: %s", out)
			}
			if !present {
				if err == nil || !strings.Contains(string(out), "run make build first") {
					t.Fatalf("missing binary must give an actionable error: %v\n%s", err, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("install without Go/user bus: %v\n%s", err, out)
			}
			installed := filepath.Join(prefix, "bin/lingon")
			data, err := os.ReadFile(installed)
			if err != nil || string(data) != string(payload) {
				t.Fatalf("installed payload changed: %q %v", data, err)
			}
			target, err := os.Readlink(filepath.Join(prefix, "bin/lingonx"))
			if err != nil || target != "lingon" {
				t.Fatalf("lingonx target = %q: %v", target, err)
			}
			if out, err := exec.Command(filepath.Join(prefix, "bin/lingonx")).CombinedOutput(); err != nil || string(out) != "installed-fixture\n" {
				t.Fatalf("installed alias cannot execute: %q %v", out, err)
			}
		})
	}
}
