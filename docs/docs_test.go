package docs

import (
	"io/fs"
	"os"
	"strings"
	"testing"
)

func TestEmbeddedFS(t *testing.T) {
	var foundConfigExample bool
	var foundReadme bool
	var foundPlugins bool
	var foundMCPExample bool
	var foundWebUISpec bool

	err := fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if path == "." {
			return nil
		}

		// Validate: No .go files are embedded in FS
		if strings.HasSuffix(path, ".go") {
			t.Errorf("found embedded .go file: %s", path)
		}

		// Validate: No internal/ directory or its files are embedded in FS
		if path == "internal" || strings.HasPrefix(path, "internal/") {
			t.Errorf("found embedded internal file or directory: %s", path)
		}

		// Validate: the VitePress site's own directories (config, static
		// assets) never gets swept into the FS the docs tool serves.
		if path == ".vitepress" || strings.HasPrefix(path, ".vitepress/") ||
			path == "public" || strings.HasPrefix(path, "public/") {
			t.Errorf("found unwanted VitePress scaffolding embedded in FS: %s", path)
		}

		if path == "config-example.yaml" {
			foundConfigExample = true
		}
		if path == "README.md" {
			foundReadme = true
		}
		if path == "plugins.md" {
			foundPlugins = true
		}
		if path == "examples/mcp-plugin.md" {
			foundMCPExample = true
		}
		if path == "specs/web-ui.md" {
			foundWebUISpec = true
		}

		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk embedded FS: %v", err)
	}

	// Validate: config-example.yaml and README.md are embedded in FS
	if !foundConfigExample {
		t.Error("expected config-example.yaml to be embedded in FS, but it was not")
	}
	if !foundReadme {
		t.Error("expected README.md to be embedded in FS, but it was not")
	}
	if !foundPlugins {
		t.Error("expected plugins.md to be embedded in FS, but it was not")
	}
	// Validate: per-plugin walkthroughs under examples/ and specs/ are
	// embedded too, so the docs tool can actually answer questions about
	// them (previously only top-level *.md/*.yaml were embedded).
	if !foundMCPExample {
		t.Error("expected examples/mcp-plugin.md to be embedded in FS, but it was not")
	}
	if !foundWebUISpec {
		t.Error("expected specs/web-ui.md to be embedded in FS, but it was not")
	}
}

func TestDocsEmbedDirective(t *testing.T) {
	content, err := os.ReadFile("docs.go")
	if err != nil {
		t.Fatalf("failed to read docs.go: %v", err)
	}

	lines := strings.Split(string(content), "\n")
	foundDirective := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//go:embed") {
			foundDirective = true
			pattern := strings.TrimSpace(strings.TrimPrefix(line, "//go:embed"))
			// Verify that the embed directive uses a glob pattern.
			// Specifically, it should contain wildcards like '*'
			if !strings.Contains(pattern, "*") {
				t.Errorf("expected embed directive to use a glob pattern (containing '*'), got: %s", line)
			}
			// Verify that the pattern only targets top-level files (no slashes)
			if strings.Contains(pattern, "/") || strings.Contains(pattern, "\\") {
				t.Errorf("expected embed directive to not target subdirectories, got: %s", line)
			}
		}
	}
	if !foundDirective {
		t.Error("could not find //go:embed directive in docs.go")
	}
}
