package sharetoken

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"unicode"
)

const (
	prefixBare     = "LGB"
	prefixEmbedded = "LGE"
	versionByte    = 1
	randomSize     = 20
	maxEndpointLen = 2048
)

const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// Kind describes the share token format.
type Kind int

const (
	// KindUnknown indicates an unrecognized share token format.
	KindUnknown Kind = iota
	// KindBare identifies a bare share token without embedded endpoint.
	KindBare
	// KindEmbedded identifies a share token that embeds an endpoint.
	KindEmbedded
)

// Parsed represents a decoded share token.
type Parsed struct {
	Kind     Kind
	Version  byte
	Random   []byte
	Endpoint string
}

// NewBareToken generates a new bare share token.
func NewBareToken() (string, error) {
	random := make([]byte, randomSize)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return EncodeBare(random)
}

// EncodeBare builds a bare share token from random bytes.
func EncodeBare(random []byte) (string, error) {
	if len(random) != randomSize {
		return "", fmt.Errorf("invalid random size: %d", len(random))
	}
	payload := make([]byte, 0, 1+randomSize+2)
	payload = append(payload, versionByte)
	payload = append(payload, random...)
	crc := crc16(payload)
	payload = append(payload, byte(crc>>8), byte(crc))
	return prefixBare + encodeCrockford(payload), nil
}

// EncodeEmbedded builds an endpoint-embedded share token from random bytes.
func EncodeEmbedded(random []byte, endpoint string) (string, error) {
	if len(random) != randomSize {
		return "", fmt.Errorf("invalid random size: %d", len(random))
	}
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return "", fmt.Errorf("endpoint is required")
	}
	if len(trimmed) > maxEndpointLen {
		return "", fmt.Errorf("endpoint too long")
	}

	payload := make([]byte, 0, 1+randomSize+2+len(trimmed)+2)
	payload = append(payload, versionByte)
	payload = append(payload, random...)
	lenBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(lenBuf, uint16(len(trimmed)))
	payload = append(payload, lenBuf...)
	payload = append(payload, []byte(trimmed)...)
	crc := crc16(payload)
	payload = append(payload, byte(crc>>8), byte(crc))
	return prefixEmbedded + encodeCrockford(payload), nil
}

// Parse decodes a share token and validates its checksum.
func Parse(token string) (Parsed, error) {
	trimmed := strings.TrimSpace(token)
	if len(trimmed) < 4 {
		return Parsed{}, fmt.Errorf("token too short")
	}
	prefix := strings.ToUpper(trimmed[:3])
	body := trimmed[3:]
	switch prefix {
	case prefixBare:
		return parseBare(body)
	case prefixEmbedded:
		return parseEmbedded(body)
	default:
		return Parsed{}, fmt.Errorf("unknown token prefix")
	}
}

// BareToken returns the bare token string for a parsed share token.
func BareToken(p Parsed) (string, error) {
	if p.Kind != KindBare && p.Kind != KindEmbedded {
		return "", fmt.Errorf("invalid token kind")
	}
	return EncodeBare(p.Random)
}

func parseBare(body string) (Parsed, error) {
	raw, err := decodeCrockford(body)
	if err != nil {
		return Parsed{}, err
	}
	expected := 1 + randomSize + 2
	if len(raw) != expected {
		return Parsed{}, fmt.Errorf("invalid token payload length")
	}
	version := raw[0]
	if version != versionByte {
		return Parsed{}, fmt.Errorf("unsupported token version")
	}
	payload := raw[:1+randomSize]
	if crc16(payload) != binary.BigEndian.Uint16(raw[1+randomSize:]) {
		return Parsed{}, fmt.Errorf("invalid token checksum")
	}
	random := append([]byte(nil), raw[1:1+randomSize]...)
	return Parsed{
		Kind:    KindBare,
		Version: version,
		Random:  random,
	}, nil
}

func parseEmbedded(body string) (Parsed, error) {
	raw, err := decodeCrockford(body)
	if err != nil {
		return Parsed{}, err
	}
	minLen := 1 + randomSize + 2 + 2
	if len(raw) < minLen {
		return Parsed{}, fmt.Errorf("invalid token payload length")
	}
	version := raw[0]
	if version != versionByte {
		return Parsed{}, fmt.Errorf("unsupported token version")
	}
	random := append([]byte(nil), raw[1:1+randomSize]...)
	lenStart := 1 + randomSize
	endpointLen := int(binary.BigEndian.Uint16(raw[lenStart : lenStart+2]))
	if endpointLen <= 0 || endpointLen > maxEndpointLen {
		return Parsed{}, fmt.Errorf("invalid endpoint length")
	}
	expected := 1 + randomSize + 2 + endpointLen + 2
	if len(raw) != expected {
		return Parsed{}, fmt.Errorf("invalid token payload length")
	}
	endpointStart := lenStart + 2
	endpoint := string(raw[endpointStart : endpointStart+endpointLen])
	payload := raw[:expected-2]
	if crc16(payload) != binary.BigEndian.Uint16(raw[expected-2:]) {
		return Parsed{}, fmt.Errorf("invalid token checksum")
	}
	return Parsed{
		Kind:     KindEmbedded,
		Version:  version,
		Random:   random,
		Endpoint: endpoint,
	}, nil
}

func encodeCrockford(data []byte) string {
	enc := base32.NewEncoding(crockfordAlphabet).WithPadding(base32.NoPadding)
	return enc.EncodeToString(data)
}

func decodeCrockford(input string) ([]byte, error) {
	cleaned := sanitize(input)
	if cleaned == "" {
		return nil, fmt.Errorf("empty token payload")
	}
	enc := base32.NewEncoding(crockfordAlphabet).WithPadding(base32.NoPadding)
	dst := make([]byte, enc.DecodedLen(len(cleaned)))
	n, err := enc.Decode(dst, []byte(cleaned))
	if err != nil {
		return nil, fmt.Errorf("invalid token payload: %w", err)
	}
	return dst[:n], nil
}

func sanitize(input string) string {
	var b strings.Builder
	b.Grow(len(input))
	for _, r := range input {
		switch r {
		case '-', ' ', '\n', '\t', '\r':
			continue
		}
		switch r {
		case 'o', 'O':
			r = '0'
		case 'i', 'I', 'l', 'L':
			r = '1'
		default:
			r = unicode.ToUpper(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

func crc16(data []byte) uint16 {
	var crc uint16 = 0xFFFF
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
