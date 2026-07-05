package testpty

import "testing"

func TestSanitizeChildOutputEscapesTerminalControls(t *testing.T) {
	got := sanitizeChildOutput([]byte("ok\x1b[2J\r\nnext\x00"))
	want := "ok\\x1b[2J\\x0d\nnext\\x00"
	if got != want {
		t.Fatalf("sanitizeChildOutput() = %q, want %q", got, want)
	}
}

func TestBoundedTailBufferKeepsOnlyTail(t *testing.T) {
	buf := newBoundedTailBuffer(5)
	if _, err := buf.Write([]byte("abc")); err != nil {
		t.Fatalf("Write abc: %v", err)
	}
	if _, err := buf.Write([]byte("defgh")); err != nil {
		t.Fatalf("Write defgh: %v", err)
	}
	if got, want := string(buf.Bytes()), "defgh"; got != want {
		t.Fatalf("Bytes() = %q, want %q", got, want)
	}
	if _, err := buf.Write([]byte("ijklmnop")); err != nil {
		t.Fatalf("Write ijklmnop: %v", err)
	}
	if got, want := string(buf.Bytes()), "lmnop"; got != want {
		t.Fatalf("Bytes() = %q, want %q", got, want)
	}
}
