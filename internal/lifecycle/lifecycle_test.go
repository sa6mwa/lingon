package lifecycle

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fixtures live under build/ and own their Git histories/remotes. No test
// touches the caller's tags, branch, terminal, registry or GitHub repository.
func testWorkflow(t *testing.T) (*workflow, *fakeRunner) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root not found")
		}
		dir = parent
	}
	base := filepath.Join(dir, "build", "lifecycle-tests")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(base, "case-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	w := &workflow{root: root, work: filepath.Join(root, "build", "lifecycle")}
	if err := os.MkdirAll(w.work, 0o700); err != nil {
		t.Fatal(err)
	}
	f := &fakeRunner{t: t, real: processRunner{log: io.Discard}, version: "0.0.0", assets: make(map[string][]byte)}
	w.r = f
	files := map[string]string{
		".gitignore":         "/build/\n/dist/\n/origin.git/\n/sdk/\n/android/app/build/\n/android/test-artifacts/\n",
		"go.mod":             "module pkt.systems/lingon\n\ngo 1.26\n",
		"cmd/lingon/main.go": "package main\n\nimport (\"fmt\"; \"strings\"; \"pkt.systems/lingon/internal/buildmeta\")\n\nfunc main() { fmt.Println(strings.TrimPrefix(buildmeta.ReleaseVersion(), \"v\")) }\n",
		"LICENSE":            "fixture license\n", "Containerfile": "FROM scratch\nCOPY lingon /usr/bin/lingon\n",
		"android/app/src/androidTest/OtherTest.kt":    "package fixture\n@RunWith(AndroidJUnit4::class)\nclass OtherTest {\n@Test\nfun example() {}\n}\n",
		"android/app/src/androidTest/EndToEndTest.kt": "package fixture\nclass EndToEndTest {\n@Test\nfun e2e() {}\n@Test\nfun cleartext_share_authentication_uses_token_fallback() {}\n}\n",
		"android/scripts/verify-release-cert.sh":      "fixture\n",
		"sdk/build-tools/36.0.0/aapt":                 "fixture\n", "sdk/build-tools/36.0.0/apksigner": "fixture\n",
	}
	stampSource, err := os.ReadFile(filepath.Join(dir, "internal", "buildmeta", "version.go"))
	if err != nil {
		t.Fatal(err)
	}
	files["internal/buildmeta/version.go"] = string(stampSource)
	for name, data := range files {
		writeTestFile(t, filepath.Join(root, name), []byte(data))
	}
	t.Setenv("ANDROID_SDK_ROOT", filepath.Join(root, "sdk"))
	testGit(t, w, "init", "--initial-branch=trunk")
	testGit(t, w, "config", "user.name", "Lifecycle Fixture")
	testGit(t, w, "config", "user.email", "fixture@example.invalid")
	testGit(t, w, "config", "commit.gpgSign", "false")
	testGit(t, w, "config", "tag.gpgSign", "false")
	testGit(t, w, "add", ".gitignore", "go.mod", "cmd", "internal", "LICENSE", "Containerfile", "android")
	testGit(t, w, "commit", "-m", "chore(fixture): initialize release input")
	testGit(t, w, "init", "--bare", "--initial-branch=trunk", "origin.git")
	testGit(t, w, "remote", "add", "origin", filepath.Join(root, "origin.git"))
	testGit(t, w, "push", "origin", "trunk")
	testGit(t, w, "switch", "-c", "feature/example")
	writeTestFile(t, filepath.Join(root, "feature.txt"), []byte("feature\n"))
	testGit(t, w, "add", "feature.txt")
	testGit(t, w, "commit", "-m", "feat(fixture): add observable behavior")
	f.calls = nil
	return w, f
}

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func testGit(t *testing.T, w *workflow, args ...string) string {
	t.Helper()
	out, err := w.git(context.Background(), args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

type fakeRunner struct {
	t                 *testing.T
	real              runner
	calls             []command
	version           string
	fail              string
	mutate            func(command)
	ghExists, ghDraft bool
	assets            map[string][]byte
	registryExists    bool
	immutableDisabled bool
	rejectFinal       bool
}

const fixtureManifest = `{"schemaVersion":2,"manifests":[{"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","platform":{"os":"linux","architecture":"amd64"}},{"digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","platform":{"os":"linux","architecture":"arm64"}}]}`

func (f *fakeRunner) run(ctx context.Context, c command) ([]byte, error) {
	// Fixture histories cannot inherit the user's repository/index overrides,
	// global hooks or signing configuration. Local fixture config is explicit.
	if c.Name == "git" || c.Name == "go" {
		c.Unset = []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS", "GIT_NAMESPACE", "GPG_TTY"}
		if c.Env == nil {
			c.Env = make(map[string]string)
		}
		c.Env["GIT_CONFIG_NOSYSTEM"] = "1"
		c.Env["GIT_CONFIG_GLOBAL"] = os.DevNull
		c.Env["GIT_CONFIG_SYSTEM"] = os.DevNull
	}
	f.calls = append(f.calls, c)
	if f.rejectFinal && c.Name == "go" && len(c.Args) > 0 && c.Args[0] == "test" {
		out, err := f.real.run(ctx, command{Dir: c.Dir, Name: "git", Args: []string{"tag", "--points-at", "HEAD"}})
		if err == nil && strings.Contains(string(out), "v1.2.3") {
			return nil, errors.New("injected final tagged gate failure")
		}
	}
	text := c.Name + " " + strings.Join(c.Args, " ")
	if f.fail != "" && strings.Contains(text, f.fail) {
		return nil, errors.New("injected gate failure")
	}
	if f.mutate != nil {
		f.mutate(c)
	}
	if c.Name == "git" {
		if strings.Join(c.Args, " ") == "remote get-url origin" {
			return []byte("git@github.com:" + githubRepository + ".git"), nil
		}
		return f.real.run(ctx, c)
	}
	if c.Name == "go" && len(c.Args) > 0 && c.Args[0] == "build" {
		// Build real small Go executables for every target, preserving real
		// build metadata and ELF/Mach-O verification. External suites are fake.
		for _, arg := range c.Args {
			if strings.HasPrefix(arg, "-ldflags=") {
				_, version, _ := strings.Cut(arg, "buildVersion=v")
				f.version = strings.Fields(version)[0]
			}
		}
		return f.real.run(ctx, c)
	}
	if c.Name == "go" && len(c.Args) > 0 && c.Args[0] == "env" {
		return f.real.run(ctx, c)
	}
	if c.Name == "go" && len(c.Args) > 0 && c.Args[0] == "run" {
		if strings.Contains(strings.Join(c.Args, " "), "test-names") {
			if strings.HasSuffix(c.Args[len(c.Args)-1], "EndToEndTest.kt") {
				return []byte("e2e\n" + cleartextTest + "\n"), nil
			}
			return []byte("example\n"), nil
		}
	}
	if c.Name == "bash" && len(c.Args) > 0 && c.Args[0] == "android/scripts/run-integration-tests.sh" {
		phase := "e2e"
		class := "fixture.EndToEndTest"
		methods := []string{"e2e", cleartextTest}
		if c.Env["LINGON_IT_CLASS"] != "" {
			phase = "other"
			class = "fixture.OtherTest"
			methods = []string{"example"}
		}
		if c.Env["LINGON_IT_CLEARTEXT"] == "1" {
			phase = "cleartext"
			methods = []string{cleartextTest}
		}
		profile := filepath.Join(c.Dir, "android", "test-artifacts", "resource-profile-fixture-"+phase)
		var report strings.Builder
		report.WriteString(`<testsuite failures="0" errors="0">`)
		for _, method := range methods {
			fmt.Fprintf(&report, `<testcase classname="%s" name="%s">`, class, method)
			if method == cleartextTest && c.Env["LINGON_IT_CLEARTEXT"] == "0" {
				report.WriteString(`<skipped/>`)
			}
			report.WriteString(`</testcase>`)
		}
		report.WriteString(`</testsuite>`)
		writeTestFile(f.t, filepath.Join(profile, "android-test-batches", "batch-1", "TEST-fixture.xml"), []byte(report.String()))
		return []byte("samples_path=" + filepath.Join(profile, "samples.jsonl") + "\n"), nil
	}
	if c.Name == "./gradlew" {
		for _, arg := range c.Args {
			if strings.HasPrefix(arg, "-Plingon.versionName=v") {
				f.version = strings.TrimPrefix(arg, "-Plingon.versionName=v")
				path := filepath.Join(c.Dir, "app/build/outputs/apk/release/app-release.apk")
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					return nil, err
				}
				file, err := os.Create(path)
				if err != nil {
					return nil, err
				}
				z := zip.NewWriter(file)
				entry, err := z.Create("classes.dex")
				if err == nil {
					_, err = entry.Write([]byte("fixture APK payload"))
				}
				_ = z.Close()
				_ = file.Close()
				if err != nil {
					return nil, err
				}
			}
		}
	}
	if filepath.Base(c.Name) == "aapt" {
		return []byte("package: name='systems.pkt.lingon' versionCode='" + androidVersionCode(f.version) + "' versionName='v" + f.version + "'"), nil
	}
	if filepath.Base(c.Name) == "lingon" {
		return f.real.run(ctx, c)
	}
	if c.Name == "podman" {
		switch {
		case c.Args[0] == "info":
			if c.Args[len(c.Args)-1] == "{{.Host.CgroupManager}}" {
				return []byte("systemd\n"), nil
			}
			return []byte("true\n"), nil
		case c.Args[0] == "image":
			return []byte("docker.io/library/alpine@sha256:" + strings.Repeat("a", 64)), nil
		case c.Args[0] == "run":
			return []byte(f.version + "\n"), nil
		case strings.HasPrefix(strings.Join(c.Args, " "), "manifest inspect"):
			if strings.HasPrefix(c.Args[2], "registry-1.docker.io/") && !f.registryExists {
				return []byte("manifest unknown"), errors.New("manifest unknown")
			}
			return []byte(fixtureManifest), nil
		case strings.HasPrefix(strings.Join(c.Args, " "), "manifest exists"):
			return nil, errors.New("fixture manifest not present")
		case strings.HasPrefix(strings.Join(c.Args, " "), "manifest push"):
			for _, arg := range c.Args {
				if strings.HasPrefix(arg, "--digestfile=") {
					writeTestFile(f.t, strings.TrimPrefix(arg, "--digestfile="), []byte("sha256:"+strings.Repeat("c", 64)))
				}
			}
			f.registryExists = true
		}
	}
	if c.Name == "gh" {
		if c.Args[0] == "api" {
			return []byte(fmt.Sprintf(`{"enabled":%t}`, !f.immutableDisabled)), nil
		}
		return f.gh(c)
	}
	return nil, nil
}

func (f *fakeRunner) gh(c command) ([]byte, error) {
	switch c.Args[1] {
	case "list":
		if f.ghExists {
			return []byte(fmt.Sprintf(`[{"tagName":"v%s","isDraft":%t}]`, f.version, f.ghDraft)), nil
		}
		return []byte("[]"), nil
	case "create":
		f.ghExists, f.ghDraft = true, true
	case "view":
		r := githubRelease{TagName: "v" + f.version, IsDraft: f.ghDraft}
		for name := range f.assets {
			r.Assets = append(r.Assets, struct{ Name string }{Name: name})
		}
		return json.Marshal(r)
	case "upload":
		path := c.Args[3]
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		name := filepath.Base(path)
		if _, exists := f.assets[name]; exists {
			return nil, fmt.Errorf("refusing replacement")
		}
		f.assets[name] = data
	case "download":
		var dir, pattern string
		for i, arg := range c.Args {
			if arg == "--dir" {
				dir = c.Args[i+1]
			}
			if arg == "--pattern" {
				pattern = c.Args[i+1]
			}
		}
		data, exists := f.assets[pattern]
		if !exists {
			return nil, fmt.Errorf("missing asset")
		}
		writeTestFile(f.t, filepath.Join(dir, pattern), data)
	case "edit":
		f.ghDraft = false
	}
	return nil, nil
}

func TestExactLightweightVersionAndNoVersionInference(t *testing.T) {
	w, _ := testWorkflow(t)
	ctx := context.Background()
	for _, want := range []string{"0.0.0", "1.2.3"} {
		if want != "0.0.0" {
			testGit(t, w, "tag", "v"+want)
		}
		got, err := w.exactVersion(ctx)
		if err != nil || got != want {
			t.Fatalf("version=%q err=%v want=%q", got, err, want)
		}
	}
	testGit(t, w, "tag", "-d", "v1.2.3")
	testGit(t, w, "tag", "-a", "v1.2.3", "-m", "annotated fixture")
	if _, err := w.exactVersion(ctx); err == nil {
		t.Fatal("annotated tag accepted")
	}
}

func TestCandidateFailureStopsBeforeSquashTagAndPublish(t *testing.T) {
	w, f := testWorkflow(t)
	before := testGit(t, w, "rev-parse", "trunk")
	f.fail = "go test -count=1"
	err := w.release(context.Background(), Options{Version: "1.2.3", Message: "feat(fixture): add behavior"})
	if err == nil || !strings.Contains(err.Error(), "Go tests") {
		t.Fatalf("unexpected failure: %v", err)
	}
	if testGit(t, w, "rev-parse", "trunk") != before || testGit(t, w, "tag", "--list") != "" {
		t.Fatal("release mutated trunk/tags after a failed candidate gate")
	}
	for _, c := range f.calls {
		if c.Name == "gh" || c.Name == "podman" {
			t.Fatalf("external publication/build after failure: %+v", c)
		}
	}
}

func TestStaleSourceStopsCandidateProof(t *testing.T) {
	w, f := testWorkflow(t)
	f.mutate = func(c command) {
		if c.Name == "go" && len(c.Args) > 0 && c.Args[0] == "vet" {
			writeTestFile(t, filepath.Join(w.root, "feature.txt"), []byte("changed during gate"))
		}
	}
	if err := w.prerelease(context.Background()); err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("stale source accepted: %v", err)
	}
}

func TestReleaseSquashesSameTreeAndFinalContainerPushCanRetry(t *testing.T) {
	w, f := testWorkflow(t)
	ctx := context.Background()
	candidate := testGit(t, w, "rev-parse", "HEAD")
	tree := testGit(t, w, "rev-parse", "HEAD^{tree}")
	if err := w.release(ctx, Options{Version: "1.2.3", Message: "feat(fixture): add behavior"}); err != nil {
		t.Fatal(err)
	}
	if testGit(t, w, "symbolic-ref", "--short", "HEAD") != "trunk" || testGit(t, w, "cat-file", "-t", "v1.2.3") != "commit" {
		t.Fatal("not a lightweight trunk release")
	}
	if testGit(t, w, "rev-parse", "HEAD^{tree}") != tree || testGit(t, w, "rev-parse", "feature/example") != candidate {
		t.Fatal("squash changed verified content or source branch")
	}
	if n := testGit(t, w, "rev-list", "--count", "trunk"); n != "2" {
		t.Fatalf("expected one squash commit, got %s", n)
	}
	r, err := w.verified(ctx)
	if err != nil || !r.FullProof {
		t.Fatalf("not publishable: %+v %v", r, err)
	}
	if err := w.matrix(ctx); err == nil {
		t.Fatal("verified tagged artifacts can be rebuilt via release-matrix")
	}
	if err := w.release(ctx, Options{}); err == nil {
		t.Fatal("completed local release can be rebuilt")
	}
	f.fail = "podman manifest push"
	if err := w.publish(ctx); err == nil || !strings.Contains(err.Error(), "GitHub remain published") {
		t.Fatalf("final push did not fail separately: %v", err)
	}
	if f.ghDraft || len(f.assets) != len(expectedArtifacts(r.Version))+1 {
		t.Fatal("GitHub was not fully published before container push")
	}
	f.fail = ""
	f.calls = nil
	if err := w.pushContainer(ctx); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.calls {
		if c.Name == "gh" || (c.Name == "go") || (c.Name == "git" && len(c.Args) > 0 && c.Args[0] == "push") {
			t.Fatalf("retry repeated publication/build: %+v", c)
		}
	}
	if err := w.release(ctx, Options{}); err == nil {
		t.Fatal("pushed version could be rebuilt")
	}
	before := len(f.calls)
	if err := w.pushContainer(ctx); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.calls[before:] {
		if c.Name == "podman" && strings.HasPrefix(strings.Join(c.Args, " "), "manifest push") {
			t.Fatal("completed image was overwritten")
		}
	}
}

func TestFinalTaggedFailureRetainsStateAndNeverPublishesOrRetries(t *testing.T) {
	w, f := testWorkflow(t)
	f.rejectFinal = true
	err := w.release(context.Background(), Options{Version: "1.2.3", Message: "feat(fixture): add behavior"})
	if err == nil || !strings.Contains(err.Error(), "final tagged gate failure") {
		t.Fatalf("final gate failure not enforced: %v", err)
	}
	if testGit(t, w, "symbolic-ref", "--short", "HEAD") != "trunk" || testGit(t, w, "cat-file", "-t", "v1.2.3") != "commit" {
		t.Fatal("failed attempt silently altered its branch/tag state")
	}
	if _, err := w.verified(context.Background()); err == nil {
		t.Fatal("candidate proof could authorize tagged artifacts after failure")
	}
	if err := w.release(context.Background(), Options{}); err == nil || !strings.Contains(err.Error(), "already attempted") {
		t.Fatalf("failed release gate could retry: %v", err)
	}
	for _, c := range f.calls {
		if c.Name == "gh" || (c.Name == "git" && len(c.Args) > 0 && c.Args[0] == "push") {
			t.Fatal("failed tagged release published externally")
		}
	}
}

func TestChecksumsRejectMissingExtraDuplicateAndEscapingArtifacts(t *testing.T) {
	w, _ := testWorkflow(t)
	dir := w.dist("1.2.3")
	for _, name := range expectedArtifacts("1.2.3") {
		writeTestFile(t, filepath.Join(dir, name), []byte(name))
	}
	if _, err := makeChecksums(dir, "1.2.3"); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyChecksums(dir, "1.2.3"); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, checksumName("1.2.3"))
	original, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{string(original) + strings.Split(string(original), "\n")[0] + "\n", strings.Replace(string(original), expectedArtifacts("1.2.3")[0], "../escape", 1), strings.Replace(string(original), strings.Repeat("a", 1), "Z", 1)} {
		writeTestFile(t, manifest, []byte(bad))
		if _, err := verifyChecksums(dir, "1.2.3"); err == nil {
			t.Fatal("bad manifest accepted")
		}
	}
	writeTestFile(t, manifest, original)
	writeTestFile(t, filepath.Join(dir, "extra.tar.gz"), []byte("extra"))
	if _, err := verifyChecksums(dir, "1.2.3"); err == nil {
		t.Fatal("extra artifact accepted")
	}
	if err := os.Remove(filepath.Join(dir, "extra.tar.gz")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, expectedArtifacts("1.2.3")[0])); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyChecksums(dir, "1.2.3"); err == nil {
		t.Fatal("missing artifact accepted")
	}
}

func TestPrivacyDetectsChunkBoundaryAndContainerManifestContract(t *testing.T) {
	w, _ := testWorkflow(t)
	for _, secret := range []string{w.root, "/home/engineer/project", "/Users/engineer/private", "file:///private", "-race=true"} {
		input := strings.Repeat("x", 32766) + secret + strings.Repeat("y", 33000)
		if err := w.scanPrivate(strings.NewReader(input), "fixture"); err == nil {
			t.Fatalf("leak escaped: %s", secret)
		}
	}
	if err := inspectImageManifest([]byte(fixtureManifest)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`{"manifests":[]}`, strings.Replace(fixtureManifest, "arm64", "amd64", 1), strings.Replace(fixtureManifest, "linux", "windows", 1)} {
		if err := inspectImageManifest([]byte(bad)); err == nil {
			t.Fatal("bad platform manifest accepted")
		}
	}
}

func TestAndroidVersionCodesIncreaseWithSemver(t *testing.T) {
	for version, want := range map[string]string{"0.0.0": "1", "0.4.0": "4000", "1.2.3": "1002003", "2.0.0": "2000000", "1.1000.0": "0", "9999.0.0": "0"} {
		if got := androidVersionCode(version); got != want {
			t.Fatalf("%s=%s want %s", version, got, want)
		}
	}
}
