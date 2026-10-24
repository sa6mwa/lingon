package emu

import (
	"fmt"
	"strings"
	"testing"
)

func TestAbsoluteCursorPositionScrollRegionBounds(t *testing.T) {
	cases := []struct {
		name  string
		setup string
		row   int
		col   int
		wantY int
		wantX int
	}{
		{"origin_off_below_margin", "\x1b[?6l\x1b[1;6r", 8, 1, 8, 1},
		{"origin_off_oversized", "\x1b[?6l\x1b[1;6r", 65535, 65535, 8, 40},
		{"origin_off_above_margin", "\x1b[?6l\x1b[3;6r", 1, 1, 1, 1},
		{"origin_on_top", "\x1b[3;6r\x1b[?6h", 1, 1, 3, 1},
		{"origin_on_relative", "\x1b[3;6r\x1b[?6h", 2, 7, 4, 7},
		{"origin_on_bottom", "\x1b[3;6r\x1b[?6h", 65535, 65535, 6, 40},
		{"origin_reset", "\x1b[3;6r\x1b[?6h\x1b[?6l", 8, 1, 8, 1},
		{"zero_defaults", "\x1b[?6l\x1b[3;6r", 0, 0, 1, 1},
	}
	for _, final := range []byte{'H', 'f'} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%c/%s", final, tc.name), func(t *testing.T) {
				e := New(40, 8)
				writeCursorFixture(t, e, tc.setup+fmt.Sprintf("\x1b[%d;%d%c", tc.row, tc.col, final))
				snap, err := e.Snapshot()
				if err != nil {
					t.Fatal(err)
				}
				if snap.Cursor.Y+1 != tc.wantY || snap.Cursor.X+1 != tc.wantX {
					t.Fatalf("cursor = row %d col %d, want row %d col %d", snap.Cursor.Y+1, snap.Cursor.X+1, tc.wantY, tc.wantX)
				}
			})
		}
	}
}

func TestVPAScrollRegionBoundsPreserveColumn(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mode  string
		row   int
		wantY int
	}{
		{"origin_off_below_margin", "\x1b[?6l", 8, 8},
		{"origin_off_oversized", "\x1b[?6l", 65535, 8},
		{"origin_off_above_margin", "\x1b[?6l", 1, 1},
		{"origin_on_relative", "\x1b[?6h", 2, 4},
		{"origin_on_bottom", "\x1b[?6h", 65535, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := New(40, 8)
			writeCursorFixture(t, e, "\x1b[3;6r"+tc.mode+"\x1b[7G"+fmt.Sprintf("\x1b[%dd", tc.row))
			snap, err := e.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if snap.Cursor.Y+1 != tc.wantY || snap.Cursor.X+1 != 7 {
				t.Fatalf("cursor = row %d col %d, want row %d col 7", snap.Cursor.Y+1, snap.Cursor.X+1, tc.wantY)
			}
		})
	}
}

func TestBottomAnchoredFeedPreservesSuccessiveRows(t *testing.T) {
	e := New(40, 8)
	writeCursorFixture(t, e, "\x1b[?6l")
	lines := []string{"one", "two", "three"}
	for i, line := range lines {
		writeCursorFixture(t, e, fmt.Sprintf("\x1b[1;6r\x1b[65535;1H\x1b[%dA%s\r\n\x1b[0m\x1b[65535;1H", 7-i, line))
		snap, err := e.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		for row, want := range lines[:i+1] {
			if got := strings.TrimRight(rowString(snap, row), " "); got != want {
				t.Fatalf("after %q: row %d = %q, want %q", line, row+1, got, want)
			}
		}
		// Check the feed first, so a bad anchor cannot hide the overwrite failure.
		if i == len(lines)-1 && snap.Cursor.Y != 7 {
			t.Fatalf("final anchor row = %d, want 8", snap.Cursor.Y+1)
		}
	}
}

func TestAbsoluteCursorPositionReachesReservedPromptRows(t *testing.T) {
	e := New(40, 8)
	writeCursorFixture(t, e, "\x1b[?6l\x1b[1;6r\x1b[7;1HSTATUS\x1b[8;1HPROMPT>")
	snap, err := e.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for row, want := range map[int]string{6: "STATUS", 7: "PROMPT>"} {
		if got := strings.TrimRight(rowString(snap, row), " "); got != want {
			t.Errorf("row %d = %q, want %q", row+1, got, want)
		}
	}
}

func TestAbsoluteCursorPositionPreservesInlineOffsetOutsideMargins(t *testing.T) {
	e := New(40, 8)
	e.SetInlineOriginRow(3)
	writeCursorFixture(t, e, "\x1b[?6l\x1b[1;4r\x1b[65535;1Hbottom\x1b[1;1Hinline")
	snap, err := e.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for row, want := range map[int]string{2: "inline", 7: "bottom"} {
		if got := strings.TrimRight(rowString(snap, row), " "); got != want {
			t.Errorf("row %d = %q, want %q", row+1, got, want)
		}
	}
}

func TestAbsoluteCursorPositionCancelsPendingWrapOutsideMargins(t *testing.T) {
	for _, final := range []byte{'H', 'f', 'd'} {
		t.Run(string(final), func(t *testing.T) {
			e := New(40, 8)
			position := fmt.Sprintf("\x1b[8;40%c", final)
			if final == 'd' {
				position = "\x1b[8d"
			}
			writeCursorFixture(t, e, "\x1b[?6l\x1b[1;6r\x1b[2;40HX"+position+"Y")
			snap, err := e.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if got := cellRune(snap, 39, 7); got != 'Y' {
				t.Fatalf("last cell = %q, want Y without wrapping", got)
			}
		})
	}
}

func writeCursorFixture(t *testing.T, e *Emulator, data string) {
	t.Helper()
	if err := e.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkAbsoluteCursorPositionScrollRegion(b *testing.B) {
	for _, mode := range []struct {
		name string
		set  string
	}{
		{"origin_off", "\x1b[?6l"},
		{"origin_on", "\x1b[?6h"},
	} {
		b.Run(mode.name, func(b *testing.B) {
			e := New(40, 8)
			if err := e.Write([]byte("\x1b[1;6r" + mode.set)); err != nil {
				b.Fatal(err)
			}
			data := []byte("\x1b[65535;1H\x1b[7A\x1b[2;7f\x1b[65535d")
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := e.Write(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
