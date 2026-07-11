package indexing

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestBuildIndexAndCandidates(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.txt", "alpha needle omega\n")
	writeTestFile(t, root, "b.txt", "nothing here\n")
	indexPath := filepath.Join(t.TempDir(), "workspace.csearch")

	if err := BuildIndex(context.Background(), root, indexPath); err != nil {
		t.Fatalf("BuildIndex() error = %v", err)
	}
	files, err := IndexCandidates(indexPath, "needle", false, false)
	if err != nil {
		t.Fatalf("IndexCandidates() error = %v", err)
	}
	if len(files) != 1 || files[0] != filepath.Join(root, "a.txt") {
		t.Fatalf("candidates = %v", files)
	}
}

func TestWorkspaceFilesRespectsGitIgnore(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	writeTestFile(t, root, ".gitignore", "ignored.txt\n")
	writeTestFile(t, root, "tracked.txt", "tracked\n")
	writeTestFile(t, root, "untracked.txt", "untracked\n")
	writeTestFile(t, root, "ignored.txt", "ignored\n")
	runGit(t, root, "add", ".gitignore", "tracked.txt")

	files, err := WorkspaceFiles(context.Background(), root)
	if err != nil {
		t.Fatalf("WorkspaceFiles() error = %v", err)
	}
	if slices.Contains(files, filepath.Join(root, "ignored.txt")) {
		t.Fatalf("ignored file returned: %v", files)
	}
	for _, name := range []string{"tracked.txt", "untracked.txt"} {
		if !slices.Contains(files, filepath.Join(root, name)) {
			t.Fatalf("%s missing from %v", name, files)
		}
	}
}

func TestWorkspaceFilesRejectsTrackedSymlinkOutsideRoot(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "link.txt")

	files, err := WorkspaceFiles(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(files, link) || safeWorkspaceFile(root, link) {
		t.Fatalf("outside symlink accepted: %v", files)
	}
}

func TestManagerCandidatesIncludeFilesChangedSinceSnapshot(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	oldPath := filepath.Join(root, "old.txt")
	newPath := filepath.Join(root, "new.txt")
	writeTestFile(t, root, "old.txt", "old\n")
	runGit(t, root, "add", "old.txt")
	indexedAt := time.Now().UTC().Add(-time.Second)
	writeTestFile(t, root, "new.txt", "new\n")
	staleTime := indexedAt.Add(-time.Hour)
	if err := os.Chtimes(newPath, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(t.TempDir(), "workspace.csearch")
	writeTestFile(t, filepath.Dir(indexPath), filepath.Base(indexPath), "placeholder")
	dbPath := filepath.Join(t.TempDir(), "indexes.db")
	m := &Manager{root: root, indexPath: indexPath, dbPath: dbPath, runner: fakeRunner{files: []string{oldPath}}}
	if err := m.ensureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.setState(context.Background(), "ready", indexedAt, ""); err != nil {
		t.Fatal(err)
	}

	files, ok := m.Candidates(context.Background(), "new", false, false)
	if !ok || !slices.Contains(files, oldPath) || !slices.Contains(files, newPath) {
		t.Fatalf("Candidates() = %v, %v", files, ok)
	}
}

func TestManagerRefreshBuildsAtomicallyAndRecordsGeneration(t *testing.T) {
	root := t.TempDir()
	dir := t.TempDir()
	m := &Manager{
		root: root, indexPath: filepath.Join(dir, "workspace.csearch"),
		dbPath: filepath.Join(dir, "indexes.db"), runner: writingRunner{},
	}
	if err := m.ensureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.refreshAsync(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(m.indexPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("index was not atomically installed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	db, err := openIndexDB(m.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var status string
	var generation int
	if err := db.QueryRow(`SELECT status, generation FROM workspace_indexes WHERE root = ?`, root).Scan(&status, &generation); err != nil {
		t.Fatal(err)
	}
	if status != "ready" || generation != 1 {
		t.Fatalf("state = %q generation %d", status, generation)
	}
}

type fakeRunner struct {
	files []string
}

type writingRunner struct{}

func (writingRunner) Build(_ context.Context, _, target string) error {
	return os.WriteFile(target, []byte("index"), 0o600)
}

func (writingRunner) Candidates(context.Context, string, string, bool, bool) ([]string, error) {
	return nil, nil
}

func (f fakeRunner) Build(context.Context, string, string) error { return nil }
func (f fakeRunner) Candidates(context.Context, string, string, bool, bool) ([]string, error) {
	return slices.Clone(f.files), nil
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}
