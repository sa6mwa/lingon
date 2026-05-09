package relaywire

import (
	"crypto/tls"
	"strings"

	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/tlsmgr"
)

// ClientTLSConfig builds a relay client TLS configuration from Lingon's local CA roots.
func ClientTLSConfig(tlsDir string, insecure bool) (*tls.Config, error) {
	dir := strings.TrimSpace(tlsDir)
	if dir == "" {
		dir = config.DefaultTLSDir()
	}
	pool, err := tlsmgr.LoadLocalCARoots(dir, nil)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		RootCAs:            pool,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: insecure,
	}, nil
}
