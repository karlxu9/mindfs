// Package projectscan adds project roots that MindFS can infer from the coding
// agents' own project histories.
//
// It lives outside server/app so the HTTP layer can run a scan on demand as
// well; server/app imports the API package, so the reverse direction would be
// an import cycle.
package projectscan

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mindfs/server/internal/agent"
	"mindfs/server/internal/fs"
	"mindfs/server/internal/gitview"
	"mindfs/server/internal/preferences"
)

// AutoScanEnvKey disables the periodic scan when set to a false-ish value.
//
// It is an environment variable rather than a flag because there are two server
// entry points with separate flag sets, and rather than a user preference
// because the loop starts before the HTTP layer that would edit one.
const AutoScanEnvKey = "MINDFS_PROJECT_AUTO_SCAN"

// Interval is how often the automatic scan runs.
const Interval = time.Minute

// Result reports what one scan pass did.
type Result struct {
	Added []fs.RootInfo
	// SkippedRemoved counts discovered paths the user had deleted. They are
	// reported so a manual scan can explain why a project the user expects is
	// still missing.
	SkippedRemoved int
}

// AutoScanEnabled reports whether the periodic scan should run.
func AutoScanEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(AutoScanEnvKey))) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

// Run adds every discoverable project root that is not already managed.
//
// Roots the user removed are skipped: the registry keeps a tombstone for them,
// and only an explicit add brings one back. Without that, deleting a project
// only held until the next pass, because the agents' histories never forget a
// directory.
func Run(registry *fs.Registry, prefs *preferences.Store) Result {
	var result Result
	if registry == nil {
		return result
	}
	existing := make(map[string]struct{})
	for _, root := range registry.List() {
		normalized := agent.NormalizeComparablePath(root.RootPath)
		if normalized != "" {
			existing[normalized] = struct{}{}
		}
	}
	for _, projectPath := range agent.DiscoverExternalProjectPaths() {
		normalized := agent.NormalizeComparablePath(projectPath)
		if normalized == "" {
			continue
		}
		if _, ok := existing[normalized]; ok {
			continue
		}
		// Checked before the metadata directory is created below, so a removed
		// project does not get a fresh .mindfs written on every pass.
		if registry.IsRemoved(projectPath) {
			result.SkippedRemoved++
			continue
		}
		if hasMindFSMetadataDir(projectPath) {
			continue
		}
		if agent.IsTemporaryWorkDir(projectPath) {
			continue
		}
		gitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		isWorktree, err := gitview.IsInsideWorktree(gitCtx, projectPath)
		cancel()
		if err == nil && isWorktree {
			continue
		}
		location := fs.MetaLocationProject
		if prefs != nil {
			location = prefs.NewProjectMetaLocation()
		}
		rootID := filepath.Base(filepath.Clean(projectPath))
		pending := fs.NewRootInfo(rootID, rootID, projectPath)
		pending.MetaLocation = location
		if _, err := pending.EnsureMetaDir(); err != nil {
			log.Printf("[projects] auto add metadata skipped path=%s err=%v", projectPath, err)
			continue
		}
		dir, added, err := registry.UpsertDiscovered(projectPath, location)
		if err != nil {
			log.Printf("[projects] auto add skipped path=%s err=%v", projectPath, err)
			continue
		}
		if !added {
			result.SkippedRemoved++
			continue
		}
		existing[normalized] = struct{}{}
		result.Added = append(result.Added, dir)
	}
	if len(result.Added) > 0 {
		log.Printf("[projects] auto added external project roots count=%d", len(result.Added))
	}
	return result
}

func hasMindFSMetadataDir(projectPath string) bool {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(projectPath, ".mindfs"))
	return err == nil && info.IsDir()
}

// StartLoop runs Run on a ticker until ctx is done. It does nothing when the
// automatic scan is disabled, leaving the manual scan endpoint as the only way
// to pick up new projects.
func StartLoop(ctx context.Context, registry *fs.Registry, prefs *preferences.Store) {
	if registry == nil {
		return
	}
	if !AutoScanEnabled() {
		log.Printf("[projects] auto scan disabled by %s", AutoScanEnvKey)
		return
	}
	ticker := time.NewTicker(Interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				Run(registry, prefs)
			}
		}
	}()
}
