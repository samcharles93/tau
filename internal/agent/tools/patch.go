package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// PatchParams are the parameters for the patch tool.
//
// The patch must be a unified diff for a single file. File header lines
// (---/+++) are optional. Hunks are applied strictly and atomically.
//
// Example:
// @@ -1,3 +1,3 @@
//
//	package main
//
// -fmt.Println("old")
// +fmt.Println("new")
//
// Context lines must match exactly.
type PatchParams struct {
	Path        string `json:"path"`
	UnifiedDiff string `json:"unified_diff"`
}

var patchSchema = Schema{
	Name:        "patch",
	Description: "Apply a unified diff patch to an existing file. Supports a single-file patch with one or more hunks. Applies atomically and fails if context does not match.",
	Parameters: json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Absolute or relative file path to patch"
			},
			"unified_diff": {
				"type": "string",
				"description": "Unified diff to apply. Must contain one or more @@ hunk headers. File header lines (---/+++) are optional. Single-file patches only."
			}
		},
		"required": ["path", "unified_diff"]
	}`),
}

// NewPatchTool creates the built-in patch tool.
func NewPatchTool(cwd string, mq *MutationQueue, rt *ReadTracker) Tool {
	return Tool{
		Schema:  patchSchema,
		Source:  "builtin",
		Execute: makePatchExecutor(cwd, mq, rt),
	}
}

func makePatchExecutor(cwd string, mq *MutationQueue, rt *ReadTracker) Executor {
	return func(ctx context.Context, params json.RawMessage, _ UIBridge) (Result, error) {
		var p PatchParams
		if err := json.Unmarshal(params, &p); err != nil {
			return Result{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
		}

		p.Path = strings.TrimSpace(p.Path)
		ctx, cancel := context.WithTimeout(ctx, DefaultToolTimeout)
		defer cancel()

		p.UnifiedDiff = normaliseDiff(strings.TrimSpace(p.UnifiedDiff))

		if p.Path == "" {
			return Result{Content: "path is required", IsError: true}, nil
		}
		if p.UnifiedDiff == "" {
			return Result{Content: "unified_diff is required", IsError: true}, nil
		}
		if err := validateWorkingDir(cwd); err != nil {
			return Result{Content: err.Error(), IsError: true}, nil
		}

		path, err := resolvePathWithinRoot(cwd, p.Path)
		if err != nil {
			return Result{Content: err.Error(), IsError: true}, nil
		}

		// Enforce read-before-write check (after confinement, before mutation).
		if rt != nil {
			if err := rt.CheckRead(cwd, p.Path); err != nil {
				return Result{Content: err.Error(), IsError: true}, nil
			}
		}
		release := mq.Acquire(path)
		defer release()

		data, err := os.ReadFile(path)
		if err != nil {
			return Result{Content: fmt.Sprintf("error reading file: %v", err), IsError: true}, nil
		}

		// Detect CRLF line endings before normalisation so we can
		// restore them after patching. The incoming diff is already
		// LF-normalised; we normalise the original for matching but
		// preserve the original line-ending style on write.
		hadCRLF := strings.Contains(string(data), "\r\n")
		original := normaliseDiff(string(data))
		patched, stats, err := applyUnifiedDiff(original, p.UnifiedDiff)
		if err != nil {
			return Result{Content: fmt.Sprintf("patch failed: %v", err), IsError: true}, nil
		}

		if patched == original {
			return Result{Content: fmt.Sprintf("patch made no changes to %s", path)}, nil
		}

		if hadCRLF {
			patched = strings.ReplaceAll(patched, "\n", "\r\n")
		}

		if err := writeFileAtomic(path, []byte(patched), 0o644); err != nil {
			return Result{Content: fmt.Sprintf("error writing file: %v", err), IsError: true}, nil
		}

		return Result{Content: fmt.Sprintf("applied %d hunk(s) to %s (+%d/-%d lines)", stats.Hunks, path, stats.Added, stats.Removed)}, nil
	}
}

type patchStats struct {
	Hunks   int
	Added   int
	Removed int
}

type unifiedHunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Lines    []string
}

func applyUnifiedDiff(original, diff string) (string, patchStats, error) {
	hunks, err := parseUnifiedDiff(diff)
	if err != nil {
		return "", patchStats{}, err
	}
	if len(hunks) == 0 {
		return "", patchStats{}, fmt.Errorf("no hunks found")
	}

	origHasTrailingNewline := strings.HasSuffix(original, "\n")
	origLines := splitLinesPreserveLogical(original)

	var out []string
	cursor := 0
	stats := patchStats{Hunks: len(hunks)}

	for idx, h := range hunks {
		if h.OldStart < 1 {
			return "", patchStats{}, fmt.Errorf("hunk %d has invalid old start line %d", idx+1, h.OldStart)
		}

		startIdx := h.OldStart - 1
		if startIdx < cursor {
			return "", patchStats{}, fmt.Errorf("hunk %d overlaps or is out of order", idx+1)
		}
		if startIdx > len(origLines) {
			return "", patchStats{}, fmt.Errorf("hunk %d starts beyond end of file", idx+1)
		}

		out = append(out, origLines[cursor:startIdx]...)
		matchAt := startIdx
		oldSeen := 0
		newSeen := 0

		for _, line := range h.Lines {
			if line == `\ No newline at end of file` {
				continue
			}
			if len(line) == 0 {
				return "", patchStats{}, fmt.Errorf("hunk %d contains an invalid diff line", idx+1)
			}

			prefix := line[0]
			text := line[1:]

			switch prefix {
			case ' ':
				if matchAt >= len(origLines) || origLines[matchAt] != text {
					return "", patchStats{}, fmt.Errorf("hunk %d context mismatch at source line %d", idx+1, matchAt+1)
				}
				out = append(out, text)
				matchAt++
				oldSeen++
				newSeen++
			case '-':
				if matchAt >= len(origLines) || origLines[matchAt] != text {
					return "", patchStats{}, fmt.Errorf("hunk %d removal mismatch at source line %d", idx+1, matchAt+1)
				}
				matchAt++
				oldSeen++
				stats.Removed++
			case '+':
				out = append(out, text)
				newSeen++
				stats.Added++
			default:
				return "", patchStats{}, fmt.Errorf("hunk %d contains an unsupported diff line prefix %q", idx+1, string(prefix))
			}
		}

		if oldSeen != h.OldCount {
			return "", patchStats{}, fmt.Errorf("hunk %d consumed %d source lines, expected %d", idx+1, oldSeen, h.OldCount)
		}
		if newSeen != h.NewCount {
			return "", patchStats{}, fmt.Errorf("hunk %d produced %d destination lines, expected %d", idx+1, newSeen, h.NewCount)
		}

		cursor = matchAt
	}

	out = append(out, origLines[cursor:]...)

	patched := strings.Join(out, "\n")
	if origHasTrailingNewline || diffRequestsTrailingNewline(diff) {
		patched += "\n"
	}

	return patched, stats, nil
}

func parseUnifiedDiff(diff string) ([]unifiedHunk, error) {
	diff = normaliseDiff(diff)
	lines := strings.Split(diff, "\n")
	var hunks []unifiedHunk

	for i := 0; i < len(lines); {
		line := lines[i]
		if line == "" {
			i++
			continue
		}
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			i++
			continue
		}
		if !strings.HasPrefix(line, "@@ ") && !strings.HasPrefix(line, "@@-") && !strings.HasPrefix(line, "@@ -") {
			return nil, fmt.Errorf("unexpected diff line before first hunk: %q", line)
		}

		h, err := parseHunkHeader(line)
		if err != nil {
			return nil, err
		}
		i++

		for i < len(lines) {
			curr := lines[i]
			if strings.HasPrefix(curr, "@@ ") || strings.HasPrefix(curr, "@@-") || strings.HasPrefix(curr, "@@ -") {
				break
			}
			if strings.HasPrefix(curr, "--- ") || strings.HasPrefix(curr, "+++ ") {
				return nil, fmt.Errorf("multi-file patches are not supported")
			}
			if curr == "" {
				h.Lines = append(h.Lines, " ")
				i++
				continue
			}
			prefix := curr[0]
			if prefix != ' ' && prefix != '+' && prefix != '-' && curr != `\ No newline at end of file` {
				return nil, fmt.Errorf("invalid diff line %q", curr)
			}
			h.Lines = append(h.Lines, curr)
			i++
		}

		hunks = append(hunks, h)
	}

	return hunks, nil
}

func parseHunkHeader(line string) (unifiedHunk, error) {
	// Format: @@ -oldStart[,oldCount] +newStart[,newCount] @@ optional section heading
	if !strings.HasPrefix(line, "@@") {
		return unifiedHunk{}, fmt.Errorf("invalid hunk header %q", line)
	}
	end := strings.Index(line[2:], "@@")
	if end < 0 {
		return unifiedHunk{}, fmt.Errorf("invalid hunk header %q", line)
	}
	body := strings.TrimSpace(line[2 : 2+end])
	parts := strings.Fields(body)
	if len(parts) < 2 {
		return unifiedHunk{}, fmt.Errorf("invalid hunk header %q", line)
	}

	oldStart, oldCount, err := parseRange(parts[0], '-')
	if err != nil {
		return unifiedHunk{}, fmt.Errorf("invalid old range in hunk header %q: %w", line, err)
	}
	newStart, newCount, err := parseRange(parts[1], '+')
	if err != nil {
		return unifiedHunk{}, fmt.Errorf("invalid new range in hunk header %q: %w", line, err)
	}

	return unifiedHunk{
		OldStart: oldStart,
		OldCount: oldCount,
		NewStart: newStart,
		NewCount: newCount,
	}, nil
}

func parseRange(s string, prefix byte) (start, count int, err error) {
	if len(s) < 2 || s[0] != prefix {
		return 0, 0, fmt.Errorf("range %q missing %c prefix", s, prefix)
	}
	s = s[1:]
	parts := strings.SplitN(s, ",", 2)
	start, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid start line %q", parts[0])
	}
	if start < 0 {
		return 0, 0, fmt.Errorf("start line must be >= 0")
	}
	if len(parts) == 1 {
		return start, 1, nil
	}
	count, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid line count %q", parts[1])
	}
	if count < 0 {
		return 0, 0, fmt.Errorf("line count must be >= 0")
	}
	return start, count, nil
}

func splitLinesPreserveLogical(s string) []string {
	if s == "" {
		return nil
	}
	if before, ok := strings.CutSuffix(s, "\n"); ok {
		s = before
	}
	return strings.Split(s, "\n")
}

func diffRequestsTrailingNewline(diff string) bool {
	lines := strings.Split(normaliseDiff(diff), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if line == "" {
			continue
		}
		// "\ No newline at end of file" after the last +/context line means no trailing newline.
		if line == `\ No newline at end of file` {
			return false
		}
		switch line[0] {
		case ' ', '+':
			return true
		case '-':
			return false
		default:
			return false
		}
	}
	return false
}

func normaliseDiff(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}
