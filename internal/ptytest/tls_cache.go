package ptytest

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"pkt.systems/lingon/internal/tlsmgr"
)

var (
	testTLSOnce sync.Once
	testTLSDir  string
	testTLSErr  error
)

func ensureTestTLSAssets(t *testing.T) string {
	t.Helper()
	testTLSOnce.Do(func() {
		dir, err := os.MkdirTemp("", "lingon-test-tls-")
		if err != nil {
			testTLSErr = err
			return
		}
		if err := tlsmgr.GenerateAll(context.Background(), dir, "", nil); err != nil {
			testTLSErr = err
			return
		}
		testTLSDir = dir
	})
	if testTLSErr != nil {
		t.Fatalf("generate test tls assets: %v", testTLSErr)
	}
	if testTLSDir == "" {
		t.Fatalf("missing test tls assets")
	}
	return testTLSDir
}

func populateTLSDir(t *testing.T, dst string) {
	t.Helper()
	src := ensureTestTLSAssets(t)
	if err := os.MkdirAll(dst, 0o700); err != nil {
		t.Fatalf("create tls dir: %v", err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read tls dir: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if err := linkOrCopyFile(srcPath, dstPath); err != nil {
			t.Fatalf("copy tls asset %s: %v", entry.Name(), err)
		}
	}
}

func linkOrCopyFile(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
