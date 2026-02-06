package tlsmgr

import (
	"testing"

	"pkt.systems/lingon/internal/testutil"
)

func TestLoadLocalCARootsMissing(t *testing.T) {
	dir := testutil.TempDir(t)
	pool, err := LoadLocalCARoots(dir, nil)
	if err != nil {
		t.Fatalf("LoadLocalCARoots: %v", err)
	}
	if pool == nil {
		t.Fatalf("expected cert pool")
	}
}

func TestLoadLocalCARoots(t *testing.T) {
	dir := testutil.TempDir(t)
	if err := GenerateCA(t.Context(), dir, nil); err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	pool, err := LoadLocalCARoots(dir, nil)
	if err != nil {
		t.Fatalf("LoadLocalCARoots: %v", err)
	}
	if pool == nil {
		t.Fatalf("expected cert pool")
	}
}
