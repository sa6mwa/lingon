package session

import (
	"reflect"
	"testing"
	"time"

	"pkt.systems/lingon/internal/mvu"
)

func TestMergeSessionsSortsLocalSessionsByID(t *testing.T) {
	now := time.Now().UTC()
	runner := &Runner{
		localSessions: map[string]*localSession{
			"local-a": {id: "local-a", name: "alpha", lastActive: now.Add(-2 * time.Minute)},
			"local-b": {id: "local-b", name: "bravo", lastActive: now.Add(-1 * time.Minute)},
		},
		localOrder:  []string{"local-b", "local-a"},
		localClosed: map[string]bool{},
	}

	remote := []remoteSessionInfo{
		{ID: "local-b", Name: "remote-bravo", Status: "active", LastActiveAt: now},
		{ID: "local-a", Name: "remote-alpha", Status: "active", LastActiveAt: now.Add(-time.Second)},
	}

	got := runner.mergeSessions(remote)
	ids := make([]string, 0, len(got))
	for _, session := range got {
		ids = append(ids, session.ID)
	}
	wantIDs := []string{"local-a", "local-b"}
	if !reflect.DeepEqual(ids, wantIDs) {
		t.Fatalf("merge order mismatch: got=%v want=%v", ids, wantIDs)
	}
	if got[0].Name != "alpha" {
		t.Fatalf("local-a name mismatch: got=%q want=%q", got[0].Name, "alpha")
	}
	if got[1].Name != "bravo" {
		t.Fatalf("local-b name mismatch: got=%q want=%q", got[1].Name, "bravo")
	}
}

func TestMergeSessionsIgnoresPreviousOrderAndSortsByID(t *testing.T) {
	now := time.Now().UTC()
	runner := &Runner{
		sessionOrder: []string{"beta", "alpha"},
	}

	first := []remoteSessionInfo{
		{ID: "alpha", Name: "alpha", Status: "active", LastActiveAt: now.Add(-time.Minute)},
		{ID: "beta", Name: "beta", Status: "active", LastActiveAt: now},
	}
	got := runner.mergeSessions(first)
	if len(got) != 2 {
		t.Fatalf("first merge len=%d, want %d", len(got), 2)
	}

	second := []remoteSessionInfo{
		{ID: "beta", Name: "beta", Status: "active", LastActiveAt: now},
		{ID: "alpha", Name: "alpha", Status: "inactive", LastActiveAt: now.Add(2 * time.Minute)},
	}
	got = runner.mergeSessions(second)
	want := []string{"alpha", "beta"}
	gotIDs := make([]string, 0, len(got))
	for _, session := range got {
		gotIDs = append(gotIDs, session.ID)
	}
	if !reflect.DeepEqual(gotIDs, want) {
		t.Fatalf("session order mismatch: got=%v want=%v", gotIDs, want)
	}
}

func TestOrderSessionsSortsKnownSessionsByID(t *testing.T) {
	runner := &Runner{
		sessionOrder: []string{"remote-2", "remote-1", "remote-3"},
	}
	incoming := []remoteSessionInfo{
		{ID: "remote-1", Name: "beta", Status: "active", LastActiveAt: time.Now().Add(-time.Minute)},
		{ID: "remote-3", Name: "gamma", Status: "active", LastActiveAt: time.Now()},
		{ID: "remote-2", Name: "alpha", Status: "inactive", LastActiveAt: time.Now().Add(-2 * time.Minute)},
	}
	got := runner.orderSessions(incoming)

	want := []string{"remote-1", "remote-2", "remote-3"}
	gotIDs := make([]string, 0, len(got))
	for _, session := range got {
		gotIDs = append(gotIDs, session.ID)
	}
	if !reflect.DeepEqual(gotIDs, want) {
		t.Fatalf("ordered ids mismatch: got=%v want=%v", gotIDs, want)
	}
}

func TestOrderSessionsSortsUnseenSessionsByID(t *testing.T) {
	runner := &Runner{
		sessionOrder: []string{"remote-2", "remote-1"},
	}
	incoming := []remoteSessionInfo{
		{ID: "remote-new", Name: "new", Status: "active"},
		{ID: "remote-1", Name: "one", Status: "active"},
		{ID: "remote-2", Name: "two", Status: "active"},
		{ID: "remote-2b", Name: "other", Status: "active"},
	}
	got := runner.orderSessions(incoming)

	want := []string{"remote-1", "remote-2", "remote-2b", "remote-new"}
	gotIDs := make([]string, 0, len(got))
	for _, session := range got {
		gotIDs = append(gotIDs, session.ID)
	}
	if !reflect.DeepEqual(gotIDs, want) {
		t.Fatalf("ordered ids mismatch: got=%v want=%v", gotIDs, want)
	}
}

func TestOrderSessionsSortsFirstPayloadByID(t *testing.T) {
	runner := &Runner{}
	incoming := []remoteSessionInfo{
		{ID: "zeta", Name: "zeta", Status: "active"},
		{ID: "alpha", Name: "alpha", Status: "active"},
	}
	got := runner.orderSessions(incoming)

	want := []string{"alpha", "zeta"}
	gotIDs := make([]string, 0, len(got))
	for _, session := range got {
		gotIDs = append(gotIDs, session.ID)
	}
	if !reflect.DeepEqual(gotIDs, want) {
		t.Fatalf("ordered ids mismatch: got=%v want=%v", gotIDs, want)
	}
}

func TestNextSessionIDSkipsActiveOnDuplicateIDs(t *testing.T) {
	sessions := []remoteSessionInfo{
		{ID: "host-a"},
		{ID: "host-a"},
		{ID: "host-b"},
	}
	next := mvu.NextSessionID(mvu.SessionTabSourcesFrom(sessions), "host-a", 1)
	if next != "host-b" {
		t.Fatalf("next session id=%q, want host-b", next)
	}
}

func TestNextSessionIDUnknownActiveMovesToNext(t *testing.T) {
	sessions := []remoteSessionInfo{
		{ID: "host-a"},
		{ID: "host-b"},
		{ID: "host-c"},
	}
	next := mvu.NextSessionID(mvu.SessionTabSourcesFrom(sessions), "missing", 1)
	if next != "host-b" {
		t.Fatalf("next session id=%q, want host-b", next)
	}
}
