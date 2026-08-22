package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// renameFile is swapped in tests to exercise the rename retry path.
var renameFile = os.Rename

// WriteFileAtomic writes data to path with the write-to-temp-then-rename
// pattern (same semantics as fs.RootInfo.WriteMetaFile), so a crash mid-write
// never leaves a half-written target: the previous content survives until the
// rename lands.
//
// The rename can fail transiently on Windows when another process holds the
// target open; a single short retry covers that. If the rename still fails,
// the temp file is kept next to the target so the freshly written data is not
// lost, and the error names it.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := renameFile(tmpName, path); err != nil {
		time.Sleep(10 * time.Millisecond)
		if err = renameFile(tmpName, path); err != nil {
			return fmt.Errorf("rename %s to %s (new content kept in temp file): %w", tmpName, path, err)
		}
	}
	return nil
}
