package session

import (
	"os"
	"strings"
	"testing"
)

func TestRemoteManagerMVUSyncDelegation(t *testing.T) {
	data, err := os.ReadFile("remote.go")
	if err != nil {
		t.Fatalf("read remote.go: %v", err)
	}
	src := string(data)
	forbidden := []string{
		"compositor.SetClock(",
		"compositor.ShowConnected(",
		"m.compositor.ShowConnected(",
		".SyncContext(",
		".SyncStatusEffect(",
	}
	for _, needle := range forbidden {
		if strings.Contains(src, needle) {
			t.Fatalf("non-MVU remote presenter path found in remote.go: %q", needle)
		}
	}
	required := []string{
		".ApplyAction(",
		"mvu.ContextAction{",
		"mvu.StatusAction{",
	}
	for _, needle := range required {
		if !strings.Contains(src, needle) {
			t.Fatalf("expected remote manager to delegate via MVU sync api: %q", needle)
		}
	}
}
