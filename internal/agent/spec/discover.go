package spec

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/samcharles93/tau/internal/skills"
)

// agentFileSuffix is the extension a filesystem-loaded agent definition must
// use, matching the embedded built-in template naming (e.g. "plan.agent.md").
const agentFileSuffix = ".agent.md"

// Source describes one root directory to scan for agent definitions.
type Source struct {
	Root     string
	Scope    skills.Scope
	Priority int
}

const (
	userSourcePriority    = 10
	projectSourcePriority = 20
)

// Diagnostic reports a parse or filesystem issue discovered while loading
// agent definitions from disk. Unlike a hard error, a Diagnostic never stops
// discovery of the remaining files.
type Diagnostic struct {
	Path    string
	Message string
}

// UserSources returns the user-level agent definition root tau scans,
// mirroring skills' provider-agnostic ~/.agents/ convention.
func UserSources() []Source {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []Source{
		{Root: filepath.Join(homeDir, ".agents", "agents"), Scope: skills.ScopeUser, Priority: userSourcePriority},
	}
}

// ProjectSources returns the project-level agent definition root for cwd.
func ProjectSources(workingDir string) []Source {
	trimmed := strings.TrimSpace(workingDir)
	if trimmed == "" {
		return nil
	}
	return []Source{
		{Root: filepath.Join(trimmed, ".agents", "agents"), Scope: skills.ScopeProject, Priority: projectSourcePriority},
	}
}

// DefaultSources returns the standard filesystem discovery roots: user-level
// then project-level, so project definitions win ties on name collision.
func DefaultSources(workingDir string) []Source {
	sources := append([]Source{}, UserSources()...)
	sources = append(sources, ProjectSources(workingDir)...)
	return sources
}

// DiscoverFromDisk scans every source for *.agent.md files, parses each with
// Parse, and returns the surviving definitions plus any diagnostics. A
// malformed file is skipped and reported as a diagnostic rather than
// aborting the whole scan. On a name collision between sources, the higher
// Priority source wins (project over user); ties break on file path so the
// result is deterministic.
func DiscoverFromDisk(sources []Source) ([]*Definition, []Diagnostic) {
	var diagnostics []Diagnostic
	selected := make(map[string]*Definition)
	selectedPriority := make(map[string]int)

	for _, source := range sources {
		root := strings.TrimSpace(source.Root)
		if root == "" {
			continue
		}

		info, err := os.Stat(root)
		if err != nil {
			continue // missing source directory is not an error
		}
		if !info.IsDir() {
			continue
		}

		walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				diagnostics = append(diagnostics, Diagnostic{Path: path, Message: walkErr.Error()})
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			if !strings.HasSuffix(strings.ToLower(entry.Name()), agentFileSuffix) {
				return nil
			}

			content, err := os.ReadFile(path)
			if err != nil {
				diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
				return nil
			}

			def, err := Parse(content)
			if err != nil {
				diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
				return nil
			}
			def.Scope = source.Scope
			def.SourcePath = path

			key := strings.ToLower(def.Name)
			if existingPriority, ok := selectedPriority[key]; ok {
				if source.Priority < existingPriority {
					diagnostics = append(diagnostics, Diagnostic{
						Path:    path,
						Message: "shadowed by higher-priority definition of the same name",
					})
					return nil
				}
				if source.Priority == existingPriority && path <= selected[key].SourcePath {
					diagnostics = append(diagnostics, Diagnostic{
						Path:    path,
						Message: "shadowed by another definition of the same name",
					})
					return nil
				}
			}
			selected[key] = def
			selectedPriority[key] = source.Priority
			return nil
		})
		if walkErr != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: root, Message: walkErr.Error()})
		}
	}

	defs := make([]*Definition, 0, len(selected))
	for _, def := range selected {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool {
		return strings.ToLower(defs[i].Name) < strings.ToLower(defs[j].Name)
	})

	return defs, diagnostics
}
