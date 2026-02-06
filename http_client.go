package lingon

import (
	"crypto/tls"
	"net/http"
	"strings"

	"pkt.systems/lingon/internal/tlsmgr"
)

func newHTTPClientWithTLSDir(tlsDir string, insecure bool) (*http.Client, error) {
	dir := strings.TrimSpace(tlsDir)
	if dir == "" {
		dir = DefaultTLSDir()
	}
	pool, err := tlsmgr.LoadLocalCARoots(dir, nil)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:            pool,
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: insecure,
		},
	}
	return &http.Client{Transport: transport}, nil
}
