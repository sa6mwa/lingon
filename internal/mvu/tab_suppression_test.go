package mvu

import "testing"

func TestSessionTabSuppression(t *testing.T) {
	var s SessionTabSuppression
	if s.Active("a") {
		t.Fatalf("expected inactive by default")
	}
	s.Set("a", true)
	if !s.Active("a") {
		t.Fatalf("expected active for session a")
	}
	if s.Active("b") {
		t.Fatalf("expected inactive for session b")
	}
	s.Set("a", false)
	if s.Active("a") {
		t.Fatalf("expected cleared for session a")
	}
}

func TestCursorTabSuppression(t *testing.T) {
	var s CursorTabSuppression
	s.Start()
	if !s.Resolve(2) {
		t.Fatalf("expected active before reaching top row")
	}
	if !s.Resolve(1) {
		t.Fatalf("expected active on top row")
	}
	if s.Resolve(2) {
		t.Fatalf("expected suppression to clear after leaving top row")
	}
	if s.Resolve(2) {
		t.Fatalf("expected suppression to remain inactive")
	}
	s.Start()
	s.Stop()
	if s.Resolve(1) {
		t.Fatalf("expected inactive after stop")
	}
}
