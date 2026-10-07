package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const registryImage = "docker.io/pktsystems/lingon"

func (w *workflow) rootlessPodman(ctx context.Context) error {
	out, err := w.exec(ctx, w.root, "podman", "info", "--format", "{{.Host.Security.Rootless}}")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) != "true" {
		return fmt.Errorf("rootless Podman is required; do not run the lifecycle with sudo")
	}
	return nil
}

func (w *workflow) verificationPodman(ctx context.Context) error {
	if err := w.rootlessPodman(ctx); err != nil {
		return err
	}
	out, err := w.exec(ctx, w.root, "podman", "info", "--format", "{{.Host.CgroupManager}}")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) != "systemd" {
		return fmt.Errorf("podman verification requires the systemd cgroup manager to join the shared resource budget")
	}
	return nil
}

func (w *workflow) buildContainer(ctx context.Context, r *receipt, epoch string) error {
	if err := w.verificationPodman(ctx); err != nil {
		return err
	}
	// Resolve the certificate stage once and record immutable provenance. The
	// stage runs on native amd64; final scratch images contain cross-built Go
	// executables and require no QEMU or target-native RUN instructions.
	if _, err := w.exec(ctx, w.root, "podman", "pull", "--arch="+runtime.GOARCH, "docker.io/library/alpine:3.23"); err != nil {
		return err
	}
	out, err := w.exec(ctx, w.root, "podman", "image", "inspect", "docker.io/library/alpine:3.23", "--format", "{{index .RepoDigests 0}}")
	if err != nil {
		return err
	}
	r.BaseImage = strings.TrimSpace(string(out))
	if !strings.HasPrefix(r.BaseImage, "docker.io/library/alpine@sha256:") {
		return fmt.Errorf("certificate base image must resolve to a registry digest")
	}
	r.Image = registryImage + ":v" + r.Version
	if r.Version == "0.0.0" {
		r.Image = "localhost/lingon-rehearsal:" + r.Commit
		// Only rehearsal state can be replaced. Release versions never can.
		if _, err := w.exec(ctx, w.root, "podman", "manifest", "exists", r.Image); err == nil {
			if _, err := w.exec(ctx, w.root, "podman", "manifest", "rm", r.Image); err != nil {
				return err
			}
		}
	}
	if _, err := w.exec(ctx, w.root, "podman", "manifest", "create", r.Image); err != nil {
		return err
	}
	for _, arch := range []string{"amd64", "arm64"} {
		work, err := os.MkdirTemp(w.work, "container-")
		if err != nil {
			return err
		}
		buildErr := func() error {
			defer os.RemoveAll(work)
			if err := copyFile(filepath.Join(w.root, "Containerfile"), filepath.Join(work, "Containerfile")); err != nil {
				return err
			}
			if err := copyFile(filepath.Join(w.work, "targets", "linux-"+arch, "lingon"), filepath.Join(work, "lingon")); err != nil {
				return err
			}
			if err := os.Chmod(filepath.Join(work, "lingon"), 0o755); err != nil {
				return err
			}
			_, err := w.exec(ctx, w.root, "podman", "build", "--cgroup-parent=lingonverification.slice", "--platform=linux/"+arch, "--pull=never", "--timestamp="+epoch,
				"--manifest="+r.Image, "--label=org.opencontainers.image.version=v"+r.Version,
				"--label=org.opencontainers.image.revision="+r.Commit,
				"--build-arg=CERTS_IMAGE="+r.BaseImage, "--build-arg=CERTS_PLATFORM=linux/"+runtime.GOARCH, "--build-arg=LINGON_BIN=lingon", "-f", filepath.Join(work, "Containerfile"), work)
			return err
		}()
		if buildErr != nil {
			return buildErr
		}
	}
	out, err = w.exec(ctx, w.root, "podman", "manifest", "inspect", r.Image)
	if err != nil {
		return err
	}
	if err := inspectImageManifest(out); err != nil {
		return err
	}
	r.ImageID = manifestHash(out)
	// Real runtime smoke is only for the native architecture. The arm64 image
	// is inspected, not falsely reported as executed on amd64.
	out, err = w.exec(ctx, w.root, "podman", "run", "--cgroup-parent=lingonverification.slice", "--rm", "--network=none", "--platform=linux/"+runtime.GOARCH, r.Image, "version", "--semver")
	if err != nil || strings.TrimSpace(string(out)) != r.Version {
		return fmt.Errorf("native container version smoke failed")
	}
	return nil
}

func inspectImageManifest(data []byte) error {
	var list struct {
		Manifests []struct {
			Digest   string
			Platform struct{ OS, Architecture string }
		}
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, m := range list.Manifests {
		if m.Platform.OS != "linux" || (m.Platform.Architecture != "amd64" && m.Platform.Architecture != "arm64") || seen[m.Platform.Architecture] || !strings.HasPrefix(m.Digest, "sha256:") {
			return fmt.Errorf("invalid release container manifest platform or digest")
		}
		seen[m.Platform.Architecture] = true
	}
	if len(seen) != 2 {
		return fmt.Errorf("container manifest must contain Linux amd64 and arm64")
	}
	return nil
}

func manifestHash(data []byte) string {
	// Ignore Podman's pretty-printing while binding all manifest contents.
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return ""
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

func (w *workflow) verifyContainer(ctx context.Context, r receipt) error {
	out, err := w.exec(ctx, w.root, "podman", "manifest", "inspect", r.Image)
	if err != nil {
		return err
	}
	if err := inspectImageManifest(out); err != nil {
		return err
	}
	if manifestHash(out) != r.ImageID {
		return fmt.Errorf("local container manifest changed; never rebuild a pushed release")
	}
	return nil
}

func (w *workflow) container(ctx context.Context) error {
	r, err := w.verified(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Verified existing container %s (%s); release-matrix builds it.\n", r.Image, r.ImageID)
	return nil
}
