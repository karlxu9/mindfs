package backup

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"

	"mindfs/server/internal/fs"

	_ "modernc.org/sqlite"
)

// StorageReport is the per-root storage checkup (R-5.3): sizes per file
// class plus the two kinds of garbage the cleanup endpoint can act on.
type StorageReport struct {
	RootID           string   `json:"root_id"`
	SessionsCount    int      `json:"sessions_count"`
	ExchangeBytes    int64    `json:"exchange_bytes"`
	AuxBytes         int64    `json:"aux_bytes"`
	DebugBytes       int64    `json:"debug_bytes"`
	DBBytes          int64    `json:"db_bytes"`
	UploadBytes      int64    `json:"upload_bytes"`
	OrphanDebugFiles []string `json:"orphan_debug_files"`
	JournalFiles     []string `json:"journal_files"`
}

type CleanupResult struct {
	RemovedDebugFiles []string `json:"removed_debug_files"`
	ReclaimedJournals []string `json:"reclaimed_journals"`
	// RemainingJournals lists journals that survived the safe reclaim —
	// their DB is busy or genuinely broken; never deleted directly.
	RemainingJournals []string `json:"remaining_journals"`
}

// BuildStorageReport walks the root's metadata directory. activeKeys is the
// set of session keys present in the sessions table; a debug log whose key is
// missing from it is an orphan left behind before the delete-path fix
// (R-3.2).
func BuildStorageReport(_ context.Context, root fs.RootInfo, activeKeys map[string]bool) (StorageReport, error) {
	report := StorageReport{
		RootID:           root.ID,
		SessionsCount:    len(activeKeys),
		OrphanDebugFiles: []string{},
		JournalFiles:     []string{},
	}
	metaDir := strings.TrimSpace(root.MetaDir())
	if metaDir == "" {
		return report, nil
	}
	if _, err := os.Stat(metaDir); err != nil {
		if os.IsNotExist(err) {
			return report, nil
		}
		return report, err
	}
	err := filepath.WalkDir(metaDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(metaDir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		name := filepath.Base(rel)
		switch {
		case strings.HasSuffix(name, "-journal") || strings.HasSuffix(name, "-wal"):
			report.JournalFiles = append(report.JournalFiles, rel)
		case strings.HasPrefix(rel, "upload/"):
			report.UploadBytes += info.Size()
		case strings.HasSuffix(name, ".debug.jsonl"):
			report.DebugBytes += info.Size()
			key := strings.TrimSuffix(name, ".debug.jsonl")
			if strings.HasPrefix(rel, "sessions/") && !activeKeys[key] {
				report.OrphanDebugFiles = append(report.OrphanDebugFiles, rel)
			}
		case strings.HasSuffix(name, ".aux.jsonl"):
			report.AuxBytes += info.Size()
		case strings.HasSuffix(name, ".jsonl"):
			report.ExchangeBytes += info.Size()
		case strings.HasSuffix(name, ".db"):
			report.DBBytes += info.Size()
		}
		return nil
	})
	return report, err
}

// CleanupStorage removes orphan debug logs (recomputed here, never trusted
// from the client) and safely reclaims sqlite journals: opening and closing
// the owning DB lets sqlite roll the journal back and delete it itself. A
// journal that survives stays listed — deleting it by hand would corrupt the
// DB (design §4.4).
func CleanupStorage(ctx context.Context, root fs.RootInfo, activeKeys map[string]bool) (CleanupResult, error) {
	result := CleanupResult{
		RemovedDebugFiles: []string{},
		ReclaimedJournals: []string{},
		RemainingJournals: []string{},
	}
	report, err := BuildStorageReport(ctx, root, activeKeys)
	if err != nil {
		return result, err
	}
	metaDir := root.MetaDir()
	for _, rel := range report.OrphanDebugFiles {
		if err := os.Remove(filepath.Join(metaDir, filepath.FromSlash(rel))); err == nil {
			result.RemovedDebugFiles = append(result.RemovedDebugFiles, rel)
		}
	}
	for _, rel := range report.JournalFiles {
		journalPath := filepath.Join(metaDir, filepath.FromSlash(rel))
		reclaimJournal(journalPath)
		if _, err := os.Stat(journalPath); os.IsNotExist(err) {
			result.ReclaimedJournals = append(result.ReclaimedJournals, rel)
		} else {
			result.RemainingJournals = append(result.RemainingJournals, rel)
		}
	}
	return result, nil
}

// reclaimJournal opens and closes the DB the journal belongs to; sqlite then
// performs crash recovery (rolling the hot journal back) and removes the
// file. The journal itself is never touched directly.
func reclaimJournal(journalPath string) {
	dbPath := strings.TrimSuffix(strings.TrimSuffix(journalPath, "-journal"), "-wal")
	if dbPath == journalPath {
		return
	}
	if _, err := os.Stat(dbPath); err != nil {
		return
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return
	}
	defer db.Close()
	// A read forces sqlite to actually open the file and run recovery.
	_, _ = db.Exec("PRAGMA schema_version")
}
