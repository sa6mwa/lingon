package lingon

import (
	"crypto/tls"
	"net/http"

	"pkt.systems/lingon/internal/tlsmgr"
)

func newHTTPClient() (*http.Client, error) {
	pool, err := tlsmgr.LoadLocalCARoots(DefaultTLSDir(), nil)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:    pool,
			MinVersion: tls.VersionTLS12,
		},
	}
	return &http.Client{Transport: transport}, nil
}
