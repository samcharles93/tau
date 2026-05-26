package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// EditParams are the parameters for the edit tool.
type EditParams struct {
	Path  string       `json:"path"`
	Edits []EditAction `json:"edits"`
}

// EditAction is a single search-and-replace operation.
type EditAction struct {
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

var editSchema = Schema{
	Name:        "edit",
	Description: "Edit an existing file using exact text replacement. Each edit specifies old_text (must match exactly) and new_text. Edits are applied sequentially.",
	Parameters: json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Absolute or relative file path to edit"
			},
			"edits": {
				"type": "array",
				"description": "List of search-and-replace operations",
				"items": {
					"type": "object",
					"properties": {
						"old_text": {
							"type": "string",
							"description": "Exact text to find (must match exactly including whitespace)"
						},
						"new_text": {
							"type": "string",
							"description": "Replacement text"
						}
					},
					"required": ["old_text", "new_text"]
				}
			}
		},
		"required": ["path", "edits"]
	}`),
}

// NewEditTool creates the built-in edit tool.
func NewEditTool(cwd string, mq *MutationQueue) Tool {
	return Tool{
		Schema:  editSchema,
		Source:  "builtin",
		Execute: makeEditExecutor(cwd, mq),
	}
}

func makeEditExecutor(cwd string, mq *MutationQueue) Executor {
	return func(ctx context.Context, params json.RawMessage, _ UIBridge) (Result, error) {
		var p EditParams
		if err := json.Unmarshal(params, &p); err != nil {
			return Result{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
		}

		if len(p.Edits) == 0 {
			return Result{Content: "at least one edit is required", IsError: true}, nil
		}

		path := resolvePath(cwd, p.Path)

		release := mq.Acquire(path)
		defer release()

		data, err := os.ReadFile(path)
		if err != nil {
			return Result{Content: fmt.Sprintf("error reading file: %v", err), IsError: true}, nil
		}

		content := string(data)
		applied := 0

		for i, edit := range p.Edits {
			if !strings.Contains(content, edit.OldText) {
				return Result{
					Content: fmt.Sprintf("edit %d: old_text not found in file (applied %d/%d edits before failure)", i+1, applied, len(p.Edits)),
					IsError: true,
				}, nil
			}
			// Replace first occurrence only.
			content = strings.Replace(content, edit.OldText, edit.NewText, 1)
			applied++
		}

		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return Result{Content: fmt.Sprintf("error writing file: %v", err), IsError: true}, nil
		}

		return Result{Content: fmt.Sprintf("applied %d edit(s) to %s", applied, path)}, nil
	}
}
