package buildscripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyReleaseCertMatchesExpectedPEM(t *testing.T) {
	t.Parallel()

	repoRoot := repoRoot(t)
	tempDir := t.TempDir()
	apkPath := filepath.Join(tempDir, "app-release.apk")
	if err := os.WriteFile(apkPath, []byte("apk"), 0o644); err != nil {
		t.Fatalf("write apk: %v", err)
	}

	expectedCert := filepath.Join(repoRoot, "android", "lingon-release-cert.pem")
	fingerprint := certificateFingerprint(t, expectedCert)
	apksignerPath := writeFakeApkSigner(t, tempDir, fingerprint)

	cmd := exec.Command(filepath.Join(repoRoot, "android", "scripts", "verify-release-cert.sh"), apkPath)
	cmd.Env = append(os.Environ(),
		"EXPECTED_CERT_PATH="+expectedCert,
		"APKSIGNER_BIN="+apksignerPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("verify script failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "Verified release APK signer certificate matches") {
		t.Fatalf("expected success message, got:\n%s", output)
	}
}

func TestVerifyReleaseCertFailsOnMismatchedSigner(t *testing.T) {
	t.Parallel()

	repoRoot := repoRoot(t)
	tempDir := t.TempDir()
	apkPath := filepath.Join(tempDir, "app-release.apk")
	if err := os.WriteFile(apkPath, []byte("apk"), 0o644); err != nil {
		t.Fatalf("write apk: %v", err)
	}

	expectedCert := filepath.Join(repoRoot, "android", "lingon-release-cert.pem")
	apksignerPath := writeFakeApkSigner(t, tempDir, "DE:AD:BE:EF")

	cmd := exec.Command(filepath.Join(repoRoot, "android", "scripts", "verify-release-cert.sh"), apkPath)
	cmd.Env = append(os.Environ(),
		"EXPECTED_CERT_PATH="+expectedCert,
		"APKSIGNER_BIN="+apksignerPath,
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected verify script to fail on mismatched signer:\n%s", output)
	}
	if !strings.Contains(string(output), "does not match") {
		t.Fatalf("expected mismatch message, got:\n%s", output)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()

	root, err := exec.Command("git", "rev-parse", "--show-toplevel").CombinedOutput()
	if err != nil {
		t.Fatalf("resolve repo root: %v\n%s", err, root)
	}
	return strings.TrimSpace(string(root))
}

func certificateFingerprint(t *testing.T, certPath string) string {
	t.Helper()

	output, err := exec.Command("openssl", "x509", "-in", certPath, "-noout", "-fingerprint", "-sha256").CombinedOutput()
	if err != nil {
		t.Fatalf("read fingerprint: %v\n%s", err, output)
	}
	const prefix = "sha256 Fingerprint="
	line := strings.TrimSpace(string(output))
	if !strings.HasPrefix(line, prefix) {
		t.Fatalf("unexpected fingerprint output: %s", line)
	}
	return strings.TrimPrefix(line, prefix)
}

func writeFakeApkSigner(t *testing.T, dir, fingerprint string) string {
	t.Helper()

	path := filepath.Join(dir, "apksigner")
	script := "#!/usr/bin/env bash\n" +
		"set -euo pipefail\n" +
		"if [[ \"$1\" != \"verify\" ]]; then\n" +
		"  echo \"unexpected command: $*\" >&2\n" +
		"  exit 1\n" +
		"fi\n" +
		"cat <<'EOF'\n" +
		"Verifies\n" +
		"Verified using v1 scheme (JAR signing): true\n" +
		"Verified using v2 scheme (APK Signature Scheme v2): true\n" +
		"Verified using v3 scheme (APK Signature Scheme v3): true\n" +
		"Signer #1 certificate DN: CN=Lingon\n" +
		"Signer #1 certificate SHA-256 digest: " + fingerprint + "\n" +
		"EOF\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake apksigner: %v", err)
	}
	return path
}
