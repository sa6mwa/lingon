package lifecycle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestReleaseArchiveUsesUnixInstallLayout(t *testing.T) {
	root := t.TempDir()
	dir := t.TempDir()
	bin := filepath.Join(root, "binary")
	writeTestFile(t, bin, []byte("executable payload"))
	writeTestFile(t, filepath.Join(root, "LICENSE"), []byte("license payload"))
	const epoch int64 = 1700000000
	selected := target{"linux", "amd64"}
	if err := writeArchive(dir, bin, root, selected, "1.2.3", epoch); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "lingon-1.2.3-linux-amd64.tar.gz")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	expected := map[string]struct {
		kind       byte
		mode       int64
		body, link string
	}{
		"": {tar.TypeDir, 0755, "", ""}, "bin/": {tar.TypeDir, 0755, "", ""},
		"share/": {tar.TypeDir, 0755, "", ""}, "share/doc/": {tar.TypeDir, 0755, "", ""}, "share/doc/lingon/": {tar.TypeDir, 0755, "", ""},
		"bin/lingon": {tar.TypeReg, 0755, "executable payload", ""}, "bin/lingonx": {tar.TypeSymlink, 0777, "", "lingon"},
		"share/doc/lingon/LICENSE": {tar.TypeReg, 0644, "license payload", ""},
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		const prefix = "lingon-1.2.3-linux-amd64/"
		if len(h.Name) < len(prefix) || h.Name[:len(prefix)] != prefix {
			t.Fatalf("outside root: %s", h.Name)
		}
		name := h.Name[len(prefix):]
		want, ok := expected[name]
		if !ok {
			t.Fatalf("unexpected/duplicate entry: %s", h.Name)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag != want.kind || h.Mode != want.mode || string(body) != want.body || h.Linkname != want.link || !h.ModTime.Equal(time.Unix(epoch, 0)) {
			t.Fatalf("wrong entry %s: %+v body=%q", name, h, body)
		}
		delete(expected, name)
	}
	if len(expected) != 0 {
		t.Fatalf("missing entries: %v", expected)
	}
	if err := writeArchive(dir, bin, root, selected, "1.2.3", epoch); err != nil {
		t.Fatal(err)
	}
	again, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, again) {
		t.Fatalf("non-reproducible archive: %v", err)
	}
}

func TestInspectArchiveEnforcesUnixInstallLayout(t *testing.T) {
	w, f := testWorkflow(t)
	ctx := context.Background()
	commit := testGit(t, w, "rev-parse", "HEAD")
	selected := target{"linux", runtime.GOARCH}
	version := "1.2.3"
	bin := filepath.Join(w.work, "fixture-bin")
	flags := "-X pkt.systems/lingon/internal/buildmeta.Stamp=LINGON_RELEASE:v" + version + ":" + commit + ":END"
	_, err := f.real.run(ctx, command{Dir: w.root, Name: "go", Args: []string{"build", "-buildvcs=true", "-trimpath", "-ldflags=" + flags, "-o", bin, "./cmd/lingon"}, Env: map[string]string{"CGO_ENABLED": "0", "GOOS": "linux", "GOARCH": runtime.GOARCH, "GOTOOLCHAIN": "local"}})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(w.work, "archives")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeArchive(dir, bin, w.root, selected, version, 1700000000); err != nil {
		t.Fatal(err)
	}
	name := "lingon-" + version + "-linux-" + runtime.GOARCH
	path := filepath.Join(dir, name+".tar.gz")
	valid, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*tar.Header) *tar.Header
	}{
		{"valid", nil},
		{"flat binary", func(h *tar.Header) *tar.Header {
			if h.Name == name+"/bin/lingon" {
				h.Name = name + "/lingon"
			}
			return h
		}},
		{"escaping alias", func(h *tar.Header) *tar.Header {
			if h.Typeflag == tar.TypeSymlink {
				h.Linkname = "../../outside"
			}
			return h
		}},
		{"missing license", func(h *tar.Header) *tar.Header {
			if h.Name == name+"/share/doc/lingon/LICENSE" {
				return nil
			}
			return h
		}},
		{"missing bin directory", func(h *tar.Header) *tar.Header {
			if h.Name == name+"/bin/" {
				return nil
			}
			return h
		}},
		{"writable directory", func(h *tar.Header) *tar.Header {
			if h.Name == name+"/bin/" {
				h.Mode = 0777
			}
			return h
		}},
		{"executable license", func(h *tar.Header) *tar.Header {
			if h.Name == name+"/share/doc/lingon/LICENSE" {
				h.Mode = 0755
			}
			return h
		}},
		{"traversal entry", func(h *tar.Header) *tar.Header {
			if h.Name == name+"/bin/lingon" {
				h.Name = name + "/bin/../lingon"
			}
			return h
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			gz := gzip.NewWriter(&out)
			tw := tar.NewWriter(gz)
			input, err := gzip.NewReader(bytes.NewReader(valid))
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			tr := tar.NewReader(input)
			for {
				h, err := tr.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(tr)
				if err != nil {
					t.Fatal(err)
				}
				if tc.mutate != nil {
					h = tc.mutate(h)
				}
				if h == nil {
					continue
				}
				if err := tw.WriteHeader(h); err != nil {
					t.Fatal(err)
				}
				if _, err := tw.Write(data); err != nil {
					t.Fatal(err)
				}
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, out.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			err = w.inspectArchive(path, receipt{Version: version, Commit: commit})
			if (err == nil) != (tc.mutate == nil) {
				t.Fatalf("layout verification: %v", err)
			}
		})
	}
}
