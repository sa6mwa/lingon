package mvu

import (
	"testing"
	"time"
)

type tabRow struct {
	id       string
	name     string
	lastSeen time.Time
}

func (r tabRow) SessionTabID() string   { return r.id }
func (r tabRow) SessionTabName() string { return r.name }
func (r tabRow) SessionTabLastActiveAt() time.Time {
	return r.lastSeen
}

func TestShortSessionID(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		{id: "", want: ""},
		{id: "host-1234", want: "1234"},
		{id: "abcdefghijk", want: "defghijk"},
		{id: "abc", want: "abc"},
		{id: "  host-xyz  ", want: "xyz"},
	}
	for _, tc := range cases {
		got := ShortSessionID(tc.id)
		if got != tc.want {
			t.Fatalf("ShortSessionID(%q): want %q got %q", tc.id, tc.want, got)
		}
	}
}

func TestSessionLabel(t *testing.T) {
	cases := []struct {
		id   string
		name string
		want string
	}{
		{id: "host-abc", name: "named", want: "named"},
		{id: "host-abc", name: "", want: "abc"},
		{id: "plainid", name: "", want: "plainid"},
		{id: "", name: "", want: ""},
	}
	for _, tc := range cases {
		got := SessionLabel(tc.id, tc.name)
		if got != tc.want {
			t.Fatalf("SessionLabel(%q, %q): want %q got %q", tc.id, tc.name, tc.want, got)
		}
	}
}

func TestSessionTabSources(t *testing.T) {
	rows := []tabRow{
		{id: "one", name: "alpha"},
		{id: "two", name: "beta"},
	}
	got := SessionTabSources(rows, func(v tabRow) string { return v.id }, func(v tabRow) string { return v.name })
	if len(got) != 2 {
		t.Fatalf("sources len=%d, want 2", len(got))
	}
	if got[0].ID != "one" || got[0].Name != "alpha" {
		t.Fatalf("unexpected first source: %+v", got[0])
	}
	if got[1].ID != "two" || got[1].Name != "beta" {
		t.Fatalf("unexpected second source: %+v", got[1])
	}
	if out := SessionTabSources([]tabRow(nil), nil, nil); out != nil {
		t.Fatalf("expected nil for empty sources")
	}
}

func TestSessionTabSourcesFrom(t *testing.T) {
	rows := []tabRow{
		{id: "one", name: "alpha"},
		{id: "two", name: "beta"},
	}
	got := SessionTabSourcesFrom(rows)
	if len(got) != 2 {
		t.Fatalf("sources len=%d, want 2", len(got))
	}
	if got[0].ID != "one" || got[0].Name != "alpha" {
		t.Fatalf("unexpected first source: %+v", got[0])
	}
	if got[1].ID != "two" || got[1].Name != "beta" {
		t.Fatalf("unexpected second source: %+v", got[1])
	}
}

func TestSortSessionsByID(t *testing.T) {
	now := time.Now().UTC()
	rows := []tabRow{
		{id: "b", name: "b", lastSeen: now.Add(-time.Minute)},
		{id: "a", name: "a", lastSeen: now},
		{id: "c", name: "c", lastSeen: now},
	}
	SortSessionsByID(rows)
	if rows[0].id != "a" {
		t.Fatalf("row[0].id=%q, want %q", rows[0].id, "a")
	}
	if rows[1].id != "b" {
		t.Fatalf("row[1].id=%q, want %q", rows[1].id, "b")
	}
	if rows[2].id != "c" {
		t.Fatalf("row[2].id=%q, want %q", rows[2].id, "c")
	}
}

func TestBuildSessionTabs(t *testing.T) {
	sessions := []SessionTabSource{
		{ID: "local-1", Name: "local"},
		{ID: "remote-2", Name: "remote"},
		{ID: "remote-3", Name: ""},
	}
	tabs, active := BuildSessionTabs(sessions, "remote-2", BuildSessionTabsOptions{
		LocalIDs: map[string]bool{"local-1": true},
		Disabled: map[string]bool{"remote-2": true},
		Muted:    map[string]bool{"remote-3": true},
	})
	if len(tabs) != 3 {
		t.Fatalf("expected 3 tabs, got %d", len(tabs))
	}
	if active != 1 {
		t.Fatalf("expected active index 1, got %d", active)
	}
	if tabs[0].Title != "*local" {
		t.Fatalf("expected local prefix, got %q", tabs[0].Title)
	}
	if !tabs[1].Disabled {
		t.Fatalf("expected disabled tab for remote-2")
	}
	if !tabs[2].Muted {
		t.Fatalf("expected muted tab for remote-3")
	}
	if tabs[2].Title == "" {
		t.Fatalf("expected fallback title for remote-3")
	}
}

func TestBuildSessionTabsActiveFallback(t *testing.T) {
	sessions := []SessionTabSource{
		{ID: "one", Name: "one"},
		{ID: "two", Name: "two"},
	}
	_, active := BuildSessionTabs(sessions, "missing", BuildSessionTabsOptions{})
	if active != 0 {
		t.Fatalf("expected active fallback to 0, got %d", active)
	}
}

func TestBuildSessionTabsEmpty(t *testing.T) {
	tabs, active := BuildSessionTabs(nil, "", BuildSessionTabsOptions{})
	if len(tabs) != 0 {
		t.Fatalf("expected no tabs")
	}
	if active != 0 {
		t.Fatalf("expected active index 0 for empty tabs")
	}
}

func TestSessionIDExists(t *testing.T) {
	sessions := []SessionTabSource{
		{ID: "one"},
		{ID: "two"},
	}
	if !SessionIDExists(sessions, "two") {
		t.Fatalf("expected two to exist")
	}
	if SessionIDExists(sessions, "missing") {
		t.Fatalf("expected missing to not exist")
	}
	if SessionIDExists(sessions, " ") {
		t.Fatalf("expected blank id to not exist")
	}
}

func TestNextSessionID(t *testing.T) {
	sessions := []SessionTabSource{
		{ID: "one"},
		{ID: "two"},
		{ID: "three"},
	}
	if got := NextSessionID(sessions, "one", 1); got != "two" {
		t.Fatalf("next from one: got %q want %q", got, "two")
	}
	if got := NextSessionID(sessions, "one", -1); got != "three" {
		t.Fatalf("prev from one: got %q want %q", got, "three")
	}
	if got := NextSessionID(sessions, "missing", 1); got != "two" {
		t.Fatalf("next from missing: got %q want %q", got, "two")
	}
	withDup := []SessionTabSource{
		{ID: "host-a"},
		{ID: "host-a"},
		{ID: "host-b"},
	}
	if got := NextSessionID(withDup, "host-a", 1); got != "host-b" {
		t.Fatalf("next from duplicate active: got %q want %q", got, "host-b")
	}
	if got := NextSessionID(nil, "one", 1); got != "" {
		t.Fatalf("next from empty: got %q want empty", got)
	}
}
