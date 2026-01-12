package relay

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStoreSaveLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewStore()
	session := Session{ID: "s1", Username: "test", CreatedAt: time.Now().UTC(), Status: "active"}
	store.CreateSession(session)

	token := ShareToken{Token: "token", SessionID: session.ID, Scope: ShareScopeView, CreatedAt: time.Now().UTC()}
	store.AddShareToken(token)

	if err := store.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadStore(dir)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	if _, ok := loaded.Sessions[session.ID]; !ok {
		t.Fatalf("expected session in loaded store")
	}
	if _, ok := loaded.ShareTokens[token.Token]; !ok {
		t.Fatalf("expected token in loaded store")
	}

	statePath := filepath.Join(dir, storeFilename)
	if statePath == "" {
		t.Fatalf("expected state path")
	}
}
