package lingon

import (
	"context"
	"os"

	"pkt.systems/lingon/internal/tlsmgr"
	"pkt.systems/pslog"
)

// TLSNew creates a new CA and server certificate bundle.
func TLSNew(ctx context.Context, dir, hostname string, logger pslog.Logger) error {
	return tlsmgr.GenerateAll(ctx, dir, hostname, logger)
}

// TLSNewCA creates a new CA.
func TLSNewCA(ctx context.Context, dir string, logger pslog.Logger) error {
	return tlsmgr.GenerateCA(ctx, dir, logger)
}

// TLSNewServer creates a new server certificate signed by the existing CA.
func TLSNewServer(ctx context.Context, dir, hostname string, logger pslog.Logger) error {
	return tlsmgr.GenerateServerCert(ctx, dir, hostname, logger)
}

// TLSExportCA writes the CA certificate to the provided writer.
func TLSExportCA(dir string, output *os.File) error {
	return tlsmgr.ExportCA(dir, output)
}
