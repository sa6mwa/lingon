package tlsmgr

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pkt.systems/pslog"
)

const (
	caCertFilename     = "ca.pem"
	caKeyFilename      = "ca.key"
	serverCertFilename = "server.pem"
	serverKeyFilename  = "server.key"
)

// EnsureLocalServerCert loads server certs if present, otherwise generates
// a local CA and server cert under the provided directory.
func EnsureLocalServerCert(ctx context.Context, dir, hostname string, logger pslog.Logger) (tls.Certificate, error) {
	logger = ensureLogger(logger)

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return tls.Certificate{}, err
	}

	if exists, err := serverCertExists(dir); err != nil {
		return tls.Certificate{}, err
	} else if !exists {
		if err := ensureCA(dir, logger); err != nil {
			return tls.Certificate{}, err
		}
		if err := generateServerCert(dir, hostname, logger, true); err != nil {
			return tls.Certificate{}, err
		}
	}

	cert, err := LoadLocalServerCert(dir)
	if err != nil {
		return tls.Certificate{}, err
	}
	return cert, nil
}

// GenerateAll creates a new CA and server cert, failing if any exist.
func GenerateAll(ctx context.Context, dir, hostname string, logger pslog.Logger) error {
	logger = ensureLogger(logger)

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	if exists, err := caExists(dir); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("ca already exists in %s", dir)
	}
	if exists, err := serverCertExists(dir); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("server cert already exists in %s", dir)
	}

	if err := generateCA(dir, logger); err != nil {
		return err
	}
	if err := generateServerCert(dir, hostname, logger, false); err != nil {
		return err
	}
	return nil
}

// GenerateCA creates a new CA, failing if one already exists.
func GenerateCA(ctx context.Context, dir string, logger pslog.Logger) error {
	logger = ensureLogger(logger)

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if exists, err := caExists(dir); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("ca already exists in %s", dir)
	}
	return generateCA(dir, logger)
}

// GenerateServerCert creates a new server cert signed by the existing CA.
func GenerateServerCert(ctx context.Context, dir, hostname string, logger pslog.Logger) error {
	logger = ensureLogger(logger)

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if exists, err := serverCertExists(dir); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("server cert already exists in %s", dir)
	}
	if exists, err := caExists(dir); err != nil {
		return err
	} else if !exists {
		return fmt.Errorf("ca is missing; generate it first with 'lingon tls new ca'")
	}
	return generateServerCert(dir, hostname, logger, false)
}

// LoadLocalServerCert loads the server certificate from disk.
func LoadLocalServerCert(dir string) (tls.Certificate, error) {
	certPath := filepath.Join(dir, serverCertFilename)
	keyPath := filepath.Join(dir, serverKeyFilename)
	return tls.LoadX509KeyPair(certPath, keyPath)
}

// LoadCA reads the CA cert from disk.
func LoadCA(dir string) ([]byte, error) {
	certPath := filepath.Join(dir, caCertFilename)
	return os.ReadFile(certPath)
}

// ExportCA exports the CA certificate to a writer.
func ExportCA(dir string, output *os.File) error {
	data, err := LoadCA(dir)
	if err != nil {
		return wrapMissing(err, "ca cert not found")
	}
	if _, err := output.Write(data); err != nil {
		return err
	}
	return nil
}

func ensureCA(dir string, logger pslog.Logger) error {
	if exists, err := caExists(dir); err != nil {
		return err
	} else if exists {
		return nil
	}
	return generateCA(dir, logger)
}

func generateCA(dir string, logger pslog.Logger) error {
	serial, err := randSerial()
	if err != nil {
		return err
	}
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return err
	}
	certificate := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: "Lingon Local CA",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}

	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		return err
	}

	if err := writePEMFile(filepath.Join(dir, caCertFilename), "CERTIFICATE", der, 0o600); err != nil {
		return err
	}

	keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	if err := writePEMFile(filepath.Join(dir, caKeyFilename), "PRIVATE KEY", keyBytes, 0o600); err != nil {
		return err
	}

	logger.Info("generated ca", "cert", filepath.Join(dir, caCertFilename))
	return nil
}

func generateServerCert(dir, hostname string, logger pslog.Logger, allowMissingCA bool) error {
	caCert, caKey, err := loadCAKeypair(dir)
	if err != nil {
		if allowMissingCA {
			return err
		}
		return err
	}

	serial, err := randSerial()
	if err != nil {
		return err
	}
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return err
	}

	dnsNames, ipAddresses := sanForHostname(hostname)
	commonName := "localhost"
	if hostname != "" {
		commonName = hostname
	}

	certificate := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: commonName,
		},
		NotBefore:   time.Now().Add(-1 * time.Hour),
		NotAfter:    time.Now().AddDate(2, 0, 0),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    dnsNames,
		IPAddresses: ipAddresses,
	}

	der, err := x509.CreateCertificate(rand.Reader, certificate, caCert, &key.PublicKey, caKey)
	if err != nil {
		return err
	}

	if err := writePEMFile(filepath.Join(dir, serverCertFilename), "CERTIFICATE", der, 0o600); err != nil {
		return err
	}

	keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	if err := writePEMFile(filepath.Join(dir, serverKeyFilename), "PRIVATE KEY", keyBytes, 0o600); err != nil {
		return err
	}

	logger.Info("generated server cert", "cert", filepath.Join(dir, serverCertFilename))
	return nil
}

func loadCAKeypair(dir string) (*x509.Certificate, *rsa.PrivateKey, error) {
	certPath := filepath.Join(dir, caCertFilename)
	keyPath := filepath.Join(dir, caKeyFilename)
	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		return nil, nil, wrapMissing(err, "ca cert not found")
	}
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, wrapMissing(err, "ca key not found")
	}

	certBlock, _ := pem.Decode(certBytes)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return nil, nil, fmt.Errorf("invalid ca cert PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}

	keyBlock, _ := pem.Decode(keyBytes)
	if keyBlock == nil {
		return nil, nil, fmt.Errorf("invalid ca key PEM")
	}
	key, err := parseRSAPrivateKey(keyBlock)
	if err != nil {
		return nil, nil, err
	}

	return cert, key, nil
}

func parseRSAPrivateKey(block *pem.Block) (*rsa.PrivateKey, error) {
	switch block.Type {
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("unsupported private key type in ca key")
		}
		return rsaKey, nil
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unsupported ca key type: %s", block.Type)
	}
}

func sanForHostname(hostname string) ([]string, []net.IP) {
	trimmed := strings.TrimSpace(hostname)
	if trimmed == "" {
		return []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	}
	if ip := net.ParseIP(trimmed); ip != nil {
		return nil, []net.IP{ip}
	}
	return []string{trimmed}, nil
}

func randSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, limit)
}

func writePEMFile(path, pemType string, data []byte, perm os.FileMode) error {
	block := &pem.Block{Type: pemType, Bytes: data}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if err := pem.Encode(file, block); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return nil
}

func caExists(dir string) (bool, error) {
	certPath := filepath.Join(dir, caCertFilename)
	keyPath := filepath.Join(dir, caKeyFilename)
	return filesExist(certPath, keyPath)
}

func serverCertExists(dir string) (bool, error) {
	certPath := filepath.Join(dir, serverCertFilename)
	keyPath := filepath.Join(dir, serverKeyFilename)
	return filesExist(certPath, keyPath)
}

func filesExist(paths ...string) (bool, error) {
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, err
		}
		if info.IsDir() {
			return false, fmt.Errorf("%s is a directory", p)
		}
	}
	return true, nil
}
