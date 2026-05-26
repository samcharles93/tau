package extensions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestDefaultSourcesHonorsEnvironment(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	t.Setenv(ExtensionPathsEnvVar, filepath.Join(t.TempDir(), "extra-a")+string(os.PathListSeparator)+filepath.Join(t.TempDir(), "extra-b"))

	cwd := t.TempDir()
	sources := DefaultSources(cwd)

	require.Len(t, sources, 4)
	require.Equal(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "tau", "extensions"), sources[0].Root)
	require.Contains(t, sourceRoots(sources), filepath.Join(cwd, ".tau", "extensions"))
	require.Equal(t, ScopeExtra, sources[2].Scope)
}

func TestDiscoverLoadsSingleFileAndManifestExtensions(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "single.lua"), `tau.log("info", "single")`)
	writeFile(t, filepath.Join(root, "manifested", ManifestFileName), `
name: manifest-ext
runtime: lua
entry: main.lua
`)
	writeFile(t, filepath.Join(root, "manifested", "main.lua"), `tau.log("info", "manifest")`)

	extensions, diagnostics := Discover([]Source{{Root: root, Scope: ScopeUser}})

	require.Empty(t, diagnostics)
	require.Len(t, extensions, 2)
	require.Equal(t, "manifest-ext", extensions[0].Name)
	require.Equal(t, "single", extensions[1].Name)
}

func TestSourcesFromConfigControlsProjectAndExtraRoots(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	t.Setenv(ExtensionPathsEnvVar, "")

	cwd := t.TempDir()
	extra := filepath.Join(t.TempDir(), "extra")

	sources := SourcesFromConfig(cwd, tauconfig.ExtensionConfig{Paths: []string{extra}})

	require.Contains(t, sourceRoots(sources), filepath.Join(cwd, ".tau", "extensions"))
	require.Contains(t, sourceRoots(sources), extra)

	sources = SourcesFromConfig(cwd, extensionConfigWithAllowProject(false))
	require.NotContains(t, sourceRoots(sources), filepath.Join(cwd, ".tau", "extensions"))

	sources = SourcesFromConfig(cwd, extensionConfigWithEnabled(false))
	require.Empty(t, sources)
}

func TestDiscoverMissingRootsAndInvalidEntries(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "disabled", ManifestFileName), `
name: disabled
runtime: lua
enabled: false
`)
	writeFile(t, filepath.Join(root, "missing-entry", ManifestFileName), `
name: missing-entry
runtime: lua
`)
	writeFile(t, filepath.Join(root, "unsupported", ManifestFileName), `
name: unsupported
runtime: wasm
`)
	writeFile(t, filepath.Join(root, "empty-name", ManifestFileName), `
runtime: lua
`)

	extensions, diagnostics := Discover([]Source{
		{Root: filepath.Join(root, "does-not-exist"), Scope: ScopeUser},
		{Root: root, Scope: ScopeUser},
	})

	require.Empty(t, extensions)
	require.True(t, containsDiagnostic(diagnostics, "extension is disabled"))
	require.True(t, containsDiagnostic(diagnostics, "entry file is missing"))
	require.True(t, containsDiagnostic(diagnostics, "unsupported runtime"))
	require.True(t, containsDiagnostic(diagnostics, "extension name is required"))
}

func TestDiscoverDiagnosesDuplicateNames(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "one.lua"), ``)
	writeFile(t, filepath.Join(root, "dir", ManifestFileName), `
name: one
runtime: lua
`)
	writeFile(t, filepath.Join(root, "dir", "main.lua"), ``)

	extensions, diagnostics := Discover([]Source{{Root: root, Scope: ScopeUser}})

	require.Len(t, extensions, 1)
	require.True(t, containsDiagnostic(diagnostics, "duplicate extension name"))
}

func sourceRoots(sources []Source) []string {
	roots := make([]string, 0, len(sources))
	for _, source := range sources {
		roots = append(roots, source.Root)
	}
	return roots
}

func containsDiagnostic(diagnostics []Diagnostic, fragment string) bool {
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, fragment) {
			return true
		}
	}
	return false
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func extensionConfigWithEnabled(enabled bool) tauconfig.ExtensionConfig {
	var cfg tauconfig.Config
	data := "extensions:\n  enabled: false\n"
	if enabled {
		data = "extensions:\n  enabled: true\n"
	}
	if err := yaml.Unmarshal([]byte(data), &cfg); err != nil {
		panic(err)
	}
	return cfg.Extensions
}

func extensionConfigWithAllowProject(allow bool) tauconfig.ExtensionConfig {
	var cfg tauconfig.Config
	data := "extensions:\n  allow_project: false\n"
	if allow {
		data = "extensions:\n  allow_project: true\n"
	}
	if err := yaml.Unmarshal([]byte(data), &cfg); err != nil {
		panic(err)
	}
	return cfg.Extensions
}
