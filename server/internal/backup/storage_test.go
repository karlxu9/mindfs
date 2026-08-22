package backup

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"mindfs/server/internal/fs"
)

func TestBuildStorageReportClassifiesAndFindsGarbage(t *testing.T) {
	root := fs.NewRootInfo("r1", "r1", t.TempDir())
	meta := root.MetaDir()
	writeFile(t, filepath.Join(meta, "sessions", "active.jsonl"), "12345678")            // 8 bytes exchange
	writeFile(t, filepath.Join(meta, "sessions", "active.aux.jsonl"), "1234")           // 4 bytes aux
	writeFile(t, filepath.Join(meta, "sessions", "active.debug.jsonl"), "12")           // 2 bytes debug (active)
	writeFile(t, filepath.Join(meta, "sessions", "ghost.debug.jsonl"), "123456")        // 6 bytes debug (orphan)
	writeFile(t, filepath.Join(meta, "sessions", "session-list.db"), "1234567890")      // 10 bytes db
	writeFile(t, filepath.Join(meta, "sessions", "session-list.db-journal"), "j")       // journal
	writeFile(t, filepath.Join(meta, "upload", "2026-01-01", "pic.png"), "123")         // 3 bytes upload
	activeKeys := map[string]bool{"active": true}

	report, err := BuildStorageReport(context.Background(), root, activeKeys)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if report.SessionsCount != 1 {
		t.Fatalf("sessions_count = %d", report.SessionsCount)
	}
	if report.ExchangeBytes != 8 || report.AuxBytes != 4 || report.DebugBytes != 8 || report.DBBytes != 10 || report.UploadBytes != 3 {
		t.Fatalf("byte classes wrong: %+v", report)
	}
	if len(report.OrphanDebugFiles) != 1 || report.OrphanDebugFiles[0] != "sessions/ghost.debug.jsonl" {
		t.Fatalf("orphans = %v, want only ghost", report.OrphanDebugFiles)
	}
	if len(report.JournalFiles) != 1 || report.JournalFiles[0] != "sessions/session-list.db-journal" {
		t.Fatalf("journals = %v", report.JournalFiles)
	}
}

func TestCleanupStorageRemovesOrphansAndReclaimsJournals(t *testing.T) {
	root := fs.NewRootInfo("r1", "r1", t.TempDir())
	meta := root.MetaDir()
	writeFile(t, filepath.Join(meta, "sessions", "active.debug.jsonl"), "keep me")
	writeFile(t, filepath.Join(meta, "sessions", "ghost.debug.jsonl"), "orphan")

	// Real sqlite DB with rows plus a bogus leftover journal: opening the DB
	// makes sqlite discard the invalid journal while the data stays intact.
	dbPath := filepath.Join(meta, "tasks", "task-kanban.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE x(id INTEGER); INSERT INTO x VALUES (1),(2),(3)"); err != nil {
		t.Fatalf("seed db: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	writeFile(t, dbPath+"-journal", "not a real journal")

	result, err := CleanupStorage(context.Background(), root, map[string]bool{"active": true})
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if len(result.RemovedDebugFiles) != 1 || result.RemovedDebugFiles[0] != "sessions/ghost.debug.jsonl" {
		t.Fatalf("removed = %v", result.RemovedDebugFiles)
	}
	if _, err := os.Stat(filepath.Join(meta, "sessions", "active.debug.jsonl")); err != nil {
		t.Fatal("active debug file was wrongly deleted")
	}
	if _, err := os.Stat(filepath.Join(meta, "sessions", "ghost.debug.jsonl")); !os.IsNotExist(err) {
		t.Fatal("orphan debug file still present")
	}
	if len(result.ReclaimedJournals) != 1 || result.ReclaimedJournals[0] != "tasks/task-kanban.db-journal" {
		t.Fatalf("reclaimed = %v remaining = %v", result.ReclaimedJournals, result.RemainingJournals)
	}
	if _, err := os.Stat(dbPath + "-journal"); !os.IsNotExist(err) {
		t.Fatal("journal survived reclaim")
	}
	verify, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer verify.Close()
	var count int
	if err := verify.QueryRow("SELECT COUNT(*) FROM x").Scan(&count); err != nil || count != 3 {
		t.Fatalf("db rows after reclaim = %d err=%v, want 3 intact", count, err)
	}
}
