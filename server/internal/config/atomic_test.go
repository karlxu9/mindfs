package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteFileAtomicWritesContentAndPerm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	if err := WriteFileAtomic(path, []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != `{"a":1}` {
		t.Fatalf("content = %q", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("perm = %o, want 600", perm)
		}
	}
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*.tmp-*"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("temp files left behind: %v", leftovers)
	}
}

func TestWriteFileAtomicReplacesExistingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := WriteFileAtomic(path, []byte("new"), 0o644); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "new" {
		t.Fatalf("content = %q, want new", got)
	}
}

func TestWriteFileAtomicFailedRenameKeepsTargetAndTemp(t *testing.T) {
	original := renameFile
	defer func() { renameFile = original }()
	attempts := 0
	renameFile = func(oldpath, newpath string) error {
		attempts++
		return errors.New("target busy")
	}

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	err := WriteFileAtomic(path, []byte("new"), 0o644)
	if err == nil {
		t.Fatal("WriteFileAtomic succeeded, want error")
	}
	if attempts != 2 {
		t.Fatalf("rename attempts = %d, want 2 (one retry)", attempts)
	}
	if !strings.Contains(err.Error(), "temp file") {
		t.Fatalf("error should name the kept temp file, got: %v", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read back: %v", readErr)
	}
	if string(got) != "old" {
		t.Fatalf("target content = %q, want untouched old", got)
	}
	leftovers, globErr := filepath.Glob(filepath.Join(filepath.Dir(path), "*.tmp-*"))
	if globErr != nil {
		t.Fatalf("glob: %v", globErr)
	}
	if len(leftovers) != 1 {
		t.Fatalf("temp files = %v, want the new content kept in exactly one", leftovers)
	}
	kept, readErr := os.ReadFile(leftovers[0])
	if readErr != nil {
		t.Fatalf("read temp: %v", readErr)
	}
	if string(kept) != "new" {
		t.Fatalf("temp content = %q, want new", kept)
	}
}

func TestWriteFileAtomicRetriesRenameOnce(t *testing.T) {
	original := renameFile
	defer func() { renameFile = original }()
	attempts := 0
	renameFile = func(oldpath, newpath string) error {
		attempts++
		if attempts == 1 {
			return errors.New("transient lock")
		}
		return os.Rename(oldpath, newpath)
	}

	path := filepath.Join(t.TempDir(), "config.json")
	if err := WriteFileAtomic(path, []byte("payload"), 0o644); err != nil {
		t.Fatalf("WriteFileAtomic with transient failure: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("rename attempts = %d, want 2", attempts)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "payload" {
		t.Fatalf("content = %q, want payload", got)
	}
}
