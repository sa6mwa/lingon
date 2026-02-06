package session

import (
	"testing"

	"pkt.systems/lingon/internal/terminal"
)

type inlineOriginSpy struct {
	row   int
	calls int
}

func (s *inlineOriginSpy) SetInlineOriginRow(row int) {
	s.row = row
	s.calls++
}

func (s *inlineOriginSpy) Write([]byte) error {
	return nil
}

func (s *inlineOriginSpy) Resize(int, int) {}

func (s *inlineOriginSpy) Snapshot() (terminal.Snapshot, error) {
	return terminal.Snapshot{}, nil
}

func TestRespondToTerminalQueriesSetsInlineOrigin(t *testing.T) {
	spy := &inlineOriginSpy{}
	session := &localSession{
		emulator: spy,
		cursorQuery: func(terminal.Snapshot) (int, int, bool) {
			return 2, 1, true
		},
	}
	snap := terminal.Snapshot{
		Cols:   80,
		Rows:   24,
		Cursor: terminal.Cursor{X: 0, Y: 7},
	}

	session.respondToTerminalQueries([]byte("\x1b[6n"), snap)

	if spy.calls != 1 {
		t.Fatalf("expected inline origin setter to be called once, got %d", spy.calls)
	}
	if spy.row != 7 {
		t.Fatalf("expected inline origin row 7, got %d", spy.row)
	}
}
