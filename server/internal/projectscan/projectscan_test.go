package projectscan

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mindfs/server/internal/agent"
	"mindfs/server/internal/fs"
)

func TestRunSkipsGitWorktrees(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found")
	}
	workspace := t.TempDir()
	mainRoot := filepath.Join(workspace, "mindfs")
	mkdirAll(t, mainRoot)
	runScanTestGit(t, mainRoot, "init")
	runScanTestGit(t, mainRoot, "config", "user.email", "test@example.com")
	runScanTestGit(t, mainRoot, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(mainRoot, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runScanTestGit(t, mainRoot, "add", "README.md")
	runScanTestGit(t, mainRoot, "commit", "-m", "initial")
	runScanTestGit(t, mainRoot, "checkout", "-b", "task-1")
	runScanTestGit(t, mainRoot, "checkout", "-")
	worktreeRoot := filepath.Join(mainRoot, ".worktree", "task-1")
	runScanTestGit(t, mainRoot, "worktree", "add", worktreeRoot, "task-1")
	worktreeSubdir := filepath.Join(worktreeRoot, "src")
	mkdirAll(t, worktreeSubdir)

	codexHome := filepath.Join(workspace, "codex-home")
	mkdirAll(t, codexHome)
	globalState := map[string]any{
		"project-order": []string{mainRoot, worktreeRoot, worktreeSubdir},
	}
	payload, err := json.Marshal(globalState)
	if err != nil {
		t.Fatalf("marshal global state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, ".codex-global-state.json"), payload, 0o644); err != nil {
		t.Fatalf("write global state: %v", err)
	}
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("HOME", filepath.Join(workspace, "home"))
	// Windows: os.UserHomeDir reads USERPROFILE, not HOME.
	t.Setenv("USERPROFILE", filepath.Join(workspace, "home"))
	// Run drops any path under os.TempDir, and workspace
	// comes from t.TempDir -- so the temp dir has to be moved out of the way or
	// mainRoot gets filtered out. os.TempDir reads TMPDIR on POSIX but TMP/TEMP
	// on Windows, hence all three.
	tempDir := filepath.Join(workspace, "tmp")
	mkdirAll(t, tempDir)
	t.Setenv("TMPDIR", tempDir)
	t.Setenv("TMP", tempDir)
	t.Setenv("TEMP", tempDir)

	registry := fs.NewRegistry(filepath.Join(workspace, "registry.json"))
	Run(registry, nil)

	roots := registry.List()
	if len(roots) != 1 {
		t.Fatalf("roots len = %d, want 1: %#v", len(roots), roots)
	}
	if agent.NormalizeComparablePath(roots[0].RootPath) != agent.NormalizeComparablePath(mainRoot) {
		t.Fatalf("root path = %q, want %q", roots[0].RootPath, mainRoot)
	}
	if strings.Contains(roots[0].RootPath, ".worktree") {
		t.Fatalf("worktree was added as root: %#v", roots[0])
	}
}

func runScanTestGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out)
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

// setupDiscoverableProject makes projectPath show up in DiscoverExternalProjectPaths
// by writing a fake Codex project history, and moves the temp dir out of the
// way so Run does not filter the project out as scratch space.
func setupDiscoverableProject(t *testing.T, workspace string, projectPaths ...string) {
	t.Helper()
	codexHome := filepath.Join(workspace, "codex-home")
	mkdirAll(t, codexHome)
	payload, err := json.Marshal(map[string]any{"project-order": projectPaths})
	if err != nil {
		t.Fatalf("marshal global state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, ".codex-global-state.json"), payload, 0o644); err != nil {
		t.Fatalf("write global state: %v", err)
	}
	t.Setenv("CODEX_HOME", codexHome)
	home := filepath.Join(workspace, "home")
	mkdirAll(t, home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	tempDir := filepath.Join(workspace, "tmp")
	mkdirAll(t, tempDir)
	t.Setenv("TMPDIR", tempDir)
	t.Setenv("TMP", tempDir)
	t.Setenv("TEMP", tempDir)
}

func TestRunAddsDiscoveredProject(t *testing.T) {
	workspace := t.TempDir()
	project := filepath.Join(workspace, "project")
	mkdirAll(t, project)
	setupDiscoverableProject(t, workspace, project)

	registry := fs.NewRegistry(filepath.Join(workspace, "registry.json"))
	result := Run(registry, nil)
	if len(result.Added) != 1 {
		t.Fatalf("Added = %#v, want the discovered project", result.Added)
	}
	if result.SkippedRemoved != 0 {
		t.Fatalf("SkippedRemoved = %d, want 0", result.SkippedRemoved)
	}
}

// The whole point of the tombstone: discovery reads the agents' histories,
// which never forget a directory, so a deleted project would otherwise come
// back on the next pass.
func TestRunSkipsProjectTheUserRemoved(t *testing.T) {
	workspace := t.TempDir()
	project := filepath.Join(workspace, "project")
	mkdirAll(t, project)
	setupDiscoverableProject(t, workspace, project)

	registry := fs.NewRegistry(filepath.Join(workspace, "registry.json"))
	if _, err := registry.Upsert(project); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if _, err := registry.Remove(project); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	result := Run(registry, nil)
	if len(result.Added) != 0 {
		t.Fatalf("Added = %#v, want none", result.Added)
	}
	if result.SkippedRemoved != 1 {
		t.Fatalf("SkippedRemoved = %d, want 1", result.SkippedRemoved)
	}
	if roots := registry.List(); len(roots) != 0 {
		t.Fatalf("roots = %#v, want none", roots)
	}
	// Skipped before EnsureMetaDir, so the pass leaves no trace in a project the
	// user has already thrown away.
	if _, err := os.Stat(filepath.Join(project, ".mindfs")); !os.IsNotExist(err) {
		t.Fatalf("stat .mindfs err = %v, want not-exist", err)
	}
}

func TestAutoScanEnabledReadsEnv(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  bool
	}{
		{"", true},
		{"1", true},
		{"true", true},
		{"anything", true},
		{"0", false},
		{"false", false},
		{"FALSE", false},
		{" off ", false},
		{"no", false},
	} {
		t.Run("value="+tt.value, func(t *testing.T) {
			t.Setenv(AutoScanEnvKey, tt.value)
			if got := AutoScanEnabled(); got != tt.want {
				t.Fatalf("AutoScanEnabled() with %q = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
