package session

import (
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
)

func TestFilterOuterOSCConsumesPendingResponses(t *testing.T) {
	clk := clock.NewMock()
	r := &Runner{clock: clk}
	r.outerOscPending = map[int]bool{10: true}
	r.outerOscDeadline = clk.Now().Add(time.Second)

	out := r.filterOuterOSC([]byte("\x1b]10;rgb:1111/2222/3333\x07"))
	if len(out) != 0 {
		t.Fatalf("expected OSC response to be consumed, got %q", string(out))
	}
	if r.outerDefaultFg != "rgb:1111/2222/3333" {
		t.Fatalf("expected default fg to update, got %q", r.outerDefaultFg)
	}
}

func TestFilterOuterOSCConsumesDuringGrace(t *testing.T) {
	clk := clock.NewMock()
	r := &Runner{clock: clk}
	now := clk.Now()
	r.outerOscGraceUntil = now.Add(time.Second)

	out := r.filterOuterOSC([]byte("\x1b]11;rgb:0000/0000/0000\x07"))
	if len(out) != 0 {
		t.Fatalf("expected OSC response to be consumed during grace, got %q", string(out))
	}
	if r.outerDefaultBg != "rgb:0000/0000/0000" {
		t.Fatalf("expected default bg to update, got %q", r.outerDefaultBg)
	}
}

func TestFilterOuterOSCPassesThroughNonOSC(t *testing.T) {
	clk := clock.NewMock()
	r := &Runner{clock: clk}
	now := clk.Now()
	r.outerOscGraceUntil = now.Add(time.Second)

	out := r.filterOuterOSC([]byte("abc\x1b]10;rgb:bbbb/bbbb/bbbb\x07def"))
	if string(out) != "abcdef" {
		t.Fatalf("expected passthrough abcdef, got %q", string(out))
	}
}

func TestFilterOuterOSCConsumesPendingWithDoubledESC(t *testing.T) {
	clk := clock.NewMock()
	r := &Runner{clock: clk}
	r.outerOscPending = map[int]bool{10: true}
	r.outerOscDeadline = clk.Now().Add(time.Second)

	// Repro for tmux-style doubled ESC before OSC.
	// We should consume this as an OSC 10 response instead of leaking "]10;rgb..."
	// into the shell input stream.
	out := r.filterOuterOSC([]byte("\x1b\x1b]10;rgb:1111/2222/3333\x07"))
	if len(out) != 0 {
		t.Fatalf("expected doubled-ESC OSC response to be consumed, got %q", string(out))
	}
	if r.outerDefaultFg != "rgb:1111/2222/3333" {
		t.Fatalf("expected default fg to update, got %q", r.outerDefaultFg)
	}
}

func TestFilterOuterOSCConsumesPendingWithDoubledESCSplitChunks(t *testing.T) {
	clk := clock.NewMock()
	r := &Runner{clock: clk}
	r.outerOscPending = map[int]bool{11: true}
	r.outerOscDeadline = clk.Now().Add(time.Second)

	out1 := r.filterOuterOSC([]byte("\x1b"))
	if len(out1) != 0 {
		t.Fatalf("expected no passthrough from first fragment, got %q", string(out1))
	}
	out2 := r.filterOuterOSC([]byte("\x1b]11;rgb:0000/0000/0000\x07"))
	if len(out2) != 0 {
		t.Fatalf("expected doubled-ESC OSC response to be consumed across fragments, got %q", string(out2))
	}
	if r.outerDefaultBg != "rgb:0000/0000/0000" {
		t.Fatalf("expected default bg to update, got %q", r.outerDefaultBg)
	}
}

func TestFilterOuterOSCConsumesLateOSCWithoutPendingOrGrace(t *testing.T) {
	clk := clock.NewMock()
	r := &Runner{clock: clk}

	in := []byte("\x1b]10;rgb:b7b7/b7b7/b7b7\x07\x1b]11;rgb:0000/0000/0000\x07")
	out := r.filterOuterOSC(in)
	if string(out) != string(in) {
		t.Fatalf("expected late OSC data to pass through without pending/grace, got %q", string(out))
	}
	if r.outerDefaultFg != "" || r.outerDefaultBg != "" {
		t.Fatalf("expected no outer default updates without pending/grace, fg=%q bg=%q", r.outerDefaultFg, r.outerDefaultBg)
	}
}

func TestFilterOuterOSCConsumesLateOSCSplitChunksWithoutPendingOrGrace(t *testing.T) {
	clk := clock.NewMock()
	r := &Runner{clock: clk}

	out1 := r.filterOuterOSC([]byte("\x1b]10;rgb:b7b7/b7"))
	if string(out1) != "\x1b]10;rgb:b7b7/b7" {
		t.Fatalf("expected first fragment passthrough without pending/grace, got %q", string(out1))
	}
	out2 := r.filterOuterOSC([]byte("b7/b7b7\x07\x1b]11;rgb:0000/0000/0000\x07"))
	if string(out2) != "b7/b7b7\x07\x1b]11;rgb:0000/0000/0000\x07" {
		t.Fatalf("expected second fragment passthrough without pending/grace, got %q", string(out2))
	}
	if r.outerDefaultFg != "" || r.outerDefaultBg != "" {
		t.Fatalf("expected no outer default updates without pending/grace, fg=%q bg=%q", r.outerDefaultFg, r.outerDefaultBg)
	}
}

func TestFilterOuterOSCPassesThroughStandaloneESCWithoutPendingOrGrace(t *testing.T) {
	clk := clock.NewMock()
	r := &Runner{clock: clk}
	out := r.filterOuterOSC([]byte{0x1b})
	if string(out) != "\x1b" {
		t.Fatalf("expected standalone ESC passthrough, got %q", string(out))
	}
}
