// Package indexing provides language-neutral workspace text indexing.
package indexing

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"
	"time"

	"github.com/google/codesearch/index"
	"github.com/samcharles93/tau/internal/config"

	_ "modernc.org/sqlite"
)

const (
	maxCandidateFiles = 2000
	maxCandidateArgBytes = 24 * 1024
	buildTimeout      = 10 * time.Minute
	metadataTimeout   = 5 * time.Second
)

// CandidateProvider returns files which may match a grep query. A false
// handled result tells the caller to use its normal unindexed search path.
type CandidateProvider interface {
	Candidates(context.Context, string, bool, bool) ([]string, bool)
}

// Manager owns the workspace index lifecycle. The codesearch implementation
// runs in a child Tau process so upstream fatal exits and mmap lifetime cannot
// terminate or leak inside the interactive agent process.
type Manager struct {
	root      string
	indexPath string
	dbPath    string
	runner    helperRunner
}

type helperRunner interface {
	Build(context.Context, string, string) error
	Candidates(context.Context, string, string, bool, bool) ([]string, error)
}

type processRunner struct{}

// NewManager prepares lifecycle storage and starts an asynchronous atomic
// refresh. An existing index remains available while the refresh runs.
func NewManager(ctx context.Context, root string) (*Manager, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	if err := os.MkdirAll(config.WorkspaceIndexDir(), 0o700); err != nil {
		return nil, fmt.Errorf("create workspace index directory: %w", err)
	}
	m := &Manager{
		root:      filepath.Clean(absRoot),
		indexPath: filepath.Join(config.WorkspaceIndexDir(), workspaceKey(absRoot)+".csearch"),
		dbPath:    config.WorkspaceIndexDBPath(),
		runner:    processRunner{},
	}
	if err := m.ensureSchema(ctx); err != nil {
		return nil, err
	}
	m.refreshAsync(ctx)
	return m, nil
}

// Candidates implements the tools.GrepIndex contract.
func (m *Manager) Candidates(ctx context.Context, pattern string, literal, caseSensitive bool) ([]string, bool) {
	state, err := m.state(ctx)
	if err != nil || state.IndexedAt.IsZero() {
		return nil, false
	}
	if _, err := os.Stat(m.indexPath); err != nil {
		return nil, false
	}
	files, err := m.runner.Candidates(ctx, m.indexPath, pattern, literal, caseSensitive)
	if err != nil {
		return nil, false
	}

	// The sidecar is a snapshot. Include files changed or created since the
	// build began so indexed search cannot miss live workspace edits.
	workspaceFiles, err := WorkspaceFiles(ctx, m.root)
	if err != nil {
		return nil, false
	}
	changedFiles := changedGitFiles(ctx, m.root)
	seen := make(map[string]struct{}, len(files))
	merged := make([]string, 0, len(files))
	argumentBytes := 0
	add := func(path string) {
		path = filepath.Clean(path)
		if !safeWorkspaceFile(m.root, path) {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		merged = append(merged, path)
		argumentBytes += len(path) + 1
	}
	for _, path := range files {
		add(path)
	}
	for _, path := range workspaceFiles {
		info, statErr := os.Stat(path)
		_, changed := changedFiles[path]
		if statErr == nil && (changed || !info.ModTime().Before(state.IndexedAt)) {
			add(path)
		}
	}
	if len(merged) == 0 || len(merged) > maxCandidateFiles || argumentBytes > maxCandidateArgBytes {
		return nil, false
	}
	slices.Sort(merged)
	return merged, true
}

func changedGitFiles(ctx context.Context, root string) map[string]struct{} {
	changed := map[string]struct{}{}
	commands := [][]string{
		{"-C", root, "ls-files", "-m", "-o", "--exclude-standard", "-z"},
		{"-C", root, "diff", "--cached", "--name-only", "--diff-filter=ACMR", "-z"},
	}
	for _, args := range commands {
		out, err := exec.CommandContext(ctx, "git", args...).Output()
		if err != nil {
			return map[string]struct{}{}
		}
		for _, name := range strings.Split(string(out), "\x00") {
			if name != "" {
				changed[filepath.Clean(filepath.Join(root, filepath.FromSlash(name)))] = struct{}{}
			}
		}
	}
	return changed
}

type indexState struct {
	IndexedAt time.Time
}

func (m *Manager) refreshAsync(parent context.Context) {
	go func() {
		ctx, cancel := context.WithTimeout(parent, buildTimeout)
		defer cancel()
		startedAt := time.Now().UTC()
		claimed, err := m.claimBuild(ctx, startedAt)
		if err != nil || !claimed {
			return
		}
		tmp := fmt.Sprintf("%s.%d.tmp", m.indexPath, os.Getpid())
		_ = os.Remove(tmp)
		if err := m.runner.Build(ctx, m.root, tmp); err != nil {
			_ = os.Remove(tmp)
			m.finishBuild(parent, "error", time.Time{}, err.Error())
			return
		}
		if err := os.Rename(tmp, m.indexPath); err != nil {
			_ = os.Remove(tmp)
			m.finishBuild(parent, "error", time.Time{}, err.Error())
			return
		}
		m.finishBuild(parent, "ready", startedAt, "")
	}()
}

func (m *Manager) finishBuild(parent context.Context, status string, indexedAt time.Time, lastError string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), metadataTimeout)
	defer cancel()
	_ = m.setState(ctx, status, indexedAt, lastError)
}

func (m *Manager) claimBuild(ctx context.Context, startedAt time.Time) (bool, error) {
	db, err := openIndexDB(m.dbPath)
	if err != nil {
		return false, err
	}
	defer db.Close()
	cutoff := startedAt.Add(-buildTimeout).Format(time.RFC3339Nano)
	result, err := db.ExecContext(ctx, `INSERT INTO workspace_indexes (root, index_path, status, indexed_at, build_started_at, last_error, updated_at)
		VALUES (?, ?, 'building', '', ?, '', ?)
		ON CONFLICT(root) DO UPDATE SET index_path=excluded.index_path, status='building',
		build_started_at=excluded.build_started_at, last_error='', updated_at=excluded.updated_at
		WHERE workspace_indexes.status != 'building' OR workspace_indexes.updated_at < ?`,
		m.root, m.indexPath, startedAt.Format(time.RFC3339Nano), startedAt.Format(time.RFC3339Nano), cutoff)
	if err != nil {
		return false, fmt.Errorf("claim workspace index build: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("inspect workspace index build claim: %w", err)
	}
	return rows > 0, nil
}

func (m *Manager) ensureSchema(ctx context.Context) error {
	db, err := openIndexDB(m.dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS workspace_indexes (
		root TEXT PRIMARY KEY,
		index_path TEXT NOT NULL,
		status TEXT NOT NULL,
		format_version TEXT NOT NULL DEFAULT 'codesearch-v2',
		generation INTEGER NOT NULL DEFAULT 0,
		indexed_at TEXT NOT NULL DEFAULT '',
		build_started_at TEXT NOT NULL DEFAULT '',
		last_error TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("create workspace index metadata: %w", err)
	}
	for _, migration := range []string{
		`ALTER TABLE workspace_indexes ADD COLUMN format_version TEXT NOT NULL DEFAULT 'codesearch-v2'`,
		`ALTER TABLE workspace_indexes ADD COLUMN generation INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE workspace_indexes ADD COLUMN build_started_at TEXT NOT NULL DEFAULT ''`,
	} {
		if _, migrationErr := db.ExecContext(ctx, migration); migrationErr != nil && !strings.Contains(migrationErr.Error(), "duplicate column name") {
			return fmt.Errorf("migrate workspace index metadata: %w", migrationErr)
		}
	}
	return nil
}

func (m *Manager) state(ctx context.Context) (indexState, error) {
	db, err := openIndexDB(m.dbPath)
	if err != nil {
		return indexState{}, err
	}
	defer db.Close()
	var indexed string
	err = db.QueryRowContext(ctx, `SELECT indexed_at FROM workspace_indexes WHERE root = ?`, m.root).Scan(&indexed)
	if errors.Is(err, sql.ErrNoRows) {
		return indexState{}, nil
	}
	if err != nil {
		return indexState{}, fmt.Errorf("read workspace index metadata: %w", err)
	}
	at, _ := time.Parse(time.RFC3339Nano, indexed)
	return indexState{IndexedAt: at}, nil
}

func (m *Manager) setState(ctx context.Context, status string, indexedAt time.Time, lastError string) error {
	db, err := openIndexDB(m.dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	indexed := ""
	if !indexedAt.IsZero() {
		indexed = indexedAt.Format(time.RFC3339Nano)
	} else if current, stateErr := m.state(ctx); stateErr == nil && !current.IndexedAt.IsZero() {
		indexed = current.IndexedAt.Format(time.RFC3339Nano)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO workspace_indexes (root, index_path, status, indexed_at, last_error, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(root) DO UPDATE SET index_path=excluded.index_path, status=excluded.status,
		indexed_at=excluded.indexed_at, last_error=excluded.last_error, updated_at=excluded.updated_at,
		generation=workspace_indexes.generation + CASE WHEN excluded.status = 'ready' THEN 1 ELSE 0 END`,
		m.root, m.indexPath, status, indexed, lastError, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("write workspace index metadata: %w", err)
	}
	return nil
}

func openIndexDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open workspace index metadata: %w", err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func workspaceKey(root string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	return hex.EncodeToString(sum[:12])
}

func (processRunner) Build(ctx context.Context, root, target string) error {
	_, err := runHelper(ctx, "build", "--root", root, "--index", target)
	return err
}

func (processRunner) Candidates(ctx context.Context, indexPath, pattern string, literal, caseSensitive bool) ([]string, error) {
	args := []string{"candidates", "--index", indexPath, "--pattern", pattern}
	if literal {
		args = append(args, "--literal")
	}
	if caseSensitive {
		args = append(args, "--case-sensitive")
	}
	out, err := runHelper(ctx, args...)
	if err != nil {
		return nil, err
	}
	var files []string
	if err := json.Unmarshal(out, &files); err != nil {
		return nil, fmt.Errorf("decode codesearch candidates: %w", err)
	}
	return files, nil
}

func runHelper(ctx context.Context, args ...string) ([]byte, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate tau executable: %w", err)
	}
	cmd := exec.CommandContext(ctx, executable, append([]string{"workspace-codesearch"}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("codesearch helper: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("run codesearch helper: %w", err)
	}
	return out, nil
}

// BuildIndex creates a codesearch sidecar for root. It is called only inside
// the isolated helper process.
func BuildIndex(ctx context.Context, root, target string) error {
	files, err := WorkspaceFiles(ctx, root)
	if err != nil {
		return err
	}
	w := index.Create(target)
	w.AddRoots([]index.Path{index.MakePath(root)})
	for _, path := range files {
		if err := w.AddFile(path); err != nil {
			continue
		}
	}
	w.Flush()
	return nil
}

// IndexCandidates returns the conservative trigram candidate set. It is
// called only inside the isolated helper process.
func IndexCandidates(indexPath, pattern string, literal, caseSensitive bool) (files []string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			files = nil
			err = fmt.Errorf("open codesearch index: %v", recovered)
		}
	}()
	if literal {
		pattern = regexp.QuoteMeta(pattern)
	}
	if !caseSensitive && !hasUppercase(pattern) {
		pattern = "(?i:" + pattern + ")"
	}
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return nil, fmt.Errorf("parse search pattern: %w", err)
	}
	ix := index.Open(indexPath)
	ids := ix.PostingQuery(index.RegexpQuery(re))
	files = make([]string, 0, len(ids))
	for _, id := range ids {
		files = append(files, ix.Name(id).String())
	}
	return files, nil
}

// WorkspaceFiles lists searchable workspace files, preferring Git's ignore
// semantics and falling back to a conservative filesystem walk.
func WorkspaceFiles(ctx context.Context, root string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "-co", "--exclude-standard", "-z")
	out, err := cmd.Output()
	if err == nil {
		parts := strings.Split(string(out), "\x00")
		files := make([]string, 0, len(parts))
		for _, part := range parts {
			if part != "" {
				path := filepath.Join(root, filepath.FromSlash(part))
				if safeWorkspaceFile(root, path) {
					files = append(files, path)
				}
			}
		}
		slices.Sort(files)
		return files, nil
	}

	var files []string
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path != root && strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.IsDir() && entry.Type().IsRegular() && safeWorkspaceFile(root, path) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk workspace files: %w", err)
	}
	slices.Sort(files)
	return files, nil
}

func safeWorkspaceFile(root, path string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	info, err := os.Lstat(absPath)
	return err == nil && info.Mode().IsRegular()
}

func hasUppercase(value string) bool {
	for _, r := range value {
		if r >= 'A' && r <= 'Z' {
			return true
		}
	}
	return false
}
