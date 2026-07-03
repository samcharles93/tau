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

// NewDocsTool creates the built-in docs tool for Tau's embedded documentation.
// It lists, searches, or reads documentation files depending on the parameters.
func NewDocsTool() Tool {
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
				return readDoc(p.Path), nil
			case strings.TrimSpace(p.Query) != "":
				return searchDocs(p.Query), nil
			default:
				return listDocs(), nil
			}
		},
	}
}

func readDoc(path string) Result {
	cleanPath := filepath.Clean(path)
	// Prevent escaping the documentation filesystem.
	if strings.HasPrefix(cleanPath, "..") || strings.HasPrefix(cleanPath, "/") {
		return Result{Content: "invalid path: escaping docs sandbox", IsError: true}
	}

	content, err := docs.FS.ReadFile(cleanPath)
	if err != nil {
		return Result{Content: fmt.Sprintf("documentation file not found: %s", path), IsError: true}
	}

	return Result{Content: string(content)}
}

func searchDocs(query string) Result {
	query = strings.ToLower(strings.TrimSpace(query))

	var matches []string
	err := walkDocs(func(path string, content []byte) {
		lines := strings.Split(string(content), "\n")
		for idx, line := range lines {
			if strings.Contains(strings.ToLower(line), query) {
				matches = append(matches, fmt.Sprintf("%s:%d: %s", path, idx+1, strings.TrimSpace(line)))
			}
		}
	})
	if err != nil {
		return Result{Content: fmt.Sprintf("error walking documentation: %v", err), IsError: true}
	}

	if len(matches) == 0 {
		return Result{Content: fmt.Sprintf("No matches found for query: %q", query)}
	}

	tr := TruncateHead(strings.Join(matches, "\n"), DefaultMaxLines, DefaultMaxBytes)
	return Result{Content: tr.Content}
}

func listDocs() Result {
	var paths []string
	err := walkDocs(func(path string, _ []byte) {
		paths = append(paths, path)
	})
	if err != nil {
		return Result{Content: fmt.Sprintf("error walking documentation: %v", err), IsError: true}
	}

	tr := TruncateHead(strings.Join(paths, "\n"), DefaultMaxLines, DefaultMaxBytes)
	return Result{Content: tr.Content}
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
