package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeLogFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "svc.log")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestTailLogFileEmptyFile(t *testing.T) {
	path := writeLogFixture(t, "")
	result, err := tailLogFile(path, 200)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	if len(result.Lines) != 0 || result.Truncated || result.SizeBytes != 0 {
		t.Fatalf("result = %+v, want empty", result)
	}
}

func TestTailLogFileReturnsLastLines(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 500; i++ {
		fmt.Fprintf(&b, "line-%03d\n", i)
	}
	path := writeLogFixture(t, b.String())
	result, err := tailLogFile(path, 3)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	want := []string{"line-498", "line-499", "line-500"}
	if len(result.Lines) != 3 {
		t.Fatalf("lines = %v, want 3", result.Lines)
	}
	for i := range want {
		if result.Lines[i] != want[i] {
			t.Fatalf("lines = %v, want %v", result.Lines, want)
		}
	}
	if !result.Truncated {
		t.Fatal("truncated should be true when older lines exist")
	}
}

func TestTailLogFileSpansBlockBoundary(t *testing.T) {
	// Each line is 100 bytes; 1000 lines is ~100KB, larger than one 64KB
	// block, so collecting 800 lines must walk more than one block.
	line := strings.Repeat("x", 90)
	var b strings.Builder
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&b, "%s-%04d\n", line, i)
	}
	path := writeLogFixture(t, b.String())
	result, err := tailLogFile(path, 800)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	if len(result.Lines) != 800 {
		t.Fatalf("lines = %d, want 800", len(result.Lines))
	}
	if !strings.HasSuffix(result.Lines[0], "-0200") || !strings.HasSuffix(result.Lines[799], "-0999") {
		t.Fatalf("window = [%s .. %s], want [-0200 .. -0999]", result.Lines[0], result.Lines[799])
	}
}

func TestTailLogFileTruncatesEnormousLine(t *testing.T) {
	huge := strings.Repeat("z", 64*1024)
	path := writeLogFixture(t, "before\n"+huge+"\n")
	result, err := tailLogFile(path, 10)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	last := result.Lines[len(result.Lines)-1]
	if len(last) > logTailMaxLineBytes+64 {
		t.Fatalf("line not truncated: %d bytes", len(last))
	}
	if !strings.HasSuffix(last, "(line truncated)") {
		t.Fatalf("missing truncation marker: %q", last[len(last)-40:])
	}
	if !result.Truncated {
		t.Fatal("truncated flag should be set")
	}
}

func TestClampLogTailLines(t *testing.T) {
	tests := []struct {
		raw  string
		want int
	}{
		{"", logTailDefaultLines},
		{"50", 50},
		{"0", 1},
		{"-3", 1},
		{"999999", logTailMaxLines},
		{"junk", logTailDefaultLines},
	}
	for _, tt := range tests {
		if got := clampLogTailLines(tt.raw); got != tt.want {
			t.Fatalf("clamp(%q) = %d, want %d", tt.raw, got, tt.want)
		}
	}
}

// The diagnostics payload must keep the field set from design §5.2: renaming
// or dropping keys breaks the panel.
func TestHandleDiagnosticsFieldSnapshot(t *testing.T) {
	logPath := writeLogFixture(t, "hello\n")
	handler := &HTTPHandler{
		Version:   "test-1.0",
		StartedAt: time.Now().Add(-90 * time.Second).UTC(),
		Addr:      "127.0.0.1:7331",
		LogPath:   logPath,
		AppContext: &AppContext{
			Dirs: nil, // no roots configured
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/diagnostics", nil)
	rec := httptest.NewRecorder()
	handler.handleDiagnostics(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"version", "started_at", "uptime_seconds", "os_arch", "addr", "roots", "agents", "webpush", "relay", "scheduled", "log"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("missing key %q in %v", key, payload)
		}
	}
	if payload["version"] != "test-1.0" || payload["addr"] != "127.0.0.1:7331" {
		t.Fatalf("identity fields wrong: %v", payload)
	}
	if uptime, ok := payload["uptime_seconds"].(float64); !ok || uptime < 89 {
		t.Fatalf("uptime_seconds = %v, want >= 89", payload["uptime_seconds"])
	}
	logInfo, ok := payload["log"].(map[string]any)
	if !ok || logInfo["path"] != logPath || logInfo["size_bytes"].(float64) != 6 {
		t.Fatalf("log = %v", payload["log"])
	}
}

func TestHandleLogsWithoutConfiguredPathReturns404(t *testing.T) {
	handler := &HTTPHandler{}
	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	rec := httptest.NewRecorder()
	handler.handleLogs(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
