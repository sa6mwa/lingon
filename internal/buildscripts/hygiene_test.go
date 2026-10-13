package buildscripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryHygiene(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, "scripts", "check-hygiene.sh")
	for _, tc := range []struct {
		name, output                      string
		force, removeIgnore, corruptIndex bool
	}{
		{name: "ignored evidence", output: "build/review/result.json"},
		{name: "ignored ledger", output: "build/verification/BUG_TRACKER.md"},
		{name: "ignored release", output: "dist/v0.0.0/archive.tar.gz"},
		{name: "root log", output: "review-verification.log"},
		{name: "forced log", output: "nested/report with spaces.log", force: true},
		{name: "forced review", output: "review-verification.md", force: true},
		{name: "forced ledger", output: "BUG_TRACKER.md", force: true},
		{name: "forced build output", output: "build/review/result.json", force: true},
		{name: "forced release", output: "dist/v0.0.0/archive.tar.gz", force: true},
		{name: "forced Android evidence", output: "android/test-artifacts/result.xml", force: true},
		{name: "removed ignore", removeIgnore: true},
		{name: "unreadable index", corruptIndex: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			work := lifecycleWork(t)
			command := func(name string, args ...string) []byte {
				cmd := exec.Command(name, args...)
				cmd.Dir = work
				cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("%s %v: %v\n%s", name, args, err, out)
				}
				return out
			}
			command("git", "init", "--quiet")
			for _, file := range []string{".gitignore", "android/.gitignore"} {
				data, err := os.ReadFile(filepath.Join(root, file))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(filepath.Join(work, file)), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(work, file), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			command("git", "add", ".gitignore", "android/.gitignore")
			if tc.removeIgnore {
				if err := os.WriteFile(filepath.Join(work, ".gitignore"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.output != "" {
				path := filepath.Join(work, tc.output)
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("generated evidence\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if tc.force {
					command("git", "add", "--force", "--", tc.output)
				}
			}
			if tc.corruptIndex {
				if err := os.WriteFile(filepath.Join(work, ".git", "index"), []byte("invalid index"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("bash", script)
			cmd.Dir = work
			cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
			out, err := cmd.CombinedOutput()
			wantFailure := tc.force || tc.removeIgnore || tc.corruptIndex || tc.name == "root log"
			if (err != nil) != wantFailure {
				t.Fatalf("hygiene failure=%v, want %v:\n%s", err, wantFailure, out)
			}
			if wantFailure && !strings.Contains(string(out), "Repository hygiene:") {
				t.Fatalf("missing actionable hygiene error: %s", out)
			}
		})
	}
}

func TestRepositoryHygieneMakeTarget(t *testing.T) {
	cmd := exec.Command("make", "-n", "hygiene")
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "scripts/verify-contained.sh bash scripts/check-hygiene.sh") {
		t.Fatalf("missing contained hygiene target: %v\n%s", err, out)
	}
}
