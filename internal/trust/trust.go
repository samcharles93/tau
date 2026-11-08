// Package trust implements the trust-on-first-use store for project-level
// root-spec overrides, per docs/specs/agents/01-agent-spec-format.md
// (Root-spec override trust). A project directory can ship a
// tau.agent.md that replaces the root agent's identity while it retains
// the full tool registry - a privilege-escalation vector when the project
// is untrusted (e.g. a freshly cloned repository). The store records which
// project + spec-hash combinations the user has explicitly approved so the
// approval doesn't need to be repeated every run, while any change to the
// spec file (a different hash) is treated as untrusted again.
package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// ModeHash trusts only the exact spec_hash recorded - any edit to the spec
// file requires re-approval.
const ModeHash = "hash"

// ModePath trusts any hash for the given project_path - future edits to
// the spec file are trusted without re-prompting.
const ModePath = "path"

// Entry is one trusted project-spec approval.
type Entry struct {
	ProjectPath string    `yaml:"project_path"`
	SpecHash    string    `yaml:"spec_hash"`
	TrustedAt   time.Time `yaml:"trusted_at"`
	TrustMode   string    `yaml:"trust_mode"`
}

// Store is the parsed contents of trust.yaml.
type Store struct {
	TrustedSpecs []Entry `yaml:"trusted_specs"`

	path string // where Save writes; set by Load
}

// storePath returns the trust store's canonical path - the tau config
// directory, never the project repository, so a project cannot self-approve
// by shipping a trust entry.
func storePath(configDir string) string {
	return filepath.Join(configDir, "trust.yaml")
}

// Load reads the trust store from configDir/trust.yaml. A missing file is
// not an error - it's treated as an empty store (nothing trusted yet).
func Load(configDir string) (*Store, error) {
	p := storePath(configDir)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &Store{path: p}, nil
		}
		return nil, fmt.Errorf("trust: read %s: %w", p, err)
	}
	var s Store
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("trust: parse %s: %w", p, err)
	}
	s.path = p
	return &s, nil
}

// Save writes the store back to its path atomically (temp file + fsync +
// rename - see writeFileAtomic), so a crash mid-write can never leave a
// partial trust.yaml behind. A new file is created 0600 and an existing
// file keeps its mode - approvals are security-relevant local state, not
// shareable.
func (s *Store) Save() error {
	if s.path == "" {
		return fmt.Errorf("trust: store has no path (not loaded via Load)")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("trust: create config dir: %w", err)
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("trust: marshal: %w", err)
	}
	if err := writeFileAtomic(s.path, data, 0o600); err != nil {
		return fmt.Errorf("trust: write %s: %w", s.path, err)
	}
	return nil
}

// writeFileAtomic replaces path with data atomically: data is written to a
// temp file in the SAME directory (so the rename never crosses a filesystem
// boundary), fsynced, closed, then renamed over path. A crash at any point
// therefore leaves either the old file or the new one - never a partial
// write. If path already exists its permission bits are preserved; otherwise
// the file is created with mode. Mirrors writeFileAtomic in
// internal/store/markdown_export.go, which cannot be shared here: app
// imports trust, so the dependency points the other way.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("trust: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once renamed
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("trust: chmod temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("trust: write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("trust: fsync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("trust: close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("trust: rename temp file: %w", err)
	}
	return nil
}

// IsTrusted reports whether projectPath+specHash is already approved: a
// "hash" entry must match both fields exactly; a "path" entry matches on
// projectPath alone (any hash in that project directory is trusted).
func (s *Store) IsTrusted(projectPath, specHash string) bool {
	for _, e := range s.TrustedSpecs {
		if e.ProjectPath != projectPath {
			continue
		}
		switch e.TrustMode {
		case ModePath:
			return true
		default: // ModeHash, or unrecognised - safest default is exact match
			if e.SpecHash == specHash {
				return true
			}
		}
	}
	return false
}

// Trust records approval of projectPath+specHash under the given mode,
// replacing any existing entry for the same projectPath (a project has at
// most one active trust entry - approving a new hash/mode supersedes the
// old one rather than accumulating stale entries).
func (s *Store) Trust(projectPath, specHash, mode string) {
	entry := Entry{
		ProjectPath: projectPath,
		SpecHash:    specHash,
		TrustedAt:   time.Now().UTC(),
		TrustMode:   mode,
	}
	for i, e := range s.TrustedSpecs {
		if e.ProjectPath == projectPath {
			s.TrustedSpecs[i] = entry
			return
		}
	}
	s.TrustedSpecs = append(s.TrustedSpecs, entry)
}

// HashFile returns the hex-encoded sha256 of a file's contents, as used for
// spec_hash - content-bound so any edit to the spec file invalidates a
// "hash"-mode trust entry.
func HashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("trust: read %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
