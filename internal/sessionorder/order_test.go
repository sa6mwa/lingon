package sessionorder

import "testing"

func TestLessUsesNameThenID(t *testing.T) {
	if !Less("Alpha", "session-c", "Bravo", "session-a") {
		t.Fatalf("expected Alpha/session-c to sort before Bravo/session-a")
	}
	if !Less("Same", "session-a", "Same", "session-b") {
		t.Fatalf("expected equal names to sort by id")
	}
	if !Less("", "Aardvark", "Bravo", "session-b") {
		t.Fatalf("expected empty name to fall back to id")
	}
	if Less("Charlie", "session-a", "Bravo", "session-b") {
		t.Fatalf("did not expect Charlie/session-a to sort before Bravo/session-b")
	}
}
