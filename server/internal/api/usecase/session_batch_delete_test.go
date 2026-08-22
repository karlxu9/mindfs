package usecase

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	rootfs "mindfs/server/internal/fs"
	"mindfs/server/internal/session"
)

func newBatchDeleteFixture(t *testing.T) (context.Context, *session.Manager, Service, rootfs.RootInfo) {
	t.Helper()
	ctx := context.Background()
	rootDir := t.TempDir()
	root := rootfs.NewRootInfo("mindfs", "mindfs", rootDir)
	manager := newTestManager(t, root)
	registry := &commandTestRegistry{root: root, manager: manager}
	return ctx, manager, Service{Registry: registry}, root
}

func mustCreateSession(t *testing.T, ctx context.Context, manager *session.Manager, name, parentKey string) *session.Session {
	t.Helper()
	created, err := manager.Create(ctx, session.CreateInput{Type: session.TypeChat, Name: name, ParentSessionKey: parentKey})
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return created
}

// Selecting a parent and one of its children together must not delete the
// child's key twice, and the whole tree plus the independently selected
// session must be gone in one call.
func TestDeleteSessionsDeletesUnionWithoutDuplicates(t *testing.T) {
	ctx, manager, service, _ := newBatchDeleteFixture(t)
	parent := mustCreateSession(t, ctx, manager, "parent", "")
	child := mustCreateSession(t, ctx, manager, "child", parent.Key)
	grandchild := mustCreateSession(t, ctx, manager, "grandchild", child.Key)
	other := mustCreateSession(t, ctx, manager, "other", "")
	survivor := mustCreateSession(t, ctx, manager, "survivor", "")

	out, err := service.DeleteSessions(ctx, DeleteSessionsInput{
		RootID: "mindfs",
		Keys:   []string{parent.Key, child.Key, other.Key, parent.Key},
	})
	if err != nil {
		t.Fatalf("DeleteSessions: %v", err)
	}
	if len(out.Failed) != 0 {
		t.Fatalf("Failed = %#v, want none", out.Failed)
	}
	if len(out.Deleted) != 4 {
		t.Fatalf("Deleted = %#v, want 4 unique keys", out.Deleted)
	}
	seen := map[string]bool{}
	for _, key := range out.Deleted {
		if seen[key] {
			t.Fatalf("key %s deleted twice: %#v", key, out.Deleted)
		}
		seen[key] = true
	}
	for _, key := range []string{parent.Key, child.Key, grandchild.Key, other.Key} {
		if !seen[key] {
			t.Fatalf("key %s missing from Deleted: %#v", key, out.Deleted)
		}
		if _, err := manager.Get(ctx, key, 0); err == nil {
			t.Fatalf("session %s still exists", key)
		}
	}
	if _, err := manager.Get(ctx, survivor.Key, 0); err != nil {
		t.Fatalf("survivor should remain: %v", err)
	}
}

// Children must be deleted before their parents, so a crash mid-batch never
// leaves an orphan pointing at a removed parent.
func TestDeleteSessionsOrdersChildrenBeforeParents(t *testing.T) {
	ctx, manager, service, _ := newBatchDeleteFixture(t)
	parent := mustCreateSession(t, ctx, manager, "parent", "")
	child := mustCreateSession(t, ctx, manager, "child", parent.Key)

	out, err := service.DeleteSessions(ctx, DeleteSessionsInput{RootID: "mindfs", Keys: []string{parent.Key}})
	if err != nil {
		t.Fatalf("DeleteSessions: %v", err)
	}
	childIndex, parentIndex := -1, -1
	for i, key := range out.Deleted {
		if key == child.Key {
			childIndex = i
		}
		if key == parent.Key {
			parentIndex = i
		}
	}
	if childIndex == -1 || parentIndex == -1 || childIndex > parentIndex {
		t.Fatalf("Deleted order = %#v, want child before parent", out.Deleted)
	}
}

// A key that does not exist is reported per key; the rest of the batch still
// deletes. No all-or-nothing rollback.
func TestDeleteSessionsReportsMissingKeysAndContinues(t *testing.T) {
	ctx, manager, service, _ := newBatchDeleteFixture(t)
	real := mustCreateSession(t, ctx, manager, "real", "")

	out, err := service.DeleteSessions(ctx, DeleteSessionsInput{
		RootID: "mindfs",
		Keys:   []string{"no-such-session", real.Key},
	})
	if err != nil {
		t.Fatalf("DeleteSessions: %v", err)
	}
	if len(out.Failed) != 1 || out.Failed[0].Key != "no-such-session" || out.Failed[0].Error != "session not found" {
		t.Fatalf("Failed = %#v", out.Failed)
	}
	if len(out.Deleted) != 1 || out.Deleted[0] != real.Key {
		t.Fatalf("Deleted = %#v", out.Deleted)
	}
	if _, err := manager.Get(ctx, real.Key, 0); err == nil {
		t.Fatal("real session still exists")
	}
}

func TestDeleteSessionsRejectsEmptyKeyList(t *testing.T) {
	ctx, _, service, _ := newBatchDeleteFixture(t)
	if _, err := service.DeleteSessions(ctx, DeleteSessionsInput{RootID: "mindfs", Keys: []string{" ", ""}}); err == nil {
		t.Fatal("DeleteSessions accepted an empty key list")
	}
}

// The single-delete path now routes through the batch path; its externally
// observable error for a missing key must not change, because the HTTP
// handler maps it to 404.
func TestDeleteSessionStillReportsSessionNotFound(t *testing.T) {
	ctx, _, service, _ := newBatchDeleteFixture(t)
	err := service.DeleteSession(ctx, DeleteSessionInput{RootID: "mindfs", Key: "missing"})
	if err == nil || err.Error() != "session not found" {
		t.Fatalf("DeleteSession error = %v, want session not found", err)
	}
}

// Cascade deletion must clean up the per-session debug logs of children the
// caller never named, not just their DB rows (R-3.2).
func TestDeleteSessionsCascadeRemovesDebugLogs(t *testing.T) {
	ctx, manager, service, root := newBatchDeleteFixture(t)
	parent := mustCreateSession(t, ctx, manager, "parent", "")
	child := mustCreateSession(t, ctx, manager, "child", parent.Key)

	sessionsDir := filepath.Join(root.MetaDir(), "sessions")
	for _, key := range []string{parent.Key, child.Key} {
		if err := os.WriteFile(filepath.Join(sessionsDir, key+".debug.jsonl"), []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("write debug log for %s: %v", key, err)
		}
	}

	out, err := service.DeleteSessions(ctx, DeleteSessionsInput{RootID: "mindfs", Keys: []string{parent.Key}})
	if err != nil {
		t.Fatalf("DeleteSessions: %v", err)
	}
	if len(out.Failed) != 0 {
		t.Fatalf("Failed = %#v, want none", out.Failed)
	}
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		t.Fatalf("read sessions dir: %v", err)
	}
	for _, entry := range entries {
		for _, key := range []string{parent.Key, child.Key} {
			if strings.HasPrefix(entry.Name(), key) {
				t.Fatalf("session file %s left behind after cascade delete", entry.Name())
			}
		}
	}
}
