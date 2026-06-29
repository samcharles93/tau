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

// SearchDocsParams are parameters for search_docs.
type SearchDocsParams struct {
	Query string `json:"query"`
}

// ReadDocParams are parameters for read_doc.
type ReadDocParams struct {
	Path string `json:"path"`
}

var searchDocsSchema = Schema{
	Name:        "search_docs",
	Description: "Search Tau's internal documentation (user manuals, developer guides, architectural decisions) for a keyword or phrase.",
	Parameters: json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "The search term or phrase to locate in the documentation"
			}
		},
		"required": ["query"]
	}`),
}

var readDocSchema = Schema{
	Name:        "read_doc",
	Description: "Read the full contents of a specific Tau documentation file.",
	Parameters: json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "The path of the documentation file to read (e.g. 'config-example.yaml', 'README.md')"
			}
		},
		"required": ["path"]
	}`),
}

// NewSearchDocsTool creates a tool to search the embedded documentation.
func NewSearchDocsTool() Tool {
	return Tool{
		Schema: searchDocsSchema,
		Source: "builtin",
		Execute: func(ctx context.Context, params json.RawMessage, _ UIBridge) (Result, error) {
			var p SearchDocsParams
			if err := json.Unmarshal(params, &p); err != nil {
				return Result{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
			}
			ctx, cancel := context.WithTimeout(ctx, DefaultToolTimeout)
			defer cancel()

			query := strings.ToLower(strings.TrimSpace(p.Query))
			if query == "" {
				return Result{Content: "query cannot be empty", IsError: true}, nil
			}

			var matches []string
			err := fs.WalkDir(docs.FS, ".", func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() || path == "docs.go" || filepath.Ext(path) == ".go" {
					return nil
				}
				content, err := docs.FS.ReadFile(path)
				if err != nil {
					return nil
				}
				lines := strings.Split(string(content), "\n")
				for idx, line := range lines {
					if strings.Contains(strings.ToLower(line), query) {
						matches = append(matches, fmt.Sprintf("%s:%d: %s", path, idx+1, strings.TrimSpace(line)))
					}
				}
				return nil
			})
			if err != nil {
				return Result{Content: fmt.Sprintf("error walking documentation: %v", err), IsError: true}, nil
			}

			if len(matches) == 0 {
				return Result{Content: fmt.Sprintf("No matches found for query: %q", p.Query)}, nil
			}

			tr := TruncateHead(strings.Join(matches, "\n"), DefaultMaxLines, DefaultMaxBytes)
			return Result{Content: tr.Content}, nil
		},
	}
}

// NewReadDocTool creates a tool to read an embedded documentation file.
func NewReadDocTool() Tool {
	return Tool{
		Schema: readDocSchema,
		Source: "builtin",
		Execute: func(ctx context.Context, params json.RawMessage, _ UIBridge) (Result, error) {
			var p ReadDocParams
			if err := json.Unmarshal(params, &p); err != nil {
				return Result{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
			}

			ctx, cancel := context.WithTimeout(ctx, DefaultToolTimeout)
			defer cancel()

			cleanPath := filepath.Clean(p.Path)
			// Prevent escaping the documentation filesystem
			if strings.HasPrefix(cleanPath, "..") || strings.HasPrefix(cleanPath, "/") {
				return Result{Content: "invalid path: escaping docs sandbox", IsError: true}, nil
			}

			content, err := docs.FS.ReadFile(cleanPath)
			if err != nil {
				return Result{Content: fmt.Sprintf("documentation file not found: %s", p.Path), IsError: true}, nil
			}

			return Result{Content: string(content)}, nil
		},
	}
}
