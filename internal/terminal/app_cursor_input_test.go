package terminal

import "testing"

func TestTranslateAppCursorKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		active bool
		in     []byte
		want   []byte
	}{
		{name: "inactive passthrough", in: []byte{0x1b, '[', 'B'}, want: []byte{0x1b, '[', 'B'}},
		{name: "down arrow", active: true, in: []byte{0x1b, '[', 'B'}, want: []byte{0x1b, 'O', 'B'}},
		{name: "up arrow", active: true, in: []byte{0x1b, '[', 'A'}, want: []byte{0x1b, 'O', 'A'}},
		{name: "home", active: true, in: []byte{0x1b, '[', 'H'}, want: []byte{0x1b, 'O', 'H'}},
		{name: "end", active: true, in: []byte{0x1b, '[', 'F'}, want: []byte{0x1b, 'O', 'F'}},
		{name: "modified arrow unchanged", active: true, in: []byte{0x1b, '[', '1', ';', '2', 'B'}, want: []byte{0x1b, '[', '1', ';', '2', 'B'}},
		{name: "lone escape unchanged", active: true, in: []byte{0x1b}, want: []byte{0x1b}},
		{name: "incomplete csi unchanged", active: true, in: []byte{0x1b, '['}, want: []byte{0x1b, '['}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := TranslateAppCursorKeys(tt.in, tt.active)
			if string(got) != string(tt.want) {
				t.Fatalf("TranslateAppCursorKeys(%q, %v) = %q, want %q", tt.in, tt.active, got, tt.want)
			}
		})
	}
}
