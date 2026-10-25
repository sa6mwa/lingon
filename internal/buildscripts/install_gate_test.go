package buildscripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInstallCheckIgnoresAmbientInstallDirectory(t *testing.T) {
	work := lifecycleWork(t)
	for _, name := range []string{"Makefile", "scripts/check-install.sh"} {
		data, err := os.ReadFile(filepath.Join(repoRoot(t), name))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(work, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	goTool := filepath.Join(work, "go-fixture")
	goScript := "#!/bin/sh\nset -eu\nwhile [ $# -gt 0 ]; do\nif [ \"$1\" = -o ]; then\nshift\nprintf '#!/bin/sh\\nprintf fixture-version\\\\n\\n' > \"$1\"\nchmod 755 \"$1\"\nexit 0\nfi\nshift\ndone\nexit 1\n"
	if err := os.WriteFile(goTool, []byte(goScript), 0o700); err != nil {
		t.Fatal(err)
	}
	bindir := filepath.Join(work, "existing-install")
	if err := os.MkdirAll(bindir, 0o700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(bindir, "lingon")
	if err := os.WriteFile(binary, []byte("keep installed binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(bindir, "lingonx")
	if err := os.Symlink("keep-alias", alias); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "scripts/check-install.sh")
	cmd.Dir = work
	cmd.Env = append(os.Environ(), "GO="+goTool, "BINDIR="+bindir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("isolated install check failed: %v\n%s", err, out)
	}
	if data, err := os.ReadFile(binary); err != nil || string(data) != "keep installed binary" {
		t.Errorf("check changed the existing binary: %q %v", data, err)
	}
	if target, err := os.Readlink(alias); err != nil || target != "keep-alias" {
		t.Errorf("check replaced the existing alias: %q %v", target, err)
	}
}
