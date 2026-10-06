package attach

import (
	"bytes"
	"testing"
)

func TestMouseInputFilterPreservesKeysAcrossEveryReadBoundary(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"ordinary\r", "ordinary\r"},
		{"\x1b[A\x1bOB\x1b[?1h", "\x1b[A\x1bOB\x1b[?1h"},
		{"a\x1b[<0;26;16M\x1b[<0;26;16mb\r", "ab\r"},
		{"\x1b[<64;1;1M\x1b[<65;1;1m", ""},
		{"\x1b[<0;26;\necho ready\r", "\necho ready\r"},
	} {
		for cut := 0; cut <= len(tc.input); cut++ {
			var filter mouseInputFilter
			out := append([]byte{}, filter.feed([]byte(tc.input[:cut]))...)
			out = append(out, filter.feed([]byte(tc.input[cut:]))...)
			out = append(out, filter.flush()...)
			if string(out) != tc.want {
				t.Fatalf("cut %d: %q became %q, want %q", cut, tc.input, out, tc.want)
			}
		}
		var filter mouseInputFilter
		var out []byte
		for _, b := range []byte(tc.input) {
			out = append(out, filter.feed([]byte{b})...)
		}
		out = append(out, filter.flush()...)
		if string(out) != tc.want {
			t.Fatalf("single-byte reads: %q became %q", tc.input, out)
		}
	}
}

func TestMouseInputFilterFlushesOnlyAmbiguousEscapePrefixes(t *testing.T) {
	for _, prefix := range []string{"\x1b", "\x1b["} {
		var filter mouseInputFilter
		if len(filter.feed([]byte(prefix))) != 0 || !filter.ambiguous() || string(filter.flush()) != prefix {
			t.Fatalf("standalone prefix %q was lost", prefix)
		}
	}
	var filter mouseInputFilter
	filter.feed([]byte("\x1b[<0;"))
	if filter.ambiguous() || len(filter.flush()) != 0 || len(filter.feed([]byte("1;1M"))) != 0 {
		t.Fatal("fragmented mouse parameters escaped to the shell")
	}
}

func BenchmarkMouseInputFilterOrdinaryInput(b *testing.B) {
	data := bytes.Repeat([]byte("ordinary input "), 128)
	var filter mouseInputFilter
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		if len(filter.feed(data)) != len(data) {
			b.Fatal("lost normal input")
		}
	}
}
