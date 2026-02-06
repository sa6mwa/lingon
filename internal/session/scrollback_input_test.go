package session

import "testing"

func TestScrollbackInputMappings(t *testing.T) {
	cases := []struct {
		name string
		seq  []byte
		want scrollCommand
	}{
		{"exit", []byte("q"), scrollExit},
		{"line-down-n", []byte("n"), scrollLineDown},
		{"line-up-p", []byte("p"), scrollLineUp},
		{"pgup-w", []byte("w"), scrollPageUp},
		{"pgdn-s", []byte("s"), scrollPageDown},
		{"home-a", []byte("a"), scrollTop},
		{"end-d", []byte("d"), scrollBottom},
		{"pgup", []byte{0x1b, '[', '5', '~'}, scrollPageUp},
		{"pgdn", []byte{0x1b, '[', '6', '~'}, scrollPageDown},
		{"arrow-up", []byte{0x1b, '[', 'A'}, scrollLineUp},
		{"arrow-down", []byte{0x1b, '[', 'B'}, scrollLineDown},
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
