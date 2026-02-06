package relay

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestWriteFileAtomicRetriesOnNotExistRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	origRename := atomicRename
	t.Cleanup(func() {
		atomicRename = origRename
	})

	var calls int32
	atomicRename = func(oldpath, newpath string) error {
		if atomic.AddInt32(&calls, 1) == 1 {
			return fs.ErrNotExist
		}
		return origRename(oldpath, newpath)
	}

	if err := writeFileAtomic(path, []byte("ok"), 0o600); err != nil {
		t.Fatalf("writeFileAtomic returned error: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got < 2 {
		t.Fatalf("rename calls = %d, want at least 2", got)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "ok" {
		t.Fatalf("file contents = %q, want %q", string(got), "ok")
	}
}

func TestWriteFileAtomicDoesNotRetryOnNonNotExistError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	origRename := atomicRename
	t.Cleanup(func() {
		atomicRename = origRename
	})

	var calls int32
	atomicRename = func(_, _ string) error {
		atomic.AddInt32(&calls, 1)
		return fs.ErrPermission
	}

	err := writeFileAtomic(path, []byte("ok"), 0o600)
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("writeFileAtomic error = %v, want %v", err, fs.ErrPermission)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("rename calls = %d, want 1", got)
	}
}
