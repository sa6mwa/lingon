package lifecycle

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
)

func TestPreparationRejectsDirtyInvalidVersionAndExistingTags(t *testing.T) {
	w, _ := testWorkflow(t)
	for _, v := range []string{"", "1.2", "1.2.3-rc1", "01.2.3", "0.0.0", "1.1000.0"} {
		if err := w.release(context.Background(), Options{Version: v, Message: "feat(fixture): behavior"}); err == nil {
			t.Fatalf("version %q accepted", v)
		}
	}
	testGit(t, w, "tag", "v1.2.3")
	if err := w.release(context.Background(), Options{Version: "1.2.3", Message: "feat(fixture): behavior"}); err == nil {
		t.Fatal("existing version accepted")
	}
	writeTestFile(t, filepath.Join(w.root, "feature.txt"), []byte("dirty"))
	if err := w.release(context.Background(), Options{Version: "1.3.0", Message: "feat(fixture): behavior"}); err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("dirty worktree accepted: %v", err)
	}
}

func TestBranchChangeAtSameCommitStopsProof(t *testing.T) {
	w, f := testWorkflow(t)
	head := testGit(t, w, "rev-parse", "HEAD")
	f.mutate = func(c command) {
		if c.Name == "go" && len(c.Args) > 0 && c.Args[0] == "vet" {
			if _, err := f.real.run(context.Background(), command{Dir: w.root, Name: "git", Args: []string{"switch", "-c", "other-same-head"}}); err != nil {
				t.Error(err)
			}
		}
	}
	if err := w.proof(context.Background(), head); err == nil || !strings.Contains(err.Error(), "branch changed") {
		t.Fatalf("same-commit branch switch accepted: %v", err)
	}
}

func TestProofIncludesAllModulesAndBothAndroidInstrumentationGroups(t *testing.T) {
	w, f := testWorkflow(t)
	head := testGit(t, w, "rev-parse", "HEAD")
	if err := w.proof(context.Background(), head); err != nil {
		t.Fatal(err)
	}
	var endToEnd, other, rootRace, androidRace, cleartext bool
	for _, c := range f.calls {
		text := strings.Join(c.Args, " ")
		if c.Name == "go" && strings.Contains(text, "test -race -count=1") {
			if c.Dir == w.root {
				rootRace = true
			}
			if c.Dir == filepath.Join(w.root, "android") {
				androidRace = true
			}
		}
		if c.Name == "bash" && strings.Contains(text, "run-integration-tests.sh") {
			if (c.Env["LINGON_IT_ONLY"] != "" && !(c.Env["LINGON_IT_ONLY"] == cleartextTest && c.Env["LINGON_IT_CLEARTEXT"] == "1")) || c.Env["LINGON_IT_KEEP_EMULATOR"] != "0" || c.Env["LINGON_IT_REUSE_DEVICE"] != "0" {
				t.Fatal("partial/unowned Android verification could escape")
			}
			endToEnd = endToEnd || (c.Env["LINGON_IT_CLASS"] == "" && c.Env["LINGON_IT_ONLY"] == "")
			cleartext = cleartext || c.Env["LINGON_IT_CLEARTEXT"] == "1"
			other = other || c.Env["LINGON_IT_CLASS"] == "fixture.OtherTest"
		}
	}
	if !endToEnd || !other || !rootRace || !androidRace || !cleartext {
		t.Fatalf("missing proof group: e2e=%v other=%v rootRace=%v androidRace=%v", endToEnd, other, rootRace, androidRace)
	}
}

func TestGitHubDraftRetryNeverOverwritesAssetsOrPublishedRelease(t *testing.T) {
	w, f := testWorkflow(t)
	r := receipt{Version: "1.2.3"}
	f.version = r.Version
	for _, name := range expectedArtifacts(r.Version) {
		writeTestFile(t, filepath.Join(w.dist(r.Version), name), []byte(name))
	}
	if _, err := makeChecksums(w.dist(r.Version), r.Version); err != nil {
		t.Fatal(err)
	}
	f.fail = "gh release upload v1.2.3 " + filepath.Join(w.dist(r.Version), "lingon-1.2.3-linux-amd64.tar.gz")
	if err := w.publishGitHub(context.Background(), r); err == nil {
		t.Fatal("failed upload accepted")
	}
	if !f.ghDraft || len(f.assets) == 0 {
		t.Fatal("expected a resumable partial draft")
	}
	before := make(map[string][]byte)
	for name, data := range f.assets {
		before[name] = append([]byte(nil), data...)
	}
	f.fail = ""
	if err := w.publishGitHub(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if f.ghDraft {
		t.Fatal("completed verified draft was not published")
	}
	for name, data := range before {
		if !bytes.Equal(f.assets[name], data) {
			t.Fatal("partial asset changed during retry")
		}
	}
	name := expectedArtifacts(r.Version)[0]
	f.assets[name] = []byte("wrong remote bytes")
	if err := w.publishGitHub(context.Background(), r); err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("changed remote asset accepted: %v", err)
	}
	delete(f.assets, name)
	if err := w.publishGitHub(context.Background(), r); err == nil || !strings.Contains(err.Error(), "roll forward") {
		t.Fatalf("published assets could be added: %v", err)
	}
}

func TestRegistryReadDoesNotTreatNetworkFailureAsMissingTag(t *testing.T) {
	w, f := testWorkflow(t)
	if _, exists, err := w.registryManifest(context.Background(), "v1.2.3"); err != nil || exists {
		t.Fatalf("not-found mapping: %v %v", exists, err)
	}
	f.fail = "podman manifest inspect"
	if _, _, err := w.registryManifest(context.Background(), "v1.2.3"); err == nil {
		t.Fatal("network/auth failure accepted as absent version")
	}
}

func TestPublicationStateBindsArtifactsAndContainer(t *testing.T) {
	w, _ := testWorkflow(t)
	r := receipt{Version: "1.2.3", ManifestHash: "first", ImageID: "first-image"}
	p, err := w.publication(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.savePublication(r, p); err != nil {
		t.Fatal(err)
	}
	r.ManifestHash = "changed"
	if _, err := w.publication(r); err == nil {
		t.Fatal("changed manifest authorized retry")
	}
	r.ManifestHash = "first"
	r.ImageID = "changed"
	if _, err := w.publication(r); err == nil {
		t.Fatal("changed image authorized retry")
	}
}

func TestAndroidReportsRequireEveryBatchAndRejectSkippedDuplicateOrFailedCases(t *testing.T) {
	w, _ := testWorkflow(t)
	one := filepath.Join(w.work, "batch-1.xml")
	two := filepath.Join(w.work, "batch-2.xml")
	writeTestFile(t, one, []byte(`<testsuite failures="0" errors="0"><testcase classname="C" name="one"/></testsuite>`))
	writeTestFile(t, two, []byte(`<testsuite failures="0" errors="0"><testcase classname="C" name="two"/></testsuite>`))
	want := map[string]bool{"C#one": true, "C#two": true}
	if err := checkAndroidReports([]string{one, two}, want, nil); err != nil {
		t.Fatal(err)
	}
	if err := checkAndroidReports([]string{two}, want, nil); err == nil {
		t.Fatal("final-only evidence accepted")
	}
	for _, body := range []string{
		`<testcase classname="C" name="one"/>`,
		`<testcase classname="C" name="two"><skipped/></testcase>`,
		`<testcase classname="C" name="two"><failure/></testcase>`,
		`<testcase classname="Other" name="two"/>`,
	} {
		writeTestFile(t, two, []byte(`<testsuite failures="0" errors="0">`+body+`</testsuite>`))
		if err := checkAndroidReports([]string{one, two}, want, nil); err == nil {
			t.Fatalf("bad coverage accepted: %s", body)
		}
	}
}

func TestPublishChecksServerImmutabilityBeforeAnyWrite(t *testing.T) {
	w, f := testWorkflow(t)
	f.immutableDisabled = true
	if err := w.publishPreconditions(context.Background()); err == nil {
		t.Fatal("mutable GitHub releases accepted")
	}
	f.immutableDisabled = false
	if err := w.publishPreconditions(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.fail = "gh api"
	if err := w.publishPreconditions(context.Background()); err == nil {
		t.Fatal("unreadable server policy accepted")
	}
	for _, c := range f.calls {
		if c.Name == "git" && len(c.Args) > 0 && c.Args[0] == "push" {
			t.Fatal("push happened before immutability proof")
		}
	}
}

func TestTailCaptureIsBoundedAndDoesNotTruncateLog(t *testing.T) {
	var tail tailBuffer
	var log bytes.Buffer
	w := io.MultiWriter(&log, &tail)
	data := strings.Repeat("a", 2*1024*1024) + "the end"
	if _, err := io.WriteString(w, data); err != nil {
		t.Fatal(err)
	}
	if log.String() != data || tail.Len() != 1024*1024 || !strings.HasSuffix(tail.String(), "the end") {
		t.Fatal("bounded command tail or complete log contract failed")
	}
}

func TestPublicRunResolvesRootBeforeCreatingScratchAndHonorsLock(t *testing.T) {
	w, _ := testWorkflow(t)
	root := filepath.Join(w.root, "cmd", "lingon")
	if err := Run(context.Background(), Options{Root: root, Operation: "version"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "build")); !os.IsNotExist(err) {
		t.Fatal("scratch was created beside source files")
	}
	logs, err := filepath.Glob(filepath.Join(w.work, "version-*.log"))
	if err != nil || len(logs) != 1 {
		t.Fatalf("root log missing: %v %v", logs, err)
	}
	lock := filepath.Join(w.work, "operation.lock")
	writeTestFile(t, lock, []byte("owned running operation"))
	if err := Run(context.Background(), Options{Root: root, Operation: "version"}); err == nil || !strings.Contains(err.Error(), "another lifecycle operation") {
		t.Fatalf("concurrent operation accepted: %v", err)
	}
	data, err := os.ReadFile(lock)
	if err != nil || string(data) != "owned running operation" {
		t.Fatal("another operation's lock was removed")
	}
}

// Timer creation is observable to the test; advancing the injected clock proves
// the deadline without a wall-clock sleep/poll loop.
type observedClock struct {
	*clock.MockClock
	timerReady chan struct{}
}

func (c *observedClock) NewTimer(d time.Duration) *clock.Timer {
	t := c.MockClock.NewTimer(d)
	close(c.timerReady)
	return t
}

func TestDevReadinessUsesOwnedCAAndExplicitClockDeadline(t *testing.T) {
	w, _ := testWorkflow(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" {
			t.Error("wrong health endpoint")
		}
		rw.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	ca := filepath.Join(w.work, "owned-ca.pem")
	writeTestFile(t, ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	if err := waitDevReady(context.Background(), devReadiness{URL: server.URL + "/v1/health", CA: ca, Clock: clock.NewMock()}); err != nil {
		t.Fatal(err)
	}
	// A valid CA is used above; an invalid CA never disables TLS verification.
	writeTestFile(t, ca, []byte("invalid"))
	if err := waitDevReady(context.Background(), devReadiness{URL: server.URL, CA: ca, Clock: clock.NewMock()}); err == nil {
		t.Fatal("invalid CA accepted")
	}
	c := &observedClock{MockClock: clock.NewMock(), timerReady: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- waitDevReady(context.Background(), devReadiness{URL: server.URL, CA: filepath.Join(w.work, "missing-ca"), Clock: c})
	}()
	<-c.timerReady
	c.Add(30 * time.Second)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "did not become ready") {
		t.Fatalf("deadline not enforced: %v", err)
	}
	if _, err := x509.ParseCertificate(server.Certificate().Raw); err != nil {
		t.Fatal(err)
	}
}

func TestDevResetTearsDownBeforeDeletingOwnedState(t *testing.T) {
	w, f := testWorkflow(t)
	state := filepath.Join(w.root, "build", "devenv")
	writeTestFile(t, filepath.Join(state, "devenv.yaml"), []byte("fixture manifest"))
	writeTestFile(t, filepath.Join(state, "state", "cache"), []byte("fixture state"))
	f.mutate = func(c command) {
		if c.Name == "podman" && len(c.Args) > 1 && c.Args[0] == "kube" && c.Args[1] == "down" {
			if _, err := os.Stat(filepath.Join(state, "state", "cache")); err != nil {
				t.Error("state removed before down")
			}
		}
	}
	f.fail = "podman kube down"
	if err := w.dev(context.Background(), "dev-reset"); err == nil {
		t.Fatal("failed down was ignored")
	}
	if _, err := os.Stat(state); err != nil {
		t.Fatal("failed down removed owned state/manifest")
	}
	f.fail = ""
	if err := w.dev(context.Background(), "dev-reset"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatal("reset did not remove owned state")
	}
}
