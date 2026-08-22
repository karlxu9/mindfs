// Package backup produces the one-click export archive (R-5.1): every
// managed root's metadata directory, the fallback session DBs referenced by
// .link pointers, and (scope=all) the user config directory, with the sqlite
// stores exported as consistent snapshots.
package backup

import (
	"archive/zip"
	"context"
	"encoding/json"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mindfs/server/internal/fs"
)

//go:embed restore.md
var restoreTemplate string

const (
	FormatVersion = 1

	sessionDBRelPath   = "sessions/session-list.db"
	sessionLinkRelPath = "sessions/session-list.db.link"
	kanbanDBRelPath    = "tasks/task-kanban.db"
	historyDBRelPath   = "commands/history.db"
)

// credentialFiles are skipped when the caller opted out of exporting
// credentials (R-5.4); agents-config is a whole directory of config backups
// that may embed keys.
var credentialFiles = map[string]bool{
	"credentials.json":             true,
	"agents-env.json":              true,
	"autostart-environment.json":   true,
	"local-cli-tokens.json":        true,
	"e2ee.json":                    true,
	"key.pem":                      true,
}

const credentialDir = "agents-config"

type ExportInput struct {
	Scope              string // "root" | "all"
	RootID             string
	IncludeCredentials bool
	Version            string
}

type ManifestRoot struct {
	RootID        string `json:"root_id"`
	RootPath      string `json:"root_path"`
	MetaLocation  string `json:"meta_location"`
	ArchivePath   string `json:"archive_path"`
	HasFallbackDB bool   `json:"has_fallback_db"`
}

type Manifest struct {
	FormatVersion       int            `json:"format_version"`
	MindFSVersion       string         `json:"mindfs_version"`
	ExportedAt          time.Time      `json:"exported_at"`
	IncludesCredentials bool           `json:"includes_credentials"`
	Roots               []ManifestRoot `json:"roots"`
	// BestEffort lists archive entries copied without a consistency snapshot
	// (commands/history.db always; a sqlite store when its snapshot failed).
	BestEffort []string `json:"best_effort,omitempty"`
}

// Exporter carries the injected dependencies so tests can drive the export
// without a full AppContext.
type Exporter struct {
	Version   string
	ConfigDir string // user config dir; empty skips the userconfig section
	Roots     []fs.RootInfo
	// SnapshotSessionDB/SnapshotKanbanDB export a consistent copy of the
	// root's DB to targetPath (a fresh path).
	SnapshotSessionDB func(ctx context.Context, rootID, targetPath string) error
	SnapshotKanbanDB  func(ctx context.Context, rootID, targetPath string) error
}

// Export writes the archive to w and returns the manifest that was embedded
// in it.
func (e *Exporter) Export(ctx context.Context, w io.Writer, input ExportInput) (Manifest, error) {
	manifest := Manifest{
		FormatVersion:       FormatVersion,
		MindFSVersion:       strings.TrimSpace(input.Version),
		ExportedAt:          time.Now().UTC(),
		IncludesCredentials: input.IncludeCredentials,
		Roots:               []ManifestRoot{},
	}

	roots, err := e.selectRoots(input)
	if err != nil {
		return manifest, err
	}

	tmpDir, err := os.MkdirTemp("", "mindfs-backup-*")
	if err != nil {
		return manifest, err
	}
	defer os.RemoveAll(tmpDir)

	zw := zip.NewWriter(w)
	for _, root := range roots {
		entry, err := e.exportRoot(ctx, zw, tmpDir, root, &manifest)
		if err != nil {
			return manifest, fmt.Errorf("export root %s: %w", root.ID, err)
		}
		manifest.Roots = append(manifest.Roots, entry)
	}
	if input.Scope == "all" && strings.TrimSpace(e.ConfigDir) != "" {
		if err := e.exportUserConfig(zw, input.IncludeCredentials); err != nil {
			return manifest, fmt.Errorf("export user config: %w", err)
		}
	}

	if err := writeZipBytes(zw, "RESTORE.md", []byte(restoreTemplate)); err != nil {
		return manifest, err
	}
	manifestPayload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return manifest, err
	}
	if err := writeZipBytes(zw, "manifest.json", append(manifestPayload, '\n')); err != nil {
		return manifest, err
	}
	return manifest, zw.Close()
}

func (e *Exporter) selectRoots(input ExportInput) ([]fs.RootInfo, error) {
	switch input.Scope {
	case "all":
		return e.Roots, nil
	case "root":
		rootID := strings.TrimSpace(input.RootID)
		for _, root := range e.Roots {
			if root.ID == rootID {
				return []fs.RootInfo{root}, nil
			}
		}
		return nil, fmt.Errorf("root not found: %s", rootID)
	default:
		return nil, errors.New(`scope must be "root" or "all"`)
	}
}

func (e *Exporter) exportRoot(ctx context.Context, zw *zip.Writer, tmpDir string, root fs.RootInfo, manifest *Manifest) (ManifestRoot, error) {
	archivePath := "roots/" + root.ID
	entry := ManifestRoot{
		RootID:       root.ID,
		RootPath:     root.RootPath,
		MetaLocation: root.EffectiveMetaLocation(),
		ArchivePath:  archivePath,
	}
	metaDir := strings.TrimSpace(root.MetaDir())
	if metaDir == "" {
		return entry, nil
	}
	if _, err := os.Stat(metaDir); err != nil {
		if os.IsNotExist(err) {
			return entry, nil
		}
		return entry, err
	}

	err := filepath.WalkDir(metaDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		rel, err := filepath.Rel(metaDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if skipMetaFile(rel) {
			return nil
		}
		zipPath := archivePath + "/" + rel
		if rel == historyDBRelPath {
			// Short-lived connections, low-value data: plain copy, flagged.
			manifest.BestEffort = append(manifest.BestEffort, zipPath)
		}
		return copyFileToZip(zw, zipPath, path)
	})
	if err != nil {
		return entry, err
	}

	// Session DB: snapshot through the manager (which resolves the .link
	// fallback transparently). The archive location depends on the layout.
	linkTarget := readSessionDBLink(filepath.Join(metaDir, filepath.FromSlash(sessionLinkRelPath)))
	localDB := filepath.Join(metaDir, filepath.FromSlash(sessionDBRelPath))
	localDBExists := fileExists(localDB)
	if linkTarget != "" || localDBExists {
		zipPath := archivePath + "/" + sessionDBRelPath
		sourceForFallbackCopy := localDB
		if linkTarget != "" {
			// The fallback DB the .link points to is what manual copies always
			// miss; it gets its own archive area (design §4.2).
			zipPath = "fallback-db/" + sanitizeArchiveComponent(root.ID) + "/session-list.db"
			entry.HasFallbackDB = true
			sourceForFallbackCopy = linkTarget
		}
		if err := e.snapshotIntoZip(ctx, zw, tmpDir, root.ID, zipPath, sourceForFallbackCopy, e.SnapshotSessionDB, manifest); err != nil {
			return entry, err
		}
	}

	kanbanDB := filepath.Join(metaDir, filepath.FromSlash(kanbanDBRelPath))
	if fileExists(kanbanDB) {
		zipPath := archivePath + "/" + kanbanDBRelPath
		if err := e.snapshotIntoZip(ctx, zw, tmpDir, root.ID, zipPath, kanbanDB, e.SnapshotKanbanDB, manifest); err != nil {
			return entry, err
		}
	}
	return entry, nil
}

// snapshotIntoZip runs the snapshot function and streams the result into the
// archive; when the snapshot fails, it degrades to a plain file copy and
// flags the entry as best-effort (design §9).
func (e *Exporter) snapshotIntoZip(ctx context.Context, zw *zip.Writer, tmpDir, rootID, zipPath, fallbackSource string, snapshot func(context.Context, string, string) error, manifest *Manifest) error {
	if snapshot != nil {
		target := filepath.Join(tmpDir, sanitizeArchiveComponent(rootID)+"-"+sanitizeArchiveComponent(filepath.Base(zipPath))+fmt.Sprintf("-%d.db", time.Now().UnixNano()))
		if err := snapshot(ctx, rootID, target); err == nil {
			return copyFileToZip(zw, zipPath, target)
		}
	}
	if !fileExists(fallbackSource) {
		return nil
	}
	manifest.BestEffort = append(manifest.BestEffort, zipPath)
	return copyFileToZip(zw, zipPath, fallbackSource)
}

func (e *Exporter) exportUserConfig(zw *zip.Writer, includeCredentials bool) error {
	configDir := e.ConfigDir
	if _, err := os.Stat(configDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return filepath.WalkDir(configDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(configDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "logs" || (!includeCredentials && rel == credentialDir) {
				return filepath.SkipDir
			}
			return nil
		}
		base := filepath.Base(rel)
		if strings.Contains(base, ".log") || strings.HasSuffix(base, ".stderr") || strings.HasSuffix(base, ".pid") || strings.Contains(base, ".tmp-") {
			return nil
		}
		if !includeCredentials && credentialFiles[base] {
			return nil
		}
		return copyFileToZip(zw, "userconfig/"+rel, path)
	})
}

// skipMetaFile drops sqlite side files, temp files, and the DB files that are
// re-added as snapshots.
func skipMetaFile(rel string) bool {
	if rel == sessionDBRelPath || rel == kanbanDBRelPath {
		return true
	}
	if strings.HasSuffix(rel, "-journal") || strings.HasSuffix(rel, "-wal") || strings.HasSuffix(rel, "-shm") {
		return true
	}
	return strings.Contains(filepath.Base(rel), ".tmp-")
}

func readSessionDBLink(linkPath string) string {
	raw, err := os.ReadFile(linkPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func sanitizeArchiveComponent(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "root"
	}
	return b.String()
}

func copyFileToZip(zw *zip.Writer, zipPath, sourcePath string) error {
	file, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer file.Close()
	writer, err := zw.Create(zipPath)
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, file)
	return err
}

func writeZipBytes(zw *zip.Writer, zipPath string, payload []byte) error {
	writer, err := zw.Create(zipPath)
	if err != nil {
		return err
	}
	_, err = writer.Write(payload)
	return err
}
