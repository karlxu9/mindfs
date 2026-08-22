package main

import (
	"os"
	"sync"
)

// stderrSidecarPath is the small companion file that catches process-level
// stdout/stderr (panics, output bypassing the log package). It is truncated
// on each start and never rotated; the rotating writer owns the real log.
func stderrSidecarPath(logPath string) string {
	return logPath + ".stderr"
}

// rotatingLogWriter keeps the service's log file under maxSize while the
// process runs. The pre-existing rotation only happened once at daemon spawn
// (in the parent), so a long-running service grew its log without bound; the
// service now owns the file and rotates on the write path (R-4.1).
type rotatingLogWriter struct {
	mu      sync.Mutex
	path    string
	maxSize int64
	backups int
	file    *os.File
	size    int64
}

func newRotatingLogWriter(path string, maxSize int64, backups int) (*rotatingLogWriter, error) {
	w := &rotatingLogWriter{path: path, maxSize: maxSize, backups: backups}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.reopenLocked(); err != nil {
		return nil, err
	}
	return w, nil
}

// Close releases the file handle. The service keeps the writer for its whole
// lifetime, but tests (and Windows unlink semantics) need an explicit close.
func (w *rotatingLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *rotatingLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil || (w.maxSize > 0 && w.size >= w.maxSize) {
		if err := w.reopenLocked(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// reopenLocked closes the current handle, rotates if the file on disk crossed
// the threshold (same .1/.2/.3 chain as the old spawn-time rotation), and
// opens a fresh append handle.
func (w *rotatingLogWriter) reopenLocked() error {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	if err := rotateLogIfNeeded(w.path, w.maxSize, w.backups); err != nil {
		return err
	}
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	w.file = file
	w.size = info.Size()
	return nil
}
