package emu

import (
	"testing"
)

func TestCursorTraceReportsHomeMove(t *testing.T) {
	e := New(10, 5)
	var events []CursorTraceEvent
	e.SetCursorTrace(func(ev CursorTraceEvent) {
		events = append(events, ev)
	})

	if err := e.Write([]byte("\x1b[3;4H")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := e.Write([]byte("\x1b[H")); err != nil {
		t.Fatalf("write: %v", err)
	}

	if len(events) == 0 {
		t.Fatalf("expected cursor trace event")
	}
	last := events[len(events)-1]
	if last.Reason != "CUP" {
		t.Fatalf("expected reason CUP, got %q", last.Reason)
	}
	if last.New.X != 0 || last.New.Y != 0 {
		t.Fatalf("expected new cursor at 0,0, got %d,%d", last.New.X, last.New.Y)
	}
	if last.Screen != "main" {
		t.Fatalf("expected main screen, got %q", last.Screen)
	}
	if len(last.Recent) == 0 {
		t.Fatalf("expected recent bytes")
	}
}
