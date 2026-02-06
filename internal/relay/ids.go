package relay

import (
	"crypto/rand"
	"encoding/base32"
)

const connIDBytes = 12

func newConnID() string {
	buf := make([]byte, connIDBytes)
	_, _ = rand.Read(buf)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
}
