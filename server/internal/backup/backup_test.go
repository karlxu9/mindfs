package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"mindfs/server/internal/fs"
)

// fixture builds a fake meta dir tree; the snapshot funcs write marker
// content so the test can tell snapshots from plain copies.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func markerSnapshot(marker string) func(context.Context, string, string) error {
	return func(_ context.Context, _ string, target string) error {
		return os.WriteFile(target, []byte(marker), 0o644)
	}
}

func archiveEntries(t *testing.T, payload []byte) map[string]string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	entries := map[string]string{}
	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open entry %s: %v", file.Name, err)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			t.Fatalf("read entry %s: %v", file.Name, err)
		}
		rc.Close()
		entries[file.Name] = buf.String()
	}
	return entries
}

func manifestFrom(t *testing.T, entries map[string]string) Manifest {
	t.Helper()
	var manifest Manifest
	if err := json.Unmarshal([]byte(entries["manifest.json"]), &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	return manifest
}

func TestExportProjectLayoutSnapshotsDBsAndKeepsJSONL(t *testing.T) {
	rootDir := t.TempDir()
	root := fs.NewRootInfo("proj", "proj", rootDir)
	meta := root.MetaDir()
	writeFile(t, filepath.Join(meta, "sessions", "session-list.db"), "live-db")
	writeFile(t, filepath.Join(meta, "sessions", "abc.jsonl"), "{\"role\":\"user\"}\n")
	writeFile(t, filepath.Join(meta, "sessions", "abc.aux.jsonl"), "{}\n")
	writeFile(t, filepath.Join(meta, "sessions", "session-list.db-journal"), "journal")
	writeFile(t, filepath.Join(meta, "tasks", "task-kanban.db"), "live-kanban")
	writeFile(t, filepath.Join(meta, "commands", "history.db"), "history")

	exporter := &Exporter{
		Roots:             []fs.RootInfo{root},
		SnapshotSessionDB: markerSnapshot("session-snapshot"),
		SnapshotKanbanDB:  markerSnapshot("kanban-snapshot"),
	}
	var buf bytes.Buffer
	manifest, err := exporter.Export(context.Background(), &buf, ExportInput{Scope: "all", IncludeCredentials: true, Version: "test"})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	entries := archiveEntries(t, buf.Bytes())

	if entries["roots/proj/sessions/session-list.db"] != "session-snapshot" {
		t.Fatalf("session db should come from the snapshot, got %q", entries["roots/proj/sessions/session-list.db"])
	}
	if entries["roots/proj/tasks/task-kanban.db"] != "kanban-snapshot" {
		t.Fatalf("kanban db should come from the snapshot, got %q", entries["roots/proj/tasks/task-kanban.db"])
	}
	if entries["roots/proj/sessions/abc.jsonl"] == "" || entries["roots/proj/sessions/abc.aux.jsonl"] == "" {
		t.Fatal("jsonl files missing from archive")
	}
	if _, ok := entries["roots/proj/sessions/session-list.db-journal"]; ok {
		t.Fatal("journal file must not be archived")
	}
	if entries["roots/proj/commands/history.db"] != "history" {
		t.Fatal("history.db should be plain-copied")
	}
	if _, ok := entries["RESTORE.md"]; !ok {
		t.Fatal("RESTORE.md missing")
	}

	decoded := manifestFrom(t, entries)
	if decoded.FormatVersion != FormatVersion || !decoded.IncludesCredentials || decoded.MindFSVersion != "test" {
		t.Fatalf("manifest header wrong: %+v", decoded)
	}
	if len(decoded.Roots) != 1 || decoded.Roots[0].RootID != "proj" || decoded.Roots[0].HasFallbackDB {
		t.Fatalf("manifest roots wrong: %+v", decoded.Roots)
	}
	sort.Strings(decoded.BestEffort)
	if len(decoded.BestEffort) != 1 || decoded.BestEffort[0] != "roots/proj/commands/history.db" {
		t.Fatalf("best effort = %v, want only history.db", decoded.BestEffort)
	}
	if manifest.Roots[0].MetaLocation != decoded.Roots[0].MetaLocation {
		t.Fatal("returned manifest differs from embedded one")
	}
}

func TestExportLinkFallbackLayout(t *testing.T) {
	rootDir := t.TempDir()
	root := fs.NewRootInfo("with link!", "linked", rootDir)
	meta := root.MetaDir()
	fallbackDB := filepath.Join(t.TempDir(), "fallback.db")
	writeFile(t, fallbackDB, "fallback-live")
	writeFile(t, filepath.Join(meta, "sessions", "session-list.db.link"), fallbackDB+"\n")
	writeFile(t, filepath.Join(meta, "sessions", "abc.jsonl"), "{}\n")

	exporter := &Exporter{
		Roots:             []fs.RootInfo{root},
		SnapshotSessionDB: markerSnapshot("fallback-snapshot"),
	}
	var buf bytes.Buffer
	_, err := exporter.Export(context.Background(), &buf, ExportInput{Scope: "all", IncludeCredentials: true})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	entries := archiveEntries(t, buf.Bytes())

	if entries["fallback-db/with_link_/session-list.db"] != "fallback-snapshot" {
		keys := make([]string, 0, len(entries))
		for k := range entries {
			keys = append(keys, k)
		}
		t.Fatalf("fallback snapshot missing; entries: %v", keys)
	}
	if _, ok := entries["roots/with link!/sessions/session-list.db"]; ok {
		t.Fatal("link layout must not produce an in-root session db")
	}
	if entries["roots/with link!/sessions/session-list.db.link"] == "" {
		t.Fatal("the .link pointer itself should stay in the archive")
	}
	decoded := manifestFrom(t, entries)
	if !decoded.Roots[0].HasFallbackDB {
		t.Fatal("manifest should flag has_fallback_db")
	}
}

func TestExportSnapshotFailureDegradesToCopy(t *testing.T) {
	rootDir := t.TempDir()
	root := fs.NewRootInfo("proj", "proj", rootDir)
	writeFile(t, filepath.Join(root.MetaDir(), "sessions", "session-list.db"), "live-db")

	exporter := &Exporter{
		Roots: []fs.RootInfo{root},
		SnapshotSessionDB: func(context.Context, string, string) error {
			return errors.New("vacuum failed")
		},
	}
	var buf bytes.Buffer
	_, err := exporter.Export(context.Background(), &buf, ExportInput{Scope: "all", IncludeCredentials: true})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	entries := archiveEntries(t, buf.Bytes())
	if entries["roots/proj/sessions/session-list.db"] != "live-db" {
		t.Fatal("failed snapshot should degrade to a plain copy")
	}
	decoded := manifestFrom(t, entries)
	found := false
	for _, item := range decoded.BestEffort {
		if item == "roots/proj/sessions/session-list.db" {
			found = true
		}
	}
	if !found {
		t.Fatalf("degraded copy not flagged best-effort: %v", decoded.BestEffort)
	}
}

func TestExportUserConfigCredentialToggle(t *testing.T) {
	configDir := t.TempDir()
	writeFile(t, filepath.Join(configDir, "registry.json"), "{}")
	writeFile(t, filepath.Join(configDir, "preferences.json"), "{}")
	writeFile(t, filepath.Join(configDir, "credentials.json"), "secret")
	writeFile(t, filepath.Join(configDir, "agents-env.json"), "secret")
	writeFile(t, filepath.Join(configDir, "autostart-environment.json"), "secret")
	writeFile(t, filepath.Join(configDir, "local-cli-tokens.json"), "secret")
	writeFile(t, filepath.Join(configDir, "e2ee.json"), "secret")
	writeFile(t, filepath.Join(configDir, "key.pem"), "secret")
	writeFile(t, filepath.Join(configDir, "agents-config", "manifest.json"), "backup")
	writeFile(t, filepath.Join(configDir, "logs", "mindfs-x.log"), "log line")
	writeFile(t, filepath.Join(configDir, "mindfs-127_0_0_1_7331.pid"), "1234")

	for _, includeCredentials := range []bool{false, true} {
		exporter := &Exporter{ConfigDir: configDir}
		var buf bytes.Buffer
		if _, err := exporter.Export(context.Background(), &buf, ExportInput{Scope: "all", IncludeCredentials: includeCredentials}); err != nil {
			t.Fatalf("export(include=%v): %v", includeCredentials, err)
		}
		entries := archiveEntries(t, buf.Bytes())
		if entries["userconfig/registry.json"] == "" || entries["userconfig/preferences.json"] == "" {
			t.Fatalf("data files missing (include=%v)", includeCredentials)
		}
		if _, ok := entries["userconfig/logs/mindfs-x.log"]; ok {
			t.Fatal("log files must never be archived")
		}
		if _, ok := entries["userconfig/mindfs-127_0_0_1_7331.pid"]; ok {
			t.Fatal("pid files must never be archived")
		}
		credentialEntries := []string{
			"userconfig/credentials.json",
			"userconfig/agents-env.json",
			"userconfig/autostart-environment.json",
			"userconfig/local-cli-tokens.json",
			"userconfig/e2ee.json",
			"userconfig/key.pem",
			"userconfig/agents-config/manifest.json",
		}
		for _, name := range credentialEntries {
			_, ok := entries[name]
			if includeCredentials && !ok {
				t.Fatalf("credential entry %s missing when included", name)
			}
			if !includeCredentials && ok {
				t.Fatalf("credential entry %s leaked when excluded", name)
			}
		}
	}
}

func TestExportScopeRootSelectsSingleRoot(t *testing.T) {
	rootA := fs.NewRootInfo("a", "a", t.TempDir())
	rootB := fs.NewRootInfo("b", "b", t.TempDir())
	writeFile(t, filepath.Join(rootA.MetaDir(), "sessions", "a.jsonl"), "{}\n")
	writeFile(t, filepath.Join(rootB.MetaDir(), "sessions", "b.jsonl"), "{}\n")

	exporter := &Exporter{Roots: []fs.RootInfo{rootA, rootB}}
	var buf bytes.Buffer
	manifest, err := exporter.Export(context.Background(), &buf, ExportInput{Scope: "root", RootID: "b", IncludeCredentials: true})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	entries := archiveEntries(t, buf.Bytes())
	if _, ok := entries["roots/a/sessions/a.jsonl"]; ok {
		t.Fatal("scope=root must not include other roots")
	}
	if _, ok := entries["roots/b/sessions/b.jsonl"]; !ok {
		t.Fatal("selected root missing")
	}
	if len(manifest.Roots) != 1 || manifest.Roots[0].RootID != "b" {
		t.Fatalf("manifest roots = %+v", manifest.Roots)
	}

	if _, err := exporter.Export(context.Background(), io.Discard, ExportInput{Scope: "root", RootID: "missing"}); err == nil {
		t.Fatal("unknown root must fail")
	}
	if _, err := exporter.Export(context.Background(), io.Discard, ExportInput{Scope: "everything"}); err == nil {
		t.Fatal("invalid scope must fail")
	}
}
