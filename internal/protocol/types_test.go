package protocol

import "testing"

func TestEnvelopeRoundTrip(t *testing.T) {
	payload := HelloPayload{ClientID: "client", Cols: 80, Rows: 24, WantsControl: true}
	env, err := NewEnvelope(MessageHello, "session", 42, payload)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}

	var decoded HelloPayload
	if err := env.DecodePayload(&decoded); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if decoded.ClientID != payload.ClientID || decoded.Cols != payload.Cols || decoded.Rows != payload.Rows {
		t.Fatalf("decoded payload mismatch: %+v", decoded)
	}
}
