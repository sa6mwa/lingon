package lifecycle

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func (w *workflow) verified(ctx context.Context) (receipt, error) {
	r, err := w.readReceipt()
	if err != nil {
		return r, err
	}
	if err := w.unchanged(ctx, r.Commit); err != nil {
		return r, err
	}
	if err := w.sameBranch(ctx, r.Branch); err != nil {
		return r, err
	}
	v, err := w.exactVersion(ctx)
	if err != nil || v != r.Version {
		return r, fmt.Errorf("receipt and exact lightweight HEAD tag disagree")
	}
	sum, err := hashFile(filepath.Join(w.dist(v), checksumName(v)))
	if err != nil || sum != r.ManifestHash {
		return r, fmt.Errorf("release manifest changed since verification")
	}
	if err := w.verifyArtifacts(ctx, r); err != nil {
		return r, err
	}
	if err := w.verifyContainer(ctx, r); err != nil {
		return r, err
	}
	return r, nil
}

func (w *workflow) verifyArtifacts(ctx context.Context, r receipt) error {
	names, err := verifyChecksums(w.dist(r.Version), r.Version)
	if err != nil {
		return err
	}
	for _, name := range names {
		path := filepath.Join(w.dist(r.Version), name)
		if strings.HasSuffix(name, ".tar.gz") {
			if err := w.inspectArchive(path, r); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		} else if strings.HasSuffix(name, ".apk") {
			if err := w.inspectAPK(ctx, path, r.Version); err != nil {
				return err
			}
			z, err := zip.OpenReader(path)
			if err != nil {
				return err
			}
			for _, file := range z.File {
				in, err := file.Open()
				if err != nil {
					_ = z.Close()
					return err
				}
				err = w.scanPrivate(in, name+":"+file.Name)
				_ = in.Close()
				if err != nil {
					_ = z.Close()
					return err
				}
			}
			_ = z.Close()
		} else {
			in, err := os.Open(path)
			if err != nil {
				return err
			}
			err = w.scanPrivate(in, name)
			_ = in.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *workflow) scanPrivate(in io.Reader, name string) error {
	forbidden := []string{w.root, w.work, "file:///private", "file:///tmp/", "/home/", "/Users/", "/.cache/go-build/", "-race=true", "-asan=true", "-msan=true"}
	// Bound memory while retaining overlap so a path crossing chunk boundaries
	// cannot escape inspection. Limit uncompressed payloads to prevent zip bombs.
	const chunk = 32 * 1024
	const overlap = 4096
	buf := make([]byte, chunk+overlap)
	keep := 0
	var total int64
	for {
		n, err := in.Read(buf[keep:])
		total += int64(n)
		if total > 512*1024*1024 {
			return fmt.Errorf("artifact entry exceeds inspection limit: %s", name)
		}
		data := buf[:keep+n]
		for _, path := range forbidden {
			if len(path) > 1 && bytes.Contains(data, []byte(path)) {
				return fmt.Errorf("private path or instrumentation in %s: %s", name, path)
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		keep = min(overlap, len(data))
		copy(buf, data[len(data)-keep:])
	}
}

func verifyStamp(path, stamp string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 32768+len(stamp))
	keep := 0
	for {
		n, err := f.Read(buf[keep:])
		data := buf[:keep+n]
		if bytes.Contains(data, []byte(stamp)) {
			return nil
		}
		if err == io.EOF {
			return fmt.Errorf("binary has no matching linked release stamp: %s", path)
		}
		if err != nil {
			return err
		}
		keep = min(len(stamp), len(data))
		copy(buf, data[len(data)-keep:])
	}
}

func (w *workflow) inspectArchive(path string, r receipt) error {
	var selected target
	found := false
	for _, t := range releaseTargets {
		if filepath.Base(path) == "lingon-"+r.Version+"-"+t.OS+"-"+t.Arch+".tar.gz" {
			selected, found = t, true
			break
		}
	}
	if !found {
		return fmt.Errorf("unknown release target archive")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	work, err := os.MkdirTemp(w.work, "verify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	prefix := strings.TrimSuffix(filepath.Base(path), ".tar.gz") + "/"
	seen := make(map[string]bool)
	expected := map[string]byte{
		"": tar.TypeDir, "bin/": tar.TypeDir, "share/": tar.TypeDir,
		"share/doc/": tar.TypeDir, "share/doc/lingon/": tar.TypeDir,
		"bin/lingon": tar.TypeReg, "bin/lingonx": tar.TypeSymlink,
		"share/doc/lingon/LICENSE": tar.TypeReg,
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(h.Name, prefix)
		kind, allowed := expected[name]
		if h.Name != prefix+name || seen[name] || !allowed {
			return fmt.Errorf("unexpected or duplicate archive entry: %s", h.Name)
		}
		seen[name] = true
		if h.Typeflag != kind {
			return fmt.Errorf("invalid archive entry type: %s", h.Name)
		}
		if kind == tar.TypeDir {
			if h.Mode != 0o755 || h.Size != 0 || h.Linkname != "" {
				return fmt.Errorf("invalid archive directory: %s", h.Name)
			}
			if err := os.MkdirAll(filepath.Join(work, name), 0o755); err != nil {
				return err
			}
			continue
		}
		if name == "bin/lingonx" {
			if h.Linkname != "lingon" || h.Size != 0 || h.Mode != 0o777 {
				return fmt.Errorf("lingonx must link to the adjacent lingon binary")
			}
			continue
		}
		mode := int64(0o644)
		if name == "bin/lingon" {
			mode = 0o755
		}
		if h.Size < 0 || h.Size > 512*1024*1024 || h.Mode != mode || h.Linkname != "" {
			return fmt.Errorf("invalid archive file: %s", h.Name)
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(work, name)), 0o755); err != nil {
			return err
		}
		out, err := os.Create(filepath.Join(work, name))
		if err != nil {
			return err
		}
		_, err = io.Copy(out, tr)
		_ = out.Close()
		if err != nil {
			return err
		}
		in, err := os.Open(filepath.Join(work, name))
		if err != nil {
			return err
		}
		err = w.scanPrivate(in, h.Name)
		_ = in.Close()
		if err != nil {
			return err
		}
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("archive is missing required payloads")
	}
	return inspectBinary(filepath.Join(work, "bin", "lingon"), selected, r.Version, r.Commit)
}

func androidTool(name string) (string, error) {
	sdk := os.Getenv("ANDROID_SDK_ROOT")
	if sdk == "" {
		sdk = os.Getenv("ANDROID_HOME")
	}
	if sdk == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		sdk = filepath.Join(home, "Android", "Sdk")
	}
	tools, err := filepath.Glob(filepath.Join(sdk, "build-tools", "*", name))
	if err != nil || len(tools) == 0 {
		return "", fmt.Errorf("android build-tools %s is required under %s", name, sdk)
	}
	sort.Strings(tools)
	return tools[len(tools)-1], nil
}

func (w *workflow) inspectAPK(ctx context.Context, path, version string) error {
	if androidVersionCode(version) == "0" {
		return fmt.Errorf("version exceeds Android's monotonic versionCode range")
	}
	signer, err := androidTool("apksigner")
	if err != nil {
		return err
	}
	if _, err := w.r.run(ctx, command{Dir: w.root, Name: "bash", Args: []string{"android/scripts/verify-release-cert.sh", path}, Env: map[string]string{"APKSIGNER_BIN": signer}}); err != nil {
		return err
	}
	aapt, err := androidTool("aapt")
	if err != nil {
		return err
	}
	out, err := w.exec(ctx, w.root, aapt, "dump", "badging", path)
	if err != nil {
		return err
	}
	want := "name='systems.pkt.lingon' versionCode='" + androidVersionCode(version) + "' versionName='v" + version + "'"
	if !strings.Contains(string(out), want) {
		return fmt.Errorf("signed APK package/version does not match release %s", version)
	}
	return nil
}

var kotlinPackage = regexp.MustCompile(`(?m)^package ([A-Za-z0-9_.]+)`)
var kotlinClass = regexp.MustCompile(`(?m)^class ([A-Za-z0-9_]+)`)

func (w *workflow) instrumentationClasses() (string, error) {
	var classes []string
	err := filepath.WalkDir(filepath.Join(w.root, "android", "app", "src", "androidTest"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".kt") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Contains(data, []byte("@Test")) || strings.HasSuffix(path, "/EndToEndTest.kt") {
			return nil
		}
		pkg, cls := kotlinPackage.FindSubmatch(data), kotlinClass.FindSubmatch(data)
		if len(pkg) != 2 || len(cls) != 2 {
			return fmt.Errorf("cannot discover instrumentation class: %s", path)
		}
		classes = append(classes, string(pkg[1])+"."+string(cls[1]))
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(classes) == 0 {
		return "", fmt.Errorf("no remaining instrumentation classes discovered")
	}
	sort.Strings(classes)
	return strings.Join(classes, ","), nil
}
