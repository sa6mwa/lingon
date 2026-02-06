package relay

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

const defaultTokenBytes = 32

func randomToken(size int) (string, error) {
	if size <= 0 {
		return "", fmt.Errorf("invalid token size: %d", size)
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("token entropy failed: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
