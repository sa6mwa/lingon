package session

import "testing"

func TestOSCStreamParserParsesBEL(t *testing.T) {
	parser := oscStreamParser{}
	input := []byte("abc\x1b]10;rgb:1111/2222/3333\x07def")
	var gotCode int
	var gotPayload string
	for _, b := range input {
		code, payload, raw, ok := parser.Feed(b)
		if ok {
			gotCode = code
			gotPayload = payload
			if len(raw) == 0 {
				t.Fatalf("expected raw OSC bytes")
			}
		}
	}
	if gotCode != 10 {
		t.Fatalf("expected code 10, got %d", gotCode)
	}
	if gotPayload != "rgb:1111/2222/3333" {
		t.Fatalf("expected payload rgb:1111/2222/3333, got %q", gotPayload)
	}
	if got := string(parser.FlushPassthrough()); got != "abcdef" {
		t.Fatalf("expected passthrough abcdef, got %q", got)
	}
}

func TestOSCStreamParserParsesST(t *testing.T) {
	parser := oscStreamParser{}
	input := []byte("\x1b]11;rgb:0000/0000/0000\x1b\\")
	var gotCode int
	var gotPayload string
	for _, b := range input {
		code, payload, raw, ok := parser.Feed(b)
		if ok {
			gotCode = code
			gotPayload = payload
			if len(raw) == 0 {
				t.Fatalf("expected raw OSC bytes")
			}
		}
	}
	if gotCode != 11 {
		t.Fatalf("expected code 11, got %d", gotCode)
	}
	if gotPayload != "rgb:0000/0000/0000" {
		t.Fatalf("expected payload rgb:0000/0000/0000, got %q", gotPayload)
	}
}

func TestOSCStreamParserFlushesIncomplete(t *testing.T) {
	parser := oscStreamParser{}
	input := []byte("\x1b]10;rgb")
	for _, b := range input {
		_, _, _, _ = parser.Feed(b)
	}
	if got := parser.FlushPassthrough(); string(got) != string(input) {
		t.Fatalf("expected incomplete passthrough %q, got %q", string(input), string(got))
	}
}

func TestOSCStreamParserPassthroughUnknown(t *testing.T) {
	parser := oscStreamParser{}
	input := []byte("\x1b]2;title\x07X")
	for _, b := range input {
		code, payload, raw, ok := parser.Feed(b)
		if ok {
			if code != 2 || payload != "title" {
				t.Fatalf("unexpected OSC payload: %d %q", code, payload)
			}
			parser.AddPassthrough(raw)
		}
	}
	if got := parser.FlushPassthrough(); string(got) != string(input) {
		t.Fatalf("expected passthrough %q, got %q", string(input), string(got))
	}
}
