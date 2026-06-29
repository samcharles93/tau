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
	OldText    string `json:"old_text"`
	NewText    string `json:"new_text"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

var editSchema = Schema{
	Name:        "edit",
	Description: "Edit an existing file using exact text replacement. Each edit specifies old_text (must match exactly including whitespace) and new_text. By default only the first occurrence is replaced; set replace_all to true to replace every occurrence. Edits are applied sequentially — if an edit fails, previous edits in the batch are not rolled back.",
	Parameters: json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Absolute or relative file path to edit"
			},
			"edits": {
				"type": "array",
				"description": "List of search-and-replace operations. Applied sequentially — if one fails, the file is left with prior edits applied.",
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
						},
						"replace_all": {
							"type": "boolean",
							"description": "Replace all occurrences of old_text. Defaults to false (first occurrence only)."
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
func NewEditTool(cwd string, mq *MutationQueue, rt *ReadTracker) Tool {
	return Tool{
		Schema:  editSchema,
		Source:  "builtin",
		Execute: makeEditExecutor(cwd, mq, rt),
	}
}

func makeEditExecutor(cwd string, mq *MutationQueue, rt *ReadTracker) Executor {
	return func(ctx context.Context, params json.RawMessage, _ UIBridge) (Result, error) {
		var p EditParams
		if err := json.Unmarshal(params, &p); err != nil {
			return Result{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
		}

		if len(p.Edits) == 0 {
			return Result{Content: "at least one edit is required", IsError: true}, nil
		}

		ctx, cancel := context.WithTimeout(ctx, DefaultToolTimeout)
		defer cancel()

		path := resolvePath(cwd, p.Path)

		if !isConfined(cwd, path) {
			return Result{Content: "path escapes working directory", IsError: true}, nil
		}

		// Enforce read-before-write check.
		if rt != nil {
			if err := rt.CheckRead(cwd, p.Path); err != nil {
				return Result{Content: err.Error(), IsError: true}, nil
			}
		}

		// Check file size before reading to avoid OOM on large files.
		info, err := os.Stat(path)
		if err != nil {
			return Result{Content: fmt.Sprintf("error stating file: %v", err), IsError: true}, nil
		}
		if info.Size() > maxReadBytes {
			return Result{Content: fmt.Sprintf("file too large to edit (%s > %s)", FormatSize(int(info.Size())), FormatSize(maxReadBytes)), IsError: true}, nil
		}

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
			if edit.ReplaceAll {
				content = strings.ReplaceAll(content, edit.OldText, edit.NewText)
			} else {
				content = strings.Replace(content, edit.OldText, edit.NewText, 1)
			}
			applied++
		}

		if err := writeFileAtomic(path, []byte(content), 0o644); err != nil {
			return Result{Content: fmt.Sprintf("error writing file: %v", err), IsError: true}, nil
		}

		return Result{Content: fmt.Sprintf("applied %d edit(s) to %s", applied, path)}, nil
	}
}
