package host

import (
	"crypto/tls"

	"pkt.systems/lingon/internal/config"
	"pkt.systems/lingon/internal/tlsmgr"
)

func clientTLSConfig() (*tls.Config, error) {
	pool, err := tlsmgr.LoadLocalCARoots(config.DefaultTLSDir(), nil)
	if err != nil {
		return nil, err
	}
	return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, nil
}
