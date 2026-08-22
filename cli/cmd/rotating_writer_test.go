package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestRotatingLogWriterRotatesAcrossThreshold(t *testing.T) {
	path := filepath.Join(t.TempDir(), "svc.log")
	w, err := newRotatingLogWriter(path, 100, 3)
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	defer w.Close()
	chunk := strings.Repeat("a", 60) + "\n"
	for i := 0; i < 2; i++ {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	// 122 bytes on disk >= 100: the next write must rotate first.
	if _, err := w.Write([]byte("fresh\n")); err != nil {
		t.Fatalf("post-threshold write: %v", err)
	}
	backup, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if len(backup) != 2*len(chunk) {
		t.Fatalf("backup size = %d, want %d", len(backup), 2*len(chunk))
	}
	main, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read main: %v", err)
	}
	if string(main) != "fresh\n" {
		t.Fatalf("main restarted with %q, want fresh line only", main)
	}
}

func TestRotatingLogWriterKeepsThreeBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "svc.log")
	w, err := newRotatingLogWriter(path, 10, 3)
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	defer w.Close()
	// Each write overflows the threshold, so every generation rotates.
	for i := 0; i < 6; i++ {
		line := strings.Repeat(string(rune('a'+i)), 12)
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	for i := 1; i <= 3; i++ {
		if _, err := os.Stat(rotatedLogPath(path, i)); err != nil {
			t.Fatalf("backup .%d missing: %v", i, err)
		}
	}
	if _, err := os.Stat(rotatedLogPath(path, 4)); !os.IsNotExist(err) {
		t.Fatalf("backup .4 should not exist, stat err=%v", err)
	}
	// Newest rotated content sits in .1.
	one, err := os.ReadFile(rotatedLogPath(path, 1))
	if err != nil {
		t.Fatalf("read .1: %v", err)
	}
	if !strings.HasPrefix(string(one), "eeee") {
		t.Fatalf(".1 content = %q, want the second-newest generation (e...)", one)
	}
}

func TestRotatingLogWriterConcurrentWritesAreSafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "svc.log")
	w, err := newRotatingLogWriter(path, 512, 3)
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	defer w.Close()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				if _, err := w.Write([]byte("concurrent log line\n")); err != nil {
					t.Errorf("write: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("main log missing after concurrent writes: %v", err)
	}
}

// Two instances on different ports must write to different log files (R-4.1
// acceptance 2); the address key matches the PID file's.
func TestServicePathsSeparateLogFilesPerAddress(t *testing.T) {
	stateDir := t.TempDir()
	pidA, logA, err := servicePaths(stateDir, "127.0.0.1:7331")
	if err != nil {
		t.Fatalf("servicePaths a: %v", err)
	}
	pidB, logB, err := servicePaths(stateDir, "127.0.0.1:8442")
	if err != nil {
		t.Fatalf("servicePaths b: %v", err)
	}
	if logA == logB {
		t.Fatalf("both instances share log file %s", logA)
	}
	if pidA == pidB {
		t.Fatalf("both instances share pid file %s", pidA)
	}
	if !strings.Contains(filepath.Base(logA), "7331") || !strings.Contains(filepath.Base(logB), "8442") {
		t.Fatalf("log names missing address: %s / %s", logA, logB)
	}
}
