package lifecycle

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"debug/elf"
	"debug/macho"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type target struct{ OS, Arch string }

var releaseTargets = []target{{"linux", "amd64"}, {"linux", "arm64"}, {"freebsd", "amd64"}, {"freebsd", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}}

type receipt struct {
	Version, Commit, Branch, Toolchain, ManifestHash string
	Image, ImageID, BaseImage                        string
	FullProof                                        bool
}

func (w *workflow) receiptPath() string { return filepath.Join(w.work, "ready.json") }

func (w *workflow) saveReceipt(r receipt) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := w.receiptPath() + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, w.receiptPath())
}

func (w *workflow) readReceipt() (receipt, error) {
	var r receipt
	data, err := os.ReadFile(w.receiptPath())
	if err != nil {
		return r, fmt.Errorf("no verified local artifacts; run make release: %w", err)
	}
	err = json.Unmarshal(data, &r)
	if err == nil && (!stableVersion.MatchString("v"+r.Version) || len(r.Commit) != 40) {
		err = fmt.Errorf("invalid release receipt")
	}
	return r, err
}

func (w *workflow) dist(version string) string { return filepath.Join(w.root, "dist", "v"+version) }

func expectedArtifacts(version string) []string {
	names := []string{"lingon-" + version + "-android.apk", "lingon-" + version + "-RELEASE.json"}
	for _, t := range releaseTargets {
		names = append(names, "lingon-"+version+"-"+t.OS+"-"+t.Arch+".tar.gz")
	}
	sort.Strings(names)
	return names
}

func checksumName(version string) string { return "lingon-" + version + "-CHECKSUMS" }

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func makeChecksums(dir, version string) (string, error) {
	var manifest strings.Builder
	for _, name := range expectedArtifacts(version) {
		sum, err := hashFile(filepath.Join(dir, name))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&manifest, "%s  %s\n", sum, name)
	}
	path := filepath.Join(dir, checksumName(version))
	if err := os.WriteFile(path, []byte(manifest.String()), 0o644); err != nil {
		return "", err
	}
	return hashFile(path)
}

// Upload selection is exclusively manifest-driven. Exact set equality rejects
// stale, missing, duplicated, escaping and unlisted artifacts.
func verifyChecksums(dir, version string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, checksumName(version)))
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		sum, name, ok := strings.Cut(line, "  ")
		if !ok || len(sum) != 64 || filepath.Base(name) != name || strings.ContainsAny(name, "\\\r\n") {
			return nil, fmt.Errorf("unsafe checksum line %q", line)
		}
		if decoded, err := hex.DecodeString(sum); err != nil || len(decoded) != 32 {
			return nil, fmt.Errorf("invalid SHA-256 for %q", name)
		}
		info, err := os.Lstat(filepath.Join(dir, name))
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("artifact must be a regular file: %s", name)
		}
		actual, err := hashFile(filepath.Join(dir, name))
		if err != nil || actual != sum {
			return nil, fmt.Errorf("checksum mismatch: %s", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if strings.Join(names, "\n") != strings.Join(expectedArtifacts(version), "\n") {
		return nil, fmt.Errorf("checksum manifest does not match the required release artifact set")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	if len(entries) != len(names)+1 {
		return nil, fmt.Errorf("unlisted files in release directory %s", dir)
	}
	return append(names, checksumName(version)), nil
}

func (w *workflow) matrix(ctx context.Context) error {
	if err := w.clean(ctx); err != nil {
		return err
	}
	version, err := w.exactVersion(ctx)
	if err != nil {
		return err
	}
	if version != "0.0.0" && !w.finalBuild {
		return fmt.Errorf("tagged artifact builds require make release; release-matrix is a rehearsal")
	}
	remote, err := w.remoteRef(ctx, "refs/tags/v"+version)
	if err != nil {
		return err
	}
	if remote != "" {
		return fmt.Errorf("cannot rebuild pushed release v%s", version)
	}
	// Invalidate an old publication receipt before starting any build.
	if err := os.Remove(w.receiptPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	commit, err := w.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	branch, err := w.git(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return err
	}
	toolchain, err := w.exec(ctx, w.root, "go", "env", "GOVERSION")
	if err != nil {
		return err
	}
	epochText, err := w.git(ctx, "show", "-s", "--format=%ct", "HEAD")
	if err != nil {
		return err
	}
	epoch, err := strconv.ParseInt(epochText, 10, 64)
	if err != nil {
		return err
	}
	dir := w.dist(version)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, t := range releaseTargets {
		bin := filepath.Join(w.work, "targets", t.OS+"-"+t.Arch, "lingon")
		if err := os.MkdirAll(filepath.Dir(bin), 0o700); err != nil {
			return err
		}
		env := map[string]string{"GOMAXPROCS": "4", "GOFLAGS": "-p=1", "GOTOOLCHAIN": "local", "CGO_ENABLED": "0", "GOOS": t.OS, "GOARCH": t.Arch, "GOAMD64": "v1", "GOARM64": "v8.0", "GOEXPERIMENT": "", "GOWORK": "off"}
		flags := "-s -w -X pkt.systems/version.buildVersion=v" + version + " -X pkt.systems/lingon/internal/buildmeta.Stamp=LINGON_RELEASE:v" + version + ":" + commit + ":END"
		if _, err := w.r.run(ctx, command{Dir: w.root, Name: "go", Args: []string{"build", "-buildvcs=true", "-trimpath", "-ldflags=" + flags, "-o", bin, "./cmd/lingon"}, Env: env}); err != nil {
			return err
		}
		if err := inspectBinary(bin, t, version, commit); err != nil {
			return err
		}
		if t.OS == "linux" && t.Arch == runtime.GOARCH {
			out, err := w.exec(ctx, w.root, bin, "version", "--semver")
			if err != nil || strings.TrimSpace(string(out)) != version {
				return fmt.Errorf("native release executable reports the wrong version")
			}
		}
		if err := writeArchive(dir, bin, w.root, t, version, epoch); err != nil {
			return err
		}
	}
	g := gradleGate("signed release APK", ":app:assembleRelease", "-Plingon.versionName=v"+version, "-Plingon.versionCode="+androidVersionCode(version))
	if _, err := w.r.run(ctx, command{Dir: filepath.Join(w.root, g.dir), Name: g.tool, Args: g.args}); err != nil {
		return err
	}
	apk := filepath.Join(dir, "lingon-"+version+"-android.apk")
	if err := copyFile(filepath.Join(w.root, "android", "app", "build", "outputs", "apk", "release", "app-release.apk"), apk); err != nil {
		return err
	}
	if err := w.inspectAPK(ctx, apk, version); err != nil {
		return err
	}
	r := receipt{Version: version, Commit: commit, Branch: branch, Toolchain: strings.TrimSpace(string(toolchain))}
	if err := w.buildContainer(ctx, &r, epochText); err != nil {
		return err
	}
	meta, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "lingon-"+version+"-RELEASE.json"), append(meta, '\n'), 0o644); err != nil {
		return err
	}
	r.ManifestHash, err = makeChecksums(dir, version)
	if err != nil {
		return err
	}
	if err := w.verifyArtifacts(ctx, r); err != nil {
		return err
	}
	if err := w.unchanged(ctx, commit); err != nil {
		return err
	}
	if err := w.sameBranch(ctx, branch); err != nil {
		return err
	}
	return w.saveReceipt(r)
}

func androidVersionCode(version string) string {
	parts := strings.Split(version, ".")
	code := 0
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n >= 1000 || n < 0 {
			return "0" // inspectAPK rejects this, never publish an invalid code.
		}
		code = code*1000 + n
	}
	if code == 0 {
		code = 1 // rehearsal only
	}
	if code > 2100000000 {
		return "0"
	}
	return strconv.Itoa(code)
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func writeArchive(dir, bin, root string, t target, version string, epoch int64) error {
	name := "lingon-" + version + "-" + t.OS + "-" + t.Arch
	f, err := os.Create(filepath.Join(dir, name+".tar.gz"))
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, directory := range []string{"", "bin", "share", "share/doc", "share/doc/lingon"} {
		if err := tw.WriteHeader(&tar.Header{Name: path.Join(name, directory) + "/", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: time.Unix(epoch, 0)}); err != nil {
			return err
		}
	}
	for _, item := range []struct{ source, name string }{{bin, "bin/lingon"}, {filepath.Join(root, "LICENSE"), "share/doc/lingon/LICENSE"}} {
		info, err := os.Stat(item.source)
		if err != nil {
			return err
		}
		mode := int64(0o644)
		if item.name == "bin/lingon" {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{Name: name + "/" + item.name, Mode: mode, Size: info.Size(), ModTime: time.Unix(epoch, 0)}); err != nil {
			return err
		}
		in, err := os.Open(item.source)
		if err != nil {
			return err
		}
		_, err = io.Copy(tw, in)
		_ = in.Close()
		if err != nil {
			return err
		}
	}
	if err := tw.WriteHeader(&tar.Header{Name: name + "/bin/lingonx", Typeflag: tar.TypeSymlink, Linkname: "lingon", Mode: 0o777, ModTime: time.Unix(epoch, 0)}); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return f.Close()
}

func inspectBinary(path string, t target, version, commit string) error {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return err
	}
	settings := make(map[string]string)
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	if info.Path != "pkt.systems/lingon/cmd/lingon" || settings["CGO_ENABLED"] != "0" || settings["GOOS"] != t.OS || settings["GOARCH"] != t.Arch || settings["-trimpath"] != "true" || settings["vcs.revision"] != commit || settings["vcs.modified"] != "false" {
		return fmt.Errorf("binary build metadata disagrees with release: %s (path=%s settings=%v)", path, info.Path, settings)
	}
	if err := verifyStamp(path, "LINGON_RELEASE:v"+version+":"+commit+":END"); err != nil {
		return err
	}
	if t.OS == "darwin" {
		file, err := macho.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		wantCPU := macho.CpuAmd64
		if t.Arch == "arm64" {
			wantCPU = macho.CpuArm64
		}
		if file.Cpu != wantCPU || file.Type != macho.TypeExec {
			return fmt.Errorf("wrong Mach-O architecture/type: %s", path)
		}
		libs, err := file.ImportedLibraries()
		if err != nil {
			return err
		}
		for _, lib := range libs {
			if !strings.HasPrefix(lib, "/usr/lib/") && !strings.HasPrefix(lib, "/System/Library/") {
				return fmt.Errorf("non-system Darwin dependency: %s", lib)
			}
		}
		for _, load := range file.Loads {
			if _, ok := load.(*macho.Rpath); ok {
				return fmt.Errorf("unexpected Darwin runtime search path")
			}
		}
		return nil
	}
	file, err := elf.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	wantMachine := elf.EM_X86_64
	if t.Arch == "arm64" {
		wantMachine = elf.EM_AARCH64
	}
	if file.Machine != wantMachine || file.Type != elf.ET_EXEC {
		return fmt.Errorf("wrong ELF architecture/type: %s", path)
	}
	if (t.OS == "freebsd" && file.OSABI != elf.ELFOSABI_FREEBSD) || (t.OS == "linux" && file.OSABI != elf.ELFOSABI_NONE) {
		return fmt.Errorf("wrong ELF OS ABI: %s", path)
	}
	for _, prog := range file.Progs {
		if prog.Type == elf.PT_INTERP || prog.Type == elf.PT_DYNAMIC {
			return fmt.Errorf("Linux/FreeBSD binary must be static: %s", path)
		}
	}
	return nil
}
