package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mindfs/server/internal/fs"
	"mindfs/server/internal/notify"
	"mindfs/server/internal/session"
)

func TestNextSessionWorktreeNameUsesDateAndNextSequence(t *testing.T) {
	now := time.Date(2026, time.July, 30, 15, 30, 0, 0, time.Local)
	got := nextSessionWorktreeName(now, []string{
		"task-12",
		"session-0729-09",
		"session-0730-01",
		"session-0730-03",
		"session-0730-invalid",
	})
	if got != "session-0730-04" {
		t.Fatalf("nextSessionWorktreeName() = %q, want %q", got, "session-0730-04")
	}
}

func TestNextSessionWorktreeNameStartsAtOne(t *testing.T) {
	now := time.Date(2026, time.July, 30, 15, 30, 0, 0, time.Local)
	if got := nextSessionWorktreeName(now, nil); got != "session-0730-01" {
		t.Fatalf("nextSessionWorktreeName() = %q, want %q", got, "session-0730-01")
	}
}

func TestListRootsRemovesDeletedManagedDirWithoutRecreatingIt(t *testing.T) {
	parent := t.TempDir()
	registry := fs.NewRegistry(filepath.Join(parent, "registry.json"))
	projectPath := filepath.Join(parent, "project")
	if err := os.Mkdir(projectPath, 0o755); err != nil {
		t.Fatalf("Mkdir project returned error: %v", err)
	}
	if _, err := registry.Upsert(projectPath); err != nil {
		t.Fatalf("Upsert returned error: %v", err)
	}
	if err := os.RemoveAll(projectPath); err != nil {
		t.Fatalf("RemoveAll project returned error: %v", err)
	}

	app := &AppContext{Dirs: registry}
	if roots := app.ListRoots(); len(roots) != 0 {
		t.Fatalf("ListRoots returned %d roots, want 0", len(roots))
	}
	if roots := registry.List(); len(roots) != 0 {
		t.Fatalf("registry still has %d roots, want 0", len(roots))
	}
	if _, err := os.Stat(projectPath); !os.IsNotExist(err) {
		t.Fatalf("deleted project was recreated or stat failed unexpectedly: %v", err)
	}
}

func TestGetRootContextRemovesDeletedManagedDirWithoutRecreatingIt(t *testing.T) {
	parent := t.TempDir()
	registry := fs.NewRegistry(filepath.Join(parent, "registry.json"))
	projectPath := filepath.Join(parent, "project")
	if err := os.Mkdir(projectPath, 0o755); err != nil {
		t.Fatalf("Mkdir project returned error: %v", err)
	}
	dir, err := registry.Upsert(projectPath)
	if err != nil {
		t.Fatalf("Upsert returned error: %v", err)
	}
	if err := os.RemoveAll(projectPath); err != nil {
		t.Fatalf("RemoveAll project returned error: %v", err)
	}

	app := &AppContext{Dirs: registry}
	if _, err := app.GetSessionManager(dir.ID); err == nil {
		t.Fatal("GetSessionManager returned nil error for deleted root")
	}
	if roots := registry.List(); len(roots) != 0 {
		t.Fatalf("registry still has %d roots, want 0", len(roots))
	}
	if _, err := os.Stat(projectPath); !os.IsNotExist(err) {
		t.Fatalf("deleted project was recreated or stat failed unexpectedly: %v", err)
	}
}

// BroadcastSessionError must fan out a session.error notification for normal
// runs and stay silent for scheduled runs, which already push scheduled.failed
// (R-2.3).
func TestBroadcastSessionErrorNotifiesAndExemptsScheduled(t *testing.T) {
	var gotEventIDs []string
	var gotPayloads []notify.Payload
	app := &AppContext{notifyPayloadOverride: func(eventID string, payload notify.Payload) {
		gotEventIDs = append(gotEventIDs, eventID)
		gotPayloads = append(gotPayloads, payload)
	}}

	app.BroadcastSessionError("root1", "sess1", "boom", "req-42")
	if len(gotPayloads) != 1 {
		t.Fatalf("notifications after user-run error = %d, want 1", len(gotPayloads))
	}
	if gotEventIDs[0] != "req-42" {
		t.Fatalf("eventID = %q, want req-42", gotEventIDs[0])
	}
	if gotPayloads[0].Type != "session.error" {
		t.Fatalf("payload type = %q, want session.error", gotPayloads[0].Type)
	}
	if gotPayloads[0].Body != "boom" {
		t.Fatalf("payload body = %q, want boom", gotPayloads[0].Body)
	}

	// Empty request IDs (e.g. kanban stage runs) still notify, with a stable
	// fallback event ID.
	app.BroadcastSessionError("root1", "sess1", "boom", "")
	if len(gotPayloads) != 2 {
		t.Fatalf("notifications after kanban-run error = %d, want 2", len(gotPayloads))
	}
	if !strings.HasPrefix(gotEventIDs[1], "session.error:root1:sess1:") {
		t.Fatalf("fallback eventID = %q", gotEventIDs[1])
	}

	// Scheduled runs are exempt: scheduled.failed already covers them.
	app.BroadcastSessionError("root1", "sess1", "boom", "scheduled:task-1")
	if len(gotPayloads) != 2 {
		t.Fatalf("notifications after scheduled error = %d, want 2 (exempt)", len(gotPayloads))
	}
}

// AppContext.Close must release every root's sqlite handle so the meta
// directory can be deleted right after shutdown (only observable on Windows,
// which refuses to unlink open files).
func TestAppContextCloseReleasesAllRootResources(t *testing.T) {
	rootDir := t.TempDir()
	root := fs.NewRootInfo("root-1", "root-1", rootDir)
	manager := session.NewManager(root)
	if _, err := manager.Create(context.Background(), session.CreateInput{Type: session.TypeChat, Name: "open-db"}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	app := &AppContext{roots: map[string]*RootContext{
		"root-1": {Root: root, Session: manager},
	}}
	if err := app.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if err := os.RemoveAll(root.MetaDir()); err != nil {
		t.Fatalf("meta dir still locked after Close: %v", err)
	}
	app.mu.RLock()
	defer app.mu.RUnlock()
	if len(app.roots) != 0 {
		t.Fatalf("roots map not drained: %d entries left", len(app.roots))
	}
}
