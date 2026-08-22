package fs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"mindfs/server/internal/apperr"
	configpkg "mindfs/server/internal/config"
)

var ErrRootNameConflict = errors.New("root name already exists")

// RemovedRoot remembers a root the user deleted, so automatic project discovery
// does not add it back.
//
// Discovery reads the agents' own project histories, which never forget a
// directory. Without a tombstone, removing a project only held until the next
// discovery pass a minute later -- and always came back after a restart.
type RemovedRoot struct {
	RootPath  string    `json:"root_path"`
	RemovedAt time.Time `json:"removed_at"`
}

type Registry struct {
	mu    sync.Mutex
	path  string
	dirs  map[string]RootInfo
	order []string
	// Keyed by comparableRegistryKey, not by the path as typed, so a tombstone
	// still matches when discovery reports a different spelling of the same
	// directory.
	removed map[string]RemovedRoot
}

func NewRegistry(path string) *Registry {
	return &Registry{path: path, dirs: make(map[string]RootInfo), removed: make(map[string]RemovedRoot)}
}

func NewDefaultRegistry() (*Registry, error) {
	configDir, err := configpkg.MindFSConfigDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(configDir, "registry.json")
	return NewRegistry(path), nil
}

func (r *Registry) Load() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	payload, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return apperr.Wrap("read", r.path, err)
	}
	var stored struct {
		Dirs    []RootInfo    `json:"dirs"`
		Order   []string      `json:"order"`
		Removed []RemovedRoot `json:"removed"`
	}
	if err := json.Unmarshal(payload, &stored); err != nil {
		return err
	}
	r.dirs = make(map[string]RootInfo)
	r.order = nil
	r.removed = make(map[string]RemovedRoot)
	for _, entry := range stored.Removed {
		key := comparableRegistryKey(entry.RootPath)
		if key == "" {
			continue
		}
		r.removed[key] = entry
	}
	seen := make(map[string]struct{})
	for _, info := range stored.Dirs {
		name := info.Name
		if name == "" {
			name = filepath.Base(info.RootPath)
		}
		id := info.ID
		if id == "" {
			id = name
		}
		if id == "" || id == "." || id == string(filepath.Separator) {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		info.Name = name
		info.ID = id
		r.dirs[id] = info
		r.order = append(r.order, id)
		// A managed root outranks its own tombstone: the user must have added it
		// back by hand after removing it.
		delete(r.removed, comparableRegistryKey(info.RootPath))
	}
	return nil
}

func (r *Registry) Save() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saveLocked()
}

func (r *Registry) saveLocked() error {
	if r.path == "" {
		return errors.New("registry path required")
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return apperr.Wrap("mkdir", filepath.Dir(r.path), err)
	}
	recs := make([]RootInfo, 0, len(r.dirs))
	for _, id := range r.order {
		if dir, ok := r.dirs[id]; ok {
			recs = append(recs, dir)
		}
	}
	removed := make([]RemovedRoot, 0, len(r.removed))
	for _, entry := range r.removed {
		removed = append(removed, entry)
	}
	// Map iteration order would otherwise rewrite the file on every save.
	sort.Slice(removed, func(i, j int) bool { return removed[i].RootPath < removed[j].RootPath })
	payload, err := json.MarshalIndent(map[string]any{"dirs": recs, "order": r.order, "removed": removed}, "", "  ")
	if err != nil {
		return err
	}
	return apperr.Wrap("write", r.path, configpkg.WriteFileAtomic(r.path, payload, 0o644))
}

func (r *Registry) List() []RootInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]RootInfo, 0, len(r.order))
	for _, id := range r.order {
		if dir, ok := r.dirs[id]; ok {
			result = append(result, dir)
		}
	}
	return result
}

func (r *Registry) Get(id string) (RootInfo, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	dir, ok := r.dirs[id]
	return dir, ok
}

// Upsert adds a root the user asked for, clearing any tombstone left by an
// earlier removal.
func (r *Registry) Upsert(root string) (RootInfo, error) {
	return r.UpsertWithMetaLocation(root, MetaLocationProject)
}

func (r *Registry) UpsertWithMetaLocation(root, metaLocation string) (RootInfo, error) {
	if root == "" {
		return RootInfo{}, errors.New("root required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// Dropped before the upsert so both land in the same save, and put back if
	// the upsert fails -- otherwise a rejected add would silently un-tombstone
	// the path and let discovery resurrect it.
	key := comparableRegistryKey(root)
	previousRemoved, wasRemoved := r.removed[key]
	delete(r.removed, key)
	dir, err := r.upsertLocked(root, metaLocation)
	if err != nil && wasRemoved {
		r.removed[key] = previousRemoved
	}
	return dir, err
}

// UpsertDiscovered adds a root that automatic discovery found. The bool is
// false when the path carries a tombstone and was therefore skipped: a root the
// user removed stays removed, and only an explicit Upsert brings it back.
//
// Discovery goes through here rather than through UpsertWithMetaLocation so the
// tombstone check lives with the registry state it guards, where a caller cannot
// forget it.
func (r *Registry) UpsertDiscovered(root, metaLocation string) (RootInfo, bool, error) {
	if root == "" {
		return RootInfo{}, false, errors.New("root required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if key := comparableRegistryKey(root); key != "" {
		if _, ok := r.removed[key]; ok {
			return RootInfo{}, false, nil
		}
	}
	dir, err := r.upsertLocked(root, metaLocation)
	if err != nil {
		return RootInfo{}, false, err
	}
	return dir, true, nil
}

func (r *Registry) upsertLocked(root, metaLocation string) (RootInfo, error) {
	now := time.Now().UTC()
	name := filepath.Base(root)
	if name == "" || name == "." || name == string(filepath.Separator) {
		return RootInfo{}, errors.New("invalid directory name")
	}
	dir, ok := r.dirs[name]
	if !ok {
		metaLocation, err := NormalizeMetaLocation(metaLocation)
		if err != nil {
			return RootInfo{}, err
		}
		dir = NewRootInfo(name, name, root)
		dir.MetaLocation = metaLocation
		dir.CreatedAt = now
		r.order = append(r.order, name)
	} else if !sameRegistryPath(dir.RootPath, root) {
		return RootInfo{}, fmt.Errorf("%w: %q is already managed at %s; rename the directory before adding %s", ErrRootNameConflict, name, dir.RootPath, root)
	}
	dir.UpdatedAt = now
	r.dirs[name] = dir
	return dir, r.saveLocked()
}

func sameRegistryPath(a, b string) bool {
	left := cleanRegistryPath(a)
	right := cleanRegistryPath(b)
	if runtime.GOOS == "windows" {
		left = strings.ToLower(left)
		right = strings.ToLower(right)
	}
	return left == right
}

func cleanRegistryPath(path string) string {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if abs, err := filepath.Abs(cleaned); err == nil {
		return abs
	}
	return cleaned
}

func (r *Registry) Remove(root string) (RootInfo, error) {
	if root == "" {
		return RootInfo{}, errors.New("root required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cleaned := filepath.Clean(root)
	name := filepath.Base(cleaned)
	if name == "" || name == "." || name == string(filepath.Separator) {
		return RootInfo{}, errors.New("invalid directory name")
	}
	dir, ok := r.dirs[name]
	if !ok {
		return RootInfo{}, errors.New("root not found")
	}
	if filepath.Clean(dir.RootPath) != cleaned {
		return RootInfo{}, errors.New("root not found")
	}
	delete(r.dirs, name)
	nextOrder := make([]string, 0, len(r.order))
	for _, id := range r.order {
		if id != name {
			nextOrder = append(nextOrder, id)
		}
	}
	r.order = nextOrder
	// Tombstone the path so discovery does not add it back a minute later.
	if key := comparableRegistryKey(dir.RootPath); key != "" {
		if r.removed == nil {
			r.removed = make(map[string]RemovedRoot)
		}
		r.removed[key] = RemovedRoot{RootPath: dir.RootPath, RemovedAt: time.Now().UTC()}
	}
	if err := r.saveLocked(); err != nil {
		return RootInfo{}, err
	}
	return dir, nil
}

// RemovedRoots lists the tombstoned paths, newest removal first.
func (r *Registry) RemovedRoots() []RemovedRoot {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RemovedRoot, 0, len(r.removed))
	for _, entry := range r.removed {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RemovedAt.After(out[j].RemovedAt) })
	return out
}

// IsRemoved reports whether the path is tombstoned.
func (r *Registry) IsRemoved(root string) bool {
	key := comparableRegistryKey(root)
	if key == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.removed[key]
	return ok
}

// ForgetRemoved drops a tombstone without adding the root back, so the next
// discovery pass is free to pick the path up again.
func (r *Registry) ForgetRemoved(root string) (bool, error) {
	key := comparableRegistryKey(root)
	if key == "" {
		return false, errors.New("root required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.removed[key]; !ok {
		return false, nil
	}
	delete(r.removed, key)
	return true, r.saveLocked()
}

// comparableRegistryKey reduces a path to the form used to match tombstones
// against whatever spelling discovery reports.
//
// EvalSymlinks matters because DiscoverExternalProjectPaths resolves its paths;
// without it, a project reached through a symlink would be tombstoned under one
// name and rediscovered under another. It fails once the directory is gone, in
// which case the cleaned absolute path is the best key available -- and good
// enough, since a missing directory is not discoverable either.
func comparableRegistryKey(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	candidate := filepath.Clean(trimmed)
	if resolved, err := filepath.EvalSymlinks(candidate); err == nil && strings.TrimSpace(resolved) != "" {
		candidate = resolved
	}
	if abs, err := filepath.Abs(candidate); err == nil {
		candidate = abs
	}
	candidate = filepath.Clean(candidate)
	if runtime.GOOS == "windows" {
		candidate = strings.ToLower(candidate)
	}
	return candidate
}

func (r *Registry) Rename(id, name, rootPath string) (RootInfo, error) {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	if id == "" {
		return RootInfo{}, errors.New("root id required")
	}
	if name == "" {
		return RootInfo{}, errors.New("root name required")
	}
	if rootPath == "" {
		return RootInfo{}, errors.New("root required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	dir, ok := r.dirs[id]
	if !ok {
		return RootInfo{}, errors.New("root not found")
	}
	if existing, exists := r.dirs[name]; exists && existing.ID != id {
		return RootInfo{}, fmt.Errorf("%w: %q is already managed at %s", ErrRootNameConflict, name, existing.RootPath)
	}

	previousDirs := make(map[string]RootInfo, len(r.dirs))
	for key, value := range r.dirs {
		previousDirs[key] = value
	}
	previousOrder := append([]string(nil), r.order...)
	rollbackMeta, err := renameHomeMeta(dir, name, filepath.Clean(rootPath))
	if err != nil {
		return RootInfo{}, err
	}

	dir.ID = name
	dir.Name = name
	dir.RootPath = filepath.Clean(rootPath)
	dir.UpdatedAt = time.Now().UTC()
	// A rename can move the root onto a tombstoned path. Clearing it here keeps
	// "a managed path carries no tombstone" true at all times, rather than only
	// after the next Load.
	previousRemoved, wasRemoved := r.removed[comparableRegistryKey(dir.RootPath)]
	delete(r.removed, comparableRegistryKey(dir.RootPath))
	delete(r.dirs, id)
	r.dirs[name] = dir
	for i, item := range r.order {
		if item == id {
			r.order[i] = name
			break
		}
	}
	if err := r.saveLocked(); err != nil {
		r.dirs = previousDirs
		r.order = previousOrder
		if wasRemoved {
			r.removed[comparableRegistryKey(dir.RootPath)] = previousRemoved
		}
		rollbackMeta()
		return RootInfo{}, err
	}
	return dir, nil
}
