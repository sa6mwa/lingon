package sharetoken

import (
	"bytes"
	"strings"
	"testing"
)

func TestBareRoundTrip(t *testing.T) {
	random := bytes.Repeat([]byte{0xAB}, randomSize)
	token, err := EncodeBare(random)
	if err != nil {
		t.Fatalf("EncodeBare: %v", err)
	}
	parsed, err := Parse(token)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Kind != KindBare {
		t.Fatalf("Kind = %v, want %v", parsed.Kind, KindBare)
	}
	if !bytes.Equal(parsed.Random, random) {
		t.Fatalf("random mismatch")
	}
	bare, err := BareToken(parsed)
	if err != nil {
		t.Fatalf("BareToken: %v", err)
	}
	if bare != token {
		t.Fatalf("roundtrip token mismatch")
	}
}

func TestEmbeddedRoundTrip(t *testing.T) {
	random := bytes.Repeat([]byte{0x3C}, randomSize)
	endpoint := "https://example.com"
	token, err := EncodeEmbedded(random, endpoint)
	if err != nil {
		t.Fatalf("EncodeEmbedded: %v", err)
	}
	parsed, err := Parse(token)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Kind != KindEmbedded {
		t.Fatalf("Kind = %v, want %v", parsed.Kind, KindEmbedded)
	}
	if parsed.Endpoint != endpoint {
		t.Fatalf("Endpoint = %q, want %q", parsed.Endpoint, endpoint)
	}
	if !bytes.Equal(parsed.Random, random) {
		t.Fatalf("random mismatch")
	}
	bare, err := BareToken(parsed)
	if err != nil {
		t.Fatalf("BareToken: %v", err)
	}
	if !strings.HasPrefix(bare, prefixBare) {
		t.Fatalf("expected bare prefix")
	}
}

func TestParseRejectsBadChecksum(t *testing.T) {
	random := bytes.Repeat([]byte{0x55}, randomSize)
	token, err := EncodeBare(random)
	if err != nil {
		t.Fatalf("EncodeBare: %v", err)
	}
	bad := token[:len(token)-1] + "0"
	if _, err := Parse(bad); err == nil {
		t.Fatalf("expected checksum failure")
	}
}
