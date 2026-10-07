package lifecycle

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type resourceInfoRunner struct {
	rootless, manager string
	calls             []command
}

func (r *resourceInfoRunner) run(_ context.Context, c command) ([]byte, error) {
	r.calls = append(r.calls, c)
	if c.Name != "podman" || len(c.Args) != 3 || c.Args[0] != "info" {
		return nil, fmt.Errorf("container work started before resource prerequisites: %s %v", c.Name, c.Args)
	}
	if c.Args[2] == "{{.Host.Security.Rootless}}" {
		return []byte(r.rootless), nil
	}
	return []byte(r.manager), nil
}

func TestContainerRequiresRootlessSystemdResourceContainment(t *testing.T) {
	for _, tc := range []struct {
		rootless, manager string
		wantError         bool
	}{
		{"true", "systemd", false}, {"false", "systemd", true}, {"true", "cgroupfs", true},
	} {
		t.Run(tc.rootless+"/"+tc.manager, func(t *testing.T) {
			r := &resourceInfoRunner{rootless: tc.rootless, manager: tc.manager}
			w := &workflow{root: ".", r: r}
			err := w.verificationPodman(t.Context())
			if (err != nil) != tc.wantError {
				t.Fatalf("rootless/systemd prerequisite: %v", err)
			}
			if tc.wantError {
				if err := w.buildContainer(t.Context(), &receipt{}, "0"); err == nil {
					t.Fatal("container build accepted an escaping resource configuration")
				}
			}
			for _, c := range r.calls {
				if c.Name != "podman" || c.Args[0] != "info" {
					t.Fatalf("container work ran before prerequisites: %v", c)
				}
			}
		})
	}
}

func TestDevelopmentRootlessPodmanDoesNotRequireVerificationManager(t *testing.T) {
	r := &resourceInfoRunner{rootless: "true", manager: "cgroupfs"}
	w := &workflow{root: ".", r: r}
	if err := w.rootlessPodman(t.Context()); err != nil {
		t.Fatalf("development rootless prerequisite: %v", err)
	}
}

func TestVerificationWrapperContainsEntireCommand(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	trace := filepath.Join(dir, "trace")
	for name, body := range map[string]string{
		"awk":         "echo /uncontained\n",
		"systemctl":   "printf '%s\\n' \"$@\" >> \"$TRACE\"\n",
		"systemd-run": "printf '%s\\n' \"$@\" >> \"$TRACE\"\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("bash", filepath.Join(root, "scripts", "verify-contained.sh"), "printf", "a b")
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "TRACE="+trace)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("wrapper: %v: %s", err, out)
	}
	out, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"CPUQuota=100%\n", "MemoryMax=3G\n", "MemorySwapMax=0\n", "--slice=lingonverification.slice\n", "GOMAXPROCS=1\n", "GOFLAGS=-p=1\n", "printf\na b\n"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
}

func TestAndroidVerificationRejectsContainmentBypass(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte("#!/bin/sh\necho unexpected-tool-start >&2\nexit 99\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "../../android/scripts/run-integration-tests.sh")
	cmd.Env = append(os.Environ(), "LINGON_IT_CGROUP=0", "LINGON_IT_CGROUP_WRAPPED=1", "PATH="+dir+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "containment cannot be disabled") {
		t.Fatalf("must reject bypass before starting tools: %v: %s", err, out)
	}
}

func TestVerificationWrapperFailsBeforeCommandWhenLimitsCannotBeSet(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"awk":         "echo /uncontained\n",
		"systemctl":   "exit 23\n",
		"systemd-run": "echo unexpected-command-start\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("bash", "../../scripts/verify-contained.sh", "/bin/true")
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err == nil || strings.Contains(string(out), "unexpected-command-start") {
		t.Fatalf("must stop when containment setup fails: %v: %s", err, out)
	}
}

func TestAndroidVerificationRejectsEscapingResourceOptions(t *testing.T) {
	for _, option := range []string{"LINGON_IT_CPU_QUOTA_PERCENT=200", "LINGON_IT_MEMORY_MAX=7G", "LINGON_IT_MEMORY_SWAP_MAX=1G", "LINGON_IT_REUSE_DEVICE=1"} {
		t.Run(option, func(t *testing.T) {
			cmd := exec.Command("bash", "../../android/scripts/run-integration-tests.sh")
			cmd.Env = append(os.Environ(), "LINGON_IT_CGROUP=1", "LINGON_IT_CGROUP_WRAPPED=1", option)
			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), "requires 1 CPU / 3 GiB") {
				t.Fatalf("must reject escaping options before tools start: %v: %s", err, out)
			}
		})
	}
}
