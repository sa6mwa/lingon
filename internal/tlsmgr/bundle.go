package tlsmgr

import (
	"crypto/tls"
	"encoding/pem"
	"fmt"
	"os"
)

// LoadBundle loads the first certificate chain and key from one or more PEM files.
func LoadBundle(files []string) (tls.Certificate, error) {
	var certBlocks [][]byte
	var keyBlock *pem.Block

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return tls.Certificate{}, err
		}
		for {
			var block *pem.Block
			block, data = pem.Decode(data)
			if block == nil {
				break
			}
			switch block.Type {
			case "CERTIFICATE":
				certBlocks = append(certBlocks, pem.EncodeToMemory(block))
			case "PRIVATE KEY", "RSA PRIVATE KEY", "EC PRIVATE KEY":
				if keyBlock == nil {
					keyBlock = block
				}
			}
		}
	}

	if len(certBlocks) == 0 {
		return tls.Certificate{}, fmt.Errorf("no certificates found in tls bundle")
	}
	if keyBlock == nil {
		return tls.Certificate{}, fmt.Errorf("no private key found in tls bundle")
	}

	certPEM := []byte{}
	for _, block := range certBlocks {
		certPEM = append(certPEM, block...)
	}
	keyPEM := pem.EncodeToMemory(keyBlock)

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, err
	}
	return cert, nil
}
