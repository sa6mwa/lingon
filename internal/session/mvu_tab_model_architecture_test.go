package session

import (
	"os"
	"strings"
	"testing"
)

func TestRemoteTabModelDelegatesToMVU(t *testing.T) {
	data, err := os.ReadFile("session.go")
	if err != nil {
		t.Fatalf("read session.go: %v", err)
	}
	src := string(data)
	if !strings.Contains(src, ".ApplyAction(") || !strings.Contains(src, "mvu.SessionTabsAction{") {
		t.Fatalf("expected host tab model to delegate to mvu.SessionTabsAction via ApplyAction")
	}
	remoteData, err := os.ReadFile("remote.go")
	if err != nil {
		t.Fatalf("read remote.go: %v", err)
	}
	remoteSrc := string(remoteData)
	if !strings.Contains(remoteSrc, "mvu.SessionLabel(") {
		t.Fatalf("expected remote session labels to delegate to mvu.SessionLabel")
	}
	if !strings.Contains(src, "SessionTabSourcesFrom(") {
		t.Fatalf("expected host tab model to use mvu.SessionTabSourcesFrom")
	}
	if !strings.Contains(remoteSrc, "SessionTabSourcesFrom(") {
		t.Fatalf("expected remote manager tab model to use mvu.SessionTabSourcesFrom")
	}
	forbidden := []string{
		"func buildTabs(",
		"func shortSessionID(",
		"func sortSessions(",
		"SessionTabSources(",
	}
	for _, needle := range forbidden {
		if strings.Contains(src, needle) || strings.Contains(remoteSrc, needle) {
			t.Fatalf("found legacy local tab-model helper: %q", needle)
		}
	}
}
