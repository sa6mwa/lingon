package attach

import (
	"os"
	"strings"
	"testing"
)

func TestMultiTabModelDelegatesToMVU(t *testing.T) {
	data, err := os.ReadFile("multi.go")
	if err != nil {
		t.Fatalf("read multi.go: %v", err)
	}
	src := string(data)
	if !strings.Contains(src, ".ApplyAction(") || !strings.Contains(src, "mvu.SessionTabsAction{") {
		t.Fatalf("expected attach multi tab model to delegate to mvu.SessionTabsAction via ApplyAction")
	}
	if !strings.Contains(src, "SessionTabSourcesFrom(") {
		t.Fatalf("expected attach multi tab model to use mvu.SessionTabSourcesFrom")
	}
	if !strings.Contains(src, "mvu.SessionLabel(") {
		t.Fatalf("expected attach multi session labels to delegate to mvu.SessionLabel")
	}
	forbidden := []string{
		"func shortSessionID(",
		"func sortSessions(",
		"SessionTabSources(",
	}
	for _, needle := range forbidden {
		if strings.Contains(src, needle) {
			t.Fatalf("found legacy local tab-model helper: %q", needle)
		}
	}
}
