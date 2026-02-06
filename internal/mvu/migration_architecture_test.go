package mvu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyCompositorPackageRemoved(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "compositor"))
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("read legacy compositor dir: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".go") {
			t.Fatalf("legacy compositor Go file still present: %s", entry.Name())
		}
	}
}

func TestAttachLegacyRenderWrappersRemoved(t *testing.T) {
	path := filepath.Join("..", "attach", "render.go")
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("legacy attach render wrappers still present: %s", path)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat legacy attach render wrappers: %v", err)
	}
}
