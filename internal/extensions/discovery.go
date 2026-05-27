package extensions

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tauconfig "github.com/samcharles93/tau/internal/config"
	"gopkg.in/yaml.v3"
)

const (
	ManifestFileName     = "tau.yaml"
	DefaultLuaEntryFile  = "main.lua"
	ExtensionPathsEnvVar = "TAU_EXTENSION_PATHS"
)

type manifest struct {
	Name    string `yaml:"name"`
	Runtime string `yaml:"runtime"`
	Entry   string `yaml:"entry"`
	Enabled *bool  `yaml:"enabled"`
}

func DefaultSources(workingDir string) []Source {
	sources := []Source{{Root: filepath.Join(userConfigDir(), "tau", "extensions"), Scope: ScopeUser}}
	if trimmed := strings.TrimSpace(workingDir); trimmed != "" {
		sources = append(sources, Source{
			Root:  filepath.Join(trimmed, ".tau", "extensions"),
			Scope: ScopeProject,
		})
	}
	sources = append(sources, AdditionalSourcesFromEnv()...)
	return sources
}

func SourcesFromConfig(workingDir string, cfg tauconfig.ExtensionConfig) []Source {
	cfg = tauconfig.NormalizeExtensions(cfg)
	if !cfg.Enabled {
		return nil
	}
	sources := []Source{{Root: filepath.Join(userConfigDir(), "tau", "extensions"), Scope: ScopeUser}}
	if cfg.AllowProject {
		if trimmed := strings.TrimSpace(workingDir); trimmed != "" {
			sources = append(sources, Source{
				Root:  filepath.Join(trimmed, ".tau", "extensions"),
				Scope: ScopeProject,
			})
		}
	}
	sources = append(sources, AdditionalSources(cfg.Paths, ScopeExtra)...)
	sources = append(sources, AdditionalSourcesFromEnv()...)
	return sources
}

func AdditionalSourcesFromEnv() []Source {
	return AdditionalSources(filepath.SplitList(os.Getenv(ExtensionPathsEnvVar)), ScopeExtra)
}

func AdditionalSources(paths []string, scope Scope) []Source {
	sources := make([]Source, 0, len(paths))
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		sources = append(sources, Source{Root: trimmed, Scope: scope})
	}
	return sources
}

func Discover(sources []Source) ([]Extension, []Diagnostic) {
	var diagnostics []Diagnostic
	seen := make(map[string]Extension)

	for _, source := range sources {
		root := strings.TrimSpace(source.Root)
		if root == "" {
			continue
		}
		info, err := os.Stat(root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: root, Severity: SeverityError, Message: err.Error()})
			continue
		}
		if !info.IsDir() {
			diagnostics = append(diagnostics, Diagnostic{Path: root, Severity: SeverityWarning, Message: "extension root is not a directory"})
			continue
		}

		entries, err := os.ReadDir(root)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: root, Severity: SeverityError, Message: err.Error()})
			continue
		}
		for _, entry := range entries {
			ext, diag, ok := discoverEntry(source, root, entry)
			diagnostics = append(diagnostics, diag...)
			if !ok {
				continue
			}

			key := normalizeName(ext.Name)
			if existing, exists := seen[key]; exists {
				diagnostics = append(diagnostics, Diagnostic{
					Path:          ext.Path,
					ExtensionName: ext.Name,
					Severity:      SeverityError,
					Message:       fmt.Sprintf("duplicate extension name already loaded from %s", existing.Path),
				})
				continue
			}
			seen[key] = ext
		}
	}

	extensions := make([]Extension, 0, len(seen))
	for _, ext := range seen {
		extensions = append(extensions, ext)
	}
	slices.SortFunc(extensions, func(left, right Extension) int {
		if compare := strings.Compare(normalizeName(left.Name), normalizeName(right.Name)); compare != 0 {
			return compare
		}
		return strings.Compare(left.Path, right.Path)
	})
	slices.SortFunc(diagnostics, func(left, right Diagnostic) int {
		if compare := strings.Compare(left.Path, right.Path); compare != 0 {
			return compare
		}
		return strings.Compare(left.Message, right.Message)
	})
	return extensions, diagnostics
}

func discoverEntry(source Source, root string, entry os.DirEntry) (Extension, []Diagnostic, bool) {
	path := filepath.Join(root, entry.Name())
	if entry.IsDir() {
		return discoverManifest(source, root, path)
	}
	if filepath.Ext(entry.Name()) != ".lua" {
		return Extension{}, nil, false
	}
	name := extensionNameFromLuaPath(path)
	if strings.TrimSpace(name) == "" {
		return Extension{}, []Diagnostic{{Path: path, Severity: SeverityError, Message: "extension name is required"}}, false
	}
	return Extension{
		Name:       name,
		Runtime:    "lua",
		Entry:      path,
		Path:       path,
		SourceRoot: root,
		Scope:      source.Scope,
	}, nil, true
}

func discoverManifest(source Source, root, dir string) (Extension, []Diagnostic, bool) {
	manifestPath := filepath.Join(dir, ManifestFileName)
	data, err := os.ReadFile(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return Extension{}, nil, false
	}
	if err != nil {
		return Extension{}, []Diagnostic{{Path: manifestPath, Severity: SeverityError, Message: err.Error()}}, false
	}

	var mf manifest
	if err := yaml.Unmarshal(data, &mf); err != nil {
		return Extension{}, []Diagnostic{{Path: manifestPath, Severity: SeverityError, Message: err.Error()}}, false
	}
	if mf.Enabled != nil && !*mf.Enabled {
		return Extension{}, []Diagnostic{{Path: manifestPath, ExtensionName: mf.Name, Severity: SeverityWarning, Message: "extension is disabled"}}, false
	}
	name := strings.TrimSpace(mf.Name)
	if name == "" {
		return Extension{}, []Diagnostic{{Path: manifestPath, Severity: SeverityError, Message: "extension name is required"}}, false
	}
	if strings.TrimSpace(mf.Runtime) != "lua" {
		return Extension{}, []Diagnostic{{Path: manifestPath, ExtensionName: name, Severity: SeverityError, Message: fmt.Sprintf("unsupported runtime %q", mf.Runtime)}}, false
	}
	entry := strings.TrimSpace(mf.Entry)
	if entry == "" {
		entry = DefaultLuaEntryFile
	}
	entryPath := filepath.Join(dir, entry)
	info, err := os.Stat(entryPath)
	if err != nil {
		return Extension{}, []Diagnostic{{Path: entryPath, ExtensionName: name, Severity: SeverityError, Message: "entry file is missing"}}, false
	}
	if info.IsDir() {
		return Extension{}, []Diagnostic{{Path: entryPath, ExtensionName: name, Severity: SeverityError, Message: "entry path is a directory"}}, false
	}
	return Extension{
		Name:       name,
		Runtime:    "lua",
		Entry:      entryPath,
		Path:       dir,
		SourceRoot: root,
		Scope:      source.Scope,
	}, nil, true
}

func userConfigDir() string {
	if dir, err := os.UserConfigDir(); err == nil && strings.TrimSpace(dir) != "" {
		return dir
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".config")
	}
	return ".config"
}
