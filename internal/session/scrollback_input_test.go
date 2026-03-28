package session

import "testing"

func TestScrollbackInputMappings(t *testing.T) {
	cases := []struct {
		name string
		seq  []byte
		want scrollCommand
	}{
		{"exit", []byte("q"), scrollExit},
		{"line-up-k", []byte("k"), scrollLineUp},
		{"line-down-j", []byte("j"), scrollLineDown},
		{"five-up-K", []byte("K"), scrollFiveUp},
		{"five-down-J", []byte("J"), scrollFiveDown},
		{"half-up-w", []byte("w"), scrollPageUp},
		{"half-up-u", []byte("u"), scrollPageUp},
		{"half-down-s", []byte("s"), scrollPageDown},
		{"half-down-d", []byte("d"), scrollPageDown},
		{"pgup", []byte{0x1b, '[', '5', '~'}, scrollPageUp},
		{"pgdn", []byte{0x1b, '[', '6', '~'}, scrollPageDown},
		{"arrow-up", []byte{0x1b, '[', 'A'}, scrollLineUp},
		{"arrow-down", []byte{0x1b, '[', 'B'}, scrollLineDown},
		{"home-g", []byte("g"), scrollTop},
		{"end-G", []byte("G"), scrollBottom},
		{"home-tilde", []byte{0x1b, '[', '1', '~'}, scrollTop},
		{"end-tilde", []byte{0x1b, '[', '4', '~'}, scrollBottom},
		{"home-escO", []byte{0x1b, 'O', 'H'}, scrollTop},
		{"end-escO", []byte{0x1b, 'O', 'F'}, scrollBottom},
		{"mouse-wheel-up", []byte{0x1b, '[', '<', '6', '4', ';', '1', ';', '1', 'M'}, scrollWheelUp},
		{"mouse-wheel-down", []byte{0x1b, '[', '<', '6', '5', ';', '1', ';', '1', 'M'}, scrollWheelDown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var st scrollInputState
			var got scrollCommand
			for _, b := range tc.seq {
				cmd := st.feed(b)
				if cmd != scrollNone {
					got = cmd
				}
			}
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestScrollbackInputRemovesLegacyAliases(t *testing.T) {
	cases := []struct {
		name string
		seq  []byte
	}{
		{"legacy-line-down-n", []byte("n")},
		{"legacy-line-up-p", []byte("p")},
		{"legacy-top-a", []byte("a")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var st scrollInputState
			for _, b := range tc.seq {
				if got := st.feed(b); got != scrollNone {
					t.Fatalf("got %v want %v", got, scrollNone)
				}
			}
		})
	}
}
