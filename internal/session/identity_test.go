package session

import (
	"strings"
	"sync/atomic"
	"testing"
)

func TestDefaultSessionIdentityUsesSequence(t *testing.T) {
	atomic.StoreInt64(&defaultSessionSequence, -2)
	idOne, nameOne := defaultSessionIdentity()
	idTwo, nameTwo := defaultSessionIdentity()

	if idOne != nameOne {
		t.Fatalf("expected generated id and name to match, got %q and %q", idOne, nameOne)
	}
	if idTwo != nameTwo {
		t.Fatalf("expected generated id and name to match, got %q and %q", idTwo, nameTwo)
	}
	if !strings.HasSuffix(idOne, "-1") {
		t.Fatalf("expected first generated id to end in -1, got %q", idOne)
	}
	if !strings.HasSuffix(idTwo, "-0") {
		t.Fatalf("expected second generated id to end in -0, got %q", idTwo)
	}
	if idOne == idTwo {
		t.Fatalf("expected distinct generated ids, got duplicate %q", idOne)
	}

	parts := strings.Split(idOne, "-")
	if len(parts) != 2 {
		t.Fatalf("expected generated id with one dash, got %q", idOne)
	}
	nameBase := parts[0]
	if len(nameBase) < 8 {
		t.Fatalf("expected generated id to include 8-char host/hash base, got %q", idOne)
	}
	hashPart := nameBase[len(nameBase)-4:]
	if len(hashPart) != 4 {
		t.Fatalf("expected 4-char hash, got %q", hashPart)
	}
	for _, ch := range parts[1] {
		if !strings.ContainsRune(base62Alphabet, ch) {
			t.Fatalf("expected hash to be base62, got %q in %q", string(ch), parts[1])
		}
	}
}

func TestInitializeSessionIdentityUsesExplicitSessionIDAsDefaultName(t *testing.T) {
	runner := &Runner{
		opts: Options{
			Publish:   true,
			SessionID: "customName",
		},
	}

	runner.initializeSessionIdentity()

	if runner.opts.SessionID != "customName" {
		t.Fatalf("session id = %q, want %q", runner.opts.SessionID, "customName")
	}
	if runner.sessionName != "customName" {
		t.Fatalf("session name = %q, want %q", runner.sessionName, "customName")
	}
	if runner.sessionBase != "customName" {
		t.Fatalf("session base = %q, want %q", runner.sessionBase, "customName")
	}
}

func TestInitializeSessionIdentityKeepsExplicitSessionName(t *testing.T) {
	runner := &Runner{
		opts: Options{
			Publish:     true,
			SessionID:   "customName",
			SessionName: "display-name",
		},
	}

	runner.initializeSessionIdentity()

	if runner.opts.SessionID != "customName" {
		t.Fatalf("session id = %q, want %q", runner.opts.SessionID, "customName")
	}
	if runner.sessionName != "display-name" {
		t.Fatalf("session name = %q, want %q", runner.sessionName, "display-name")
	}
	if runner.sessionBase != "display-name" {
		t.Fatalf("session base = %q, want %q", runner.sessionBase, "display-name")
	}
}
