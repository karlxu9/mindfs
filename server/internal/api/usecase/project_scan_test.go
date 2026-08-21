package usecase

import (
	"context"
	"errors"
	"testing"

	rootfs "mindfs/server/internal/fs"
)

// scanTestRegistry reuses uploadTestRegistry for the parts of Registry this
// test does not care about, and overrides only listing and scanning.
type scanTestRegistry struct {
	uploadTestRegistry
	roots   []rootfs.RootInfo
	added   []rootfs.RootInfo
	skipped int
	err     error
	calls   int
}

func (r *scanTestRegistry) ListRoots() []rootfs.RootInfo { return r.roots }

func (r *scanTestRegistry) ScanProjectRoots() ([]rootfs.RootInfo, int, error) {
	r.calls++
	if r.err != nil {
		return nil, 0, r.err
	}
	r.roots = append(r.roots, r.added...)
	return r.added, r.skipped, nil
}

func TestScanManagedDirsReturnsAddedSkippedAndFullList(t *testing.T) {
	registry := &scanTestRegistry{
		roots:   []rootfs.RootInfo{{ID: "existing", RootPath: "/tmp/existing"}},
		added:   []rootfs.RootInfo{{ID: "found", RootPath: "/tmp/found"}},
		skipped: 2,
	}
	out, err := (&Service{Registry: registry}).ScanManagedDirs(context.Background())
	if err != nil {
		t.Fatalf("ScanManagedDirs: %v", err)
	}
	if len(out.Added) != 1 || out.Added[0].ID != "found" {
		t.Fatalf("Added = %#v", out.Added)
	}
	if out.SkippedRemoved != 2 {
		t.Fatalf("SkippedRemoved = %d, want 2", out.SkippedRemoved)
	}
	// Dirs is read after the scan so the caller can replace its list in one
	// round trip instead of refetching.
	if len(out.Dirs) != 2 {
		t.Fatalf("Dirs = %#v, want existing plus found", out.Dirs)
	}
	if registry.calls != 1 {
		t.Fatalf("scan calls = %d, want 1", registry.calls)
	}
}

func TestScanManagedDirsPropagatesScanError(t *testing.T) {
	registry := &scanTestRegistry{err: errors.New("discovery unavailable")}
	if _, err := (&Service{Registry: registry}).ScanManagedDirs(context.Background()); err == nil {
		t.Fatal("ScanManagedDirs returned no error")
	}
}

// A registry that cannot scan has to say so rather than reporting an empty
// scan, which would read as "no new projects".
func TestScanManagedDirsRejectsRegistryWithoutScanSupport(t *testing.T) {
	_, err := (&Service{Registry: uploadTestRegistry{}}).ScanManagedDirs(context.Background())
	if err == nil {
		t.Fatal("ScanManagedDirs returned no error for a registry without scan support")
	}
}
