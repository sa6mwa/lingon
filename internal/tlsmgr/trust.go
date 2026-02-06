package tlsmgr

import (
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
)

// LoadLocalCARoots loads the local CA cert into the provided pool (or a new one).
func LoadLocalCARoots(dir string, pool *x509.CertPool) (*x509.CertPool, error) {
	if pool == nil {
		systemPool, err := x509.SystemCertPool()
		if err != nil || systemPool == nil {
			pool = x509.NewCertPool()
		} else {
			pool = systemPool
		}
	}
	certPath := filepath.Join(dir, caCertFilename)
	data, err := os.ReadFile(certPath)
	if err != nil {
		if os.IsNotExist(err) {
			return pool, nil
		}
		return nil, err
	}
	if ok := pool.AppendCertsFromPEM(data); !ok {
		return nil, fmt.Errorf("failed to parse ca cert")
	}
	return pool, nil
}
