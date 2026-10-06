package buildscripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func lifecycleWork(t *testing.T) string {
	t.Helper()
	base := filepath.Join(repoRoot(t), "build", "make-contract-tests")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	work, err := os.MkdirTemp(base, "case-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(work) })
	return work
}

func TestRootBuildRebuildsExistingBinary(t *testing.T) {
	work := lifecycleWork(t)
	tool := filepath.Join(work, "go-fixture")
	log := filepath.Join(work, "calls.log")
	bin := filepath.Join(work, "lingon")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$FAKE_GO_LOG\"\n"
	if err := os.WriteFile(tool, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("pre-existing stale binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("make", "build", "GO="+tool, "BIN="+bin)
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "FAKE_GO_LOG="+log)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make build: %v\n%s", err, out)
	}
	calls, err := os.ReadFile(log)
	if err != nil || !strings.Contains(string(calls), "build -buildvcs=true") {
		t.Fatalf("existing output skipped source rebuild: %q %v", calls, err)
	}
}

func TestLifecycleTargetsAreSerialAndPublicationDoesNotBuild(t *testing.T) {
	for _, target := range []string{"prerelease", "release", "release-matrix", "package-verify", "publish", "push-container", "dev-up", "dev-down", "dev-reset", "test-short"} {
		t.Run(target, func(t *testing.T) {
			cmd := exec.Command("make", "-n", target)
			cmd.Dir = repoRoot(t)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("make %s: %v\n%s", target, err, out)
			}
			if target == "publish" || target == "push-container" {
				if strings.Contains(string(out), "./cmd/lingon ") || strings.Contains(string(out), "assembleRelease") || strings.Contains(string(out), "podman build") {
					t.Fatalf("publication rebuilt payloads: %s", out)
				}
			}
		})
	}
	for _, name := range []string{"Makefile", "android/Makefile"} {
		data, err := os.ReadFile(filepath.Join(repoRoot(t), name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), ".NOTPARALLEL:") {
			t.Fatalf("%s can parallelize release work", name)
		}
		if strings.Contains(string(data), "nerdctl") || strings.Contains(string(data), "CONTAINER_BUILDER") {
			t.Fatalf("%s retains alternate engines", name)
		}
	}
}

func TestAndroidBuildUsesPropertiesWithoutMutatingTrackedMetadata(t *testing.T) {
	cmd := exec.Command("make", "-n", "-C", filepath.Join(repoRoot(t), "android"), "release")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Android release plan: %v\n%s", err, out)
	}
	text := string(out)
	if strings.Contains(text, "sed -i") || !strings.Contains(text, "-Plingon.versionName=") || !strings.Contains(text, "-Plingon.versionCode=") {
		t.Fatalf("release still rewrites metadata or loses its version: %s", out)
	}
}
