package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestRegistry(t *testing.T) (*Registry, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.json")
	root := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	return NewRegistry(path), root
}

func TestUpsertDiscoveredSkipsRemovedRoot(t *testing.T) {
	registry, root := newTestRegistry(t)
	if _, err := registry.Upsert(root); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if _, err := registry.Remove(root); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	dir, added, err := registry.UpsertDiscovered(root, MetaLocationProject)
	if err != nil {
		t.Fatalf("UpsertDiscovered: %v", err)
	}
	if added {
		t.Fatalf("UpsertDiscovered added a removed root: %#v", dir)
	}
	if roots := registry.List(); len(roots) != 0 {
		t.Fatalf("roots = %#v, want none", roots)
	}
}

// The tombstone has to survive a reload, because the discovery pass that would
// add the project back runs on every start.
func TestRemovedRootStaysRemovedAcrossReload(t *testing.T) {
	registry, root := newTestRegistry(t)
	if _, err := registry.Upsert(root); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if _, err := registry.Remove(root); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	reloaded := NewRegistry(registry.path)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reloaded.IsRemoved(root) {
		t.Fatalf("IsRemoved(%q) = false after reload", root)
	}
	if _, added, err := reloaded.UpsertDiscovered(root, MetaLocationProject); err != nil || added {
		t.Fatalf("UpsertDiscovered after reload: added=%v err=%v", added, err)
	}
}

// Discovery reports whatever spelling the agent history holds, so a tombstone
// written for one spelling has to match the others.
func TestIsRemovedMatchesEquivalentPathSpellings(t *testing.T) {
	registry, root := newTestRegistry(t)
	if _, err := registry.Upsert(root); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if _, err := registry.Remove(root); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	variants := []string{
		root + string(filepath.Separator),
		filepath.Join(root, "."),
		filepath.Join(root, "sub", ".."),
	}
	for _, variant := range variants {
		if !registry.IsRemoved(variant) {
			t.Errorf("IsRemoved(%q) = false, want true", variant)
		}
	}
}

// An explicit add is the user asking for the project back, so it has to clear
// the tombstone -- otherwise the project would vanish again on the next reload.
func TestUpsertClearsTombstone(t *testing.T) {
	registry, root := newTestRegistry(t)
	if _, err := registry.Upsert(root); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if _, err := registry.Remove(root); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := registry.Upsert(root); err != nil {
		t.Fatalf("re-Upsert: %v", err)
	}
	if registry.IsRemoved(root) {
		t.Fatal("tombstone survived an explicit Upsert")
	}
	payload, err := os.ReadFile(registry.path)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	if strings.Contains(string(payload), "removed_at") {
		t.Fatalf("saved registry still holds a tombstone: %s", payload)
	}
}

func TestForgetRemovedAllowsRediscovery(t *testing.T) {
	registry, root := newTestRegistry(t)
	if _, err := registry.Upsert(root); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if _, err := registry.Remove(root); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	forgotten, err := registry.ForgetRemoved(root)
	if err != nil || !forgotten {
		t.Fatalf("ForgetRemoved = %v, err=%v", forgotten, err)
	}
	if _, added, err := registry.UpsertDiscovered(root, MetaLocationProject); err != nil || !added {
		t.Fatalf("UpsertDiscovered after forget: added=%v err=%v", added, err)
	}
	if again, err := registry.ForgetRemoved(root); err != nil || again {
		t.Fatalf("second ForgetRemoved = %v, err=%v, want false", again, err)
	}
}

// A root present in dirs outranks its own tombstone: the only way to be in both
// is to have been added back by hand, and the managed entry is the newer fact.
func TestLoadDropsTombstoneForManagedRoot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")
	root := filepath.Join(dir, "project")
	payload := `{
	  "dirs": [{"id": "project", "name": "project", "root_path": ` + jsonPath(root) + `}],
	  "order": ["project"],
	  "removed": [{"root_path": ` + jsonPath(root) + `, "removed_at": "2026-01-01T00:00:00Z"}]
	}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	registry := NewRegistry(path)
	if err := registry.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if registry.IsRemoved(root) {
		t.Fatal("managed root is still tombstoned after Load")
	}
	if len(registry.RemovedRoots()) != 0 {
		t.Fatalf("RemovedRoots = %#v, want none", registry.RemovedRoots())
	}
}

func jsonPath(path string) string {
	return `"` + strings.ReplaceAll(path, `\`, `\\`) + `"`
}

// A rejected add must not drop the tombstone: the path stays refused, and
// discovery must stay refused with it.
func TestFailedUpsertKeepsTombstone(t *testing.T) {
	dir := t.TempDir()
	registry := NewRegistry(filepath.Join(dir, "registry.json"))
	first := filepath.Join(dir, "a", "project")
	second := filepath.Join(dir, "b", "project")
	for _, path := range []string{first, second} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	if _, err := registry.Upsert(first); err != nil {
		t.Fatalf("Upsert first: %v", err)
	}
	if _, err := registry.Remove(first); err != nil {
		t.Fatalf("Remove first: %v", err)
	}
	// Occupy the name so adding the tombstoned path back is rejected.
	if _, err := registry.Upsert(second); err != nil {
		t.Fatalf("Upsert second: %v", err)
	}
	if _, err := registry.Upsert(first); err == nil {
		t.Fatal("Upsert of a conflicting name returned no error")
	}
	if !registry.IsRemoved(first) {
		t.Fatal("tombstone was dropped by a failed Upsert")
	}
}
