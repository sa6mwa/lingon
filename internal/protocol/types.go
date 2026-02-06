package protocol

import (
	"encoding/json"
)

// MessageType identifies a protocol message.
type MessageType string

// Message types for the Lingon protocol.
const (
	MessageHello    MessageType = "hello"
	MessageWelcome  MessageType = "welcome"
	MessageSnapshot MessageType = "snapshot"
	MessageDiff     MessageType = "diff"
	MessageOut      MessageType = "out"
	MessageIn       MessageType = "in"
	MessageResize   MessageType = "resize"
	MessageControl  MessageType = "control"
	MessageError    MessageType = "error"
)

// Envelope wraps all protocol messages.
type Envelope struct {
	Type      MessageType     `json:"type"`
	SessionID string          `json:"session_id,omitempty"`
	Seq       uint64          `json:"seq,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// NewEnvelope constructs an envelope with a marshaled payload.
func NewEnvelope(msgType MessageType, sessionID string, seq uint64, payload any) (Envelope, error) {
	var raw json.RawMessage
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return Envelope{}, err
		}
		raw = data
	}
	return Envelope{Type: msgType, SessionID: sessionID, Seq: seq, Payload: raw}, nil
}

// DecodePayload unmarshals the payload into the provided struct.
func (e Envelope) DecodePayload(out any) error {
	if len(e.Payload) == 0 {
		return nil
	}
	return json.Unmarshal(e.Payload, out)
}

// HelloPayload starts a client session handshake.
type HelloPayload struct {
	ClientID     string `json:"client_id,omitempty"`
	Cols         int    `json:"cols"`
	Rows         int    `json:"rows"`
	WantsControl bool   `json:"wants_control,omitempty"`
	LastSeq      uint64 `json:"last_seq,omitempty"`
	ClientType   string `json:"client_type,omitempty"`
}

// WelcomePayload responds to a hello.
type WelcomePayload struct {
	GrantedControl bool   `json:"granted_control"`
	ServerCols     int    `json:"server_cols"`
	ServerRows     int    `json:"server_rows"`
	HolderClientID string `json:"holder_client_id,omitempty"`
}

// SnapshotPayload delivers a full terminal snapshot.
type SnapshotPayload struct {
	Encoding string          `json:"encoding"`
	State    json.RawMessage `json:"state"`
}

// DiffPayload delivers incremental updates.
type DiffPayload struct {
	Encoding string          `json:"encoding"`
	Delta    json.RawMessage `json:"delta"`
}

// StreamPayload sends raw output bytes.
type StreamPayload struct {
	Data []byte `json:"data"`
}

// InputPayload delivers input bytes from controller.
type InputPayload struct {
	Data []byte `json:"data"`
}

// ResizePayload sends terminal size updates.
type ResizePayload struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// ControlPayload announces the current controller.
type ControlPayload struct {
	HolderClientID string `json:"holder_client_id"`
}

// ErrorPayload communicates error details.
type ErrorPayload struct {
	Message string `json:"message"`
}
