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

func TestLintUsesPinnedToolAndForwardsArguments(t *testing.T) {
	work := lifecycleWork(t)
	tool := filepath.Join(work, "go-fixture")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "scripts/golangci-lint.sh", "run", "--concurrency=1", "--build-tags=integration", "./...")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "GO="+tool)
	out, err := cmd.CombinedOutput()
	want := "run\ngithub.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0\nrun\n--concurrency=1\n--build-tags=integration\n./...\n"
	if err != nil || string(out) != want {
		t.Fatalf("pinned lint invocation: %v\n%s", err, out)
	}
	cmd = exec.Command("make", "-n", "golangci-lint")
	cmd.Dir = repoRoot(t)
	out, err = cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "scripts/verify-contained.sh bash scripts/golangci-lint.sh run --concurrency=1") {
		t.Fatalf("lint target bypasses pinned contained tool: %v\n%s", err, out)
	}
}

func TestAndroidSDKInstallerProvidesCompileAndRuntimePlatforms(t *testing.T) {
	work := lifecycleWork(t)
	tools := filepath.Join(work, "tools")
	if err := os.MkdirAll(tools, 0o700); err != nil {
		t.Fatal(err)
	}
	fixtures := map[string]string{
		"java": "#!/bin/sh\nexit 0\n",
		"curl": "#!/bin/sh\nexit 0\n",
		"unzip": `#!/bin/sh
while [ "$1" != "-d" ]; do shift; done
shift
mkdir -p "$1/cmdline-tools/bin"
cat > "$1/cmdline-tools/bin/sdkmanager" <<'SDKMANAGER'
#!/bin/sh
printf '%s\n' "$@" >> "$SDK_FIXTURE_LOG"
exit 0
SDKMANAGER
chmod 700 "$1/cmdline-tools/bin/sdkmanager"
`,
	}
	for name, body := range fixtures {
		if err := os.WriteFile(filepath.Join(tools, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(work, "sdk-calls.log")
	cmd := exec.Command("bash", "android/scripts/install-android-sdk.sh")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "PATH="+tools+":"+os.Getenv("PATH"), "TMPDIR="+work,
		"ANDROID_SDK_ROOT="+filepath.Join(work, "sdk"), "SDK_FIXTURE_LOG="+log)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("SDK fixture installer: %v\n%s", err, out)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"platforms;android-37.0", "build-tools;36.0.0", "system-images;android-35;default;x86_64"} {
		if !strings.Contains(string(calls), required+"\n") {
			t.Errorf("SDK installer missing %s: %s", required, calls)
		}
	}
}
