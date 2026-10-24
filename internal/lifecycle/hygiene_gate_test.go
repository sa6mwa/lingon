package lifecycle

import (
	"context"
	"strings"
	"testing"
)

func TestRepositoryHygieneStopsBeforeExpensiveProof(t *testing.T) {
	w, runner := testWorkflow(t)
	head := testGit(t, w, "rev-parse", "HEAD")
	runner.fail = "bash scripts/check-hygiene.sh"
	if err := w.prerelease(context.Background()); err == nil || !strings.Contains(err.Error(), "repository hygiene") {
		t.Fatalf("hygiene failure must stop prerelease: %v", err)
	}
	for _, call := range runner.calls {
		if call.Name == "go" || call.Name == "./gradlew" || call.Name == "podman" {
			t.Fatalf("hygiene failure started expensive work: %s %v", call.Name, call.Args)
		}
	}
	if got := testGit(t, w, "rev-parse", "HEAD"); got != head {
		t.Fatalf("failed hygiene changed HEAD: got %s, want %s", got, head)
	}
}
