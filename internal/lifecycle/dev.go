package lifecycle

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pkt.systems/lingon/internal/clock"
)

func (w *workflow) dev(ctx context.Context, operation string) error {
	if err := w.rootlessPodman(ctx); err != nil {
		return err
	}
	root := filepath.Join(w.root, "build", "devenv")
	manifest := filepath.Join(root, "devenv.yaml")
	sum := sha256.Sum256([]byte(w.root))
	id := fmt.Sprintf("lingon-%x", sum)[:19]
	if operation == "dev-up" {
		if _, err := os.Stat(manifest); err == nil {
			return fmt.Errorf("existing dev manifest; run dev-down before changing the service")
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(filepath.Join(root, "state"), 0o700); err != nil {
			return err
		}
		port := os.Getenv("LINGON_DEV_PORT")
		if port == "" {
			port = strconv.Itoa(20000 + (int(sum[0])*256+int(sum[1]))%20000)
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 1024 || n > 65535 {
			return fmt.Errorf("LINGON_DEV_PORT must be an unprivileged TCP port")
		}
		image := os.Getenv("LINGON_DEV_IMAGE")
		if image == "" || strings.ContainsAny(image, "\r\n\"'") {
			return fmt.Errorf("dev-up requires LINGON_DEV_IMAGE=<verified version or digest>")
		}
		data, err := os.ReadFile(filepath.Join(w.root, "devenv.yaml.in"))
		if err != nil {
			return err
		}
		// Quote every substituted YAML scalar. Never expand arbitrary env vars.
		rendered := strings.NewReplacer("@POD@", strconv.Quote(id), "@IMAGE@", strconv.Quote(image), "@STATE@", strconv.Quote(filepath.Join(root, "state")), "@PORT@", port, "@UID@", strconv.Itoa(os.Getuid()), "@GID@", strconv.Itoa(os.Getgid())).Replace(string(data))
		if err := os.WriteFile(manifest, []byte(rendered), 0o600); err != nil {
			return err
		}
		if _, err := w.exec(ctx, w.root, "podman", "kube", "play", manifest); err != nil {
			return fmt.Errorf("kube play failed; manifest retained for dev-down: %w", err)
		}
		if err := waitDevReady(ctx, devReadiness{URL: "https://127.0.0.1:" + port + "/v1/health", CA: filepath.Join(root, "state", ".lingon", "tls", "ca.pem"), Clock: clock.New()}); err != nil {
			_, downErr := w.exec(ctx, w.root, "podman", "kube", "down", manifest)
			return fmt.Errorf("dev readiness failed: %w; cleanup result: %v (manifest retained)", err, downErr)
		}
		fmt.Printf("Pod %s ready at https://127.0.0.1:%s/v1/health. State: %s\n", id, port, filepath.Join(root, "state"))
		return nil
	}
	switch operation {
	case "dev-ps":
		out, err := w.exec(ctx, w.root, "podman", "pod", "ps", "--filter", "name="+id)
		fmt.Print(string(out))
		return err
	case "dev-logs":
		out, err := w.exec(ctx, w.root, "podman", "pod", "logs", id)
		fmt.Print(string(out))
		return err
	case "dev-down", "dev-reset":
		if _, err := os.Stat(manifest); err == nil {
			if _, err := w.exec(ctx, w.root, "podman", "kube", "down", manifest); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if operation == "dev-reset" {
			return os.RemoveAll(root) // after down, by the same unprivileged user
		}
		if err := os.Remove(manifest); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return fmt.Errorf("unknown dev operation")
}

type devReadiness struct {
	URL, CA string
	Clock   clock.Clock
}

func waitDevReady(ctx context.Context, in devReadiness) error {
	deadline := in.Clock.NewTimer(30 * time.Second)
	defer deadline.Stop()
	for {
		if pem, err := os.ReadFile(in.CA); err == nil {
			roots := x509.NewCertPool()
			if !roots.AppendCertsFromPEM(pem) {
				return fmt.Errorf("invalid owned development CA")
			}
			transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
			client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, in.URL, nil)
			if err != nil {
				return err
			}
			resp, err := client.Do(req)
			if err == nil {
				_ = resp.Body.Close()
				transport.CloseIdleConnections()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
			transport.CloseIdleConnections()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("service did not become ready")
		case <-in.Clock.After(500 * time.Millisecond):
		}
	}
}
