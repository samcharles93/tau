package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/samcharles93/tau/docs"
)

// DocsParams are the parameters for the docs tool.
type DocsParams struct {
	Query string `json:"query,omitempty"`
	Path  string `json:"path,omitempty"`
}

var docsSchema = Schema{
	Name:        "docs",
	Description: "Access Tau's own documentation (user manual, configuration reference, developer guides). Provide 'query' to search all docs for a keyword or phrase, 'path' to read a full documentation file, or neither to list available files. Use when the user asks about Tau itself — usage, configuration, errors, skills, or capabilities.",
	Parameters: json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "Search term or phrase to locate across the documentation. Returns matching lines with file paths."
			},
			"path": {
				"type": "string",
				"description": "Documentation file to read in full (e.g. 'config-example.yaml', 'README.md'). Takes precedence over query."
			}
		}
	}`),
}

// PluginDocsProvider returns the self-declared documentation of every
// currently loaded plugin, keyed by plugin name (see api.Documented). It is
// called fresh on every docs tool invocation so results reflect the live
// plugin set, including after a hot reload. A nil provider (or one that
// returns nil) is valid and means "no plugin docs available".
type PluginDocsProvider func() map[string]string

// pluginDocPath returns the virtual docs-tool path a plugin's documentation
// is exposed under, namespaced so it can't collide with tau's own docs.
func pluginDocPath(pluginName string) string {
	return "plugins/" + pluginName + ".md"
}

// NewDocsTool creates the built-in docs tool for Tau's embedded documentation.
// It lists, searches, or reads documentation files depending on the
// parameters. pluginDocs, if non-nil, is merged in alongside tau's own
// embedded docs so plugins can surface their own documentation (e.g. an MCP
// server's config format) without tau needing to know about them in advance.
func NewDocsTool(pluginDocs PluginDocsProvider) Tool {
	return Tool{
		Schema: docsSchema,
		Source: "builtin",
		Execute: func(ctx context.Context, params json.RawMessage, _ UIBridge) (Result, error) {
			var p DocsParams
			if err := json.Unmarshal(params, &p); err != nil {
				return Result{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
			}

			_, cancel := context.WithTimeout(ctx, DefaultToolTimeout)
			defer cancel()

			switch {
			case strings.TrimSpace(p.Path) != "":
				return readDoc(p.Path, pluginDocs), nil
			case strings.TrimSpace(p.Query) != "":
				return searchDocs(p.Query, pluginDocs), nil
			default:
				return listDocs(pluginDocs), nil
			}
		},
	}
}

func readDoc(path string, pluginDocs PluginDocsProvider) Result {
	cleanPath := filepath.Clean(path)
	// Prevent escaping the documentation filesystem.
	if strings.HasPrefix(cleanPath, "..") || strings.HasPrefix(cleanPath, "/") {
		return Result{Content: "invalid path: escaping docs sandbox", IsError: true}
	}

	if pluginDocs != nil {
		for name, content := range pluginDocs() {
			if pluginDocPath(name) == cleanPath {
				return Result{Content: content}
			}
		}
	}

	content, err := docs.FS.ReadFile(cleanPath)
	if err != nil {
		return Result{Content: fmt.Sprintf("documentation file not found: %s", path), IsError: true}
	}

	return Result{Content: string(content)}
}

func searchDocs(query string, pluginDocs PluginDocsProvider) Result {
	query = strings.ToLower(strings.TrimSpace(query))

	var matches []string
	search := func(path string, content []byte) {
		lines := strings.Split(string(content), "\n")
		for idx, line := range lines {
			if strings.Contains(strings.ToLower(line), query) {
				matches = append(matches, fmt.Sprintf("%s:%d: %s", path, idx+1, strings.TrimSpace(line)))
			}
		}
	}

	err := walkDocs(search)
	if err != nil {
		return Result{Content: fmt.Sprintf("error walking documentation: %v", err), IsError: true}
	}
	if pluginDocs != nil {
		for name, content := range pluginDocs() {
			search(pluginDocPath(name), []byte(content))
		}
	}

	if len(matches) == 0 {
		return Result{Content: fmt.Sprintf("No matches found for query: %q", query)}
	}

	tr := TruncateHead(strings.Join(matches, "\n"), DefaultMaxLines, DefaultMaxBytes)
	return Result{Content: tr.Content, Truncated: tr.Truncated, ResultBytes: tr.OriginalSize}
}

func listDocs(pluginDocs PluginDocsProvider) Result {
	var paths []string
	err := walkDocs(func(path string, _ []byte) {
		paths = append(paths, path)
	})
	if err != nil {
		return Result{Content: fmt.Sprintf("error walking documentation: %v", err), IsError: true}
	}
	if pluginDocs != nil {
		for name := range pluginDocs() {
			paths = append(paths, pluginDocPath(name))
		}
	}

	tr := TruncateHead(strings.Join(paths, "\n"), DefaultMaxLines, DefaultMaxBytes)
	return Result{Content: tr.Content, Truncated: tr.Truncated, ResultBytes: tr.OriginalSize}
}

// walkDocs visits every documentation file in the embedded FS, skipping
// directories and Go source files.
func walkDocs(visit func(path string, content []byte)) error {
	return fs.WalkDir(docs.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) == ".go" {
			return nil
		}
		content, err := docs.FS.ReadFile(path)
		if err != nil {
			return nil
		}
		visit(path, content)
		return nil
	})
}
