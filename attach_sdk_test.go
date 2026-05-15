package lingon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListSessionsSortsByID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions" {
			t.Fatalf("path=%q, want /sessions", r.URL.Path)
		}
		if err := json.NewEncoder(w).Encode([]Session{
			{ID: "session-c", Name: "Charlie"},
			{ID: "session-a", Name: "Alpha"},
			{ID: "session-b", Name: "Bravo"},
		}); err != nil {
			t.Fatalf("encode sessions: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	sessions, err := ListSessions(context.Background(), server.URL, "token")
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	got := make([]string, 0, len(sessions))
	for _, session := range sessions {
		got = append(got, session.ID)
	}
	want := []string{"session-a", "session-b", "session-c"}
	if len(got) != len(want) {
		t.Fatalf("session ids=%v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("session ids=%v, want %v", got, want)
		}
	}
}
