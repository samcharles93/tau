package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// GrepParams are the parameters for the grep tool.
type GrepParams struct {
	Pattern       string `json:"pattern"`
	Path          string `json:"path,omitempty"`    // file or directory
	Include       string `json:"include,omitempty"` // glob pattern for file names
	IsRegex       bool   `json:"is_regex,omitempty"`
	CaseSensitive bool   `json:"case_sensitive,omitempty"`
	ContextBefore int    `json:"context_before,omitempty"` // lines before each match (-B)
	ContextAfter  int    `json:"context_after,omitempty"`  // lines after each match (-A)
}

var grepSchema = Schema{
	Name:        "grep",
	Description: "Search for a pattern in files using ripgrep (rg). Respects .gitignore. Returns matching lines with file paths and line numbers. Use context_before/context_after to show surrounding lines.",
	Parameters: json.RawMessage(`{
		"type": "object",
		"properties": {
			"pattern": {
				"type": "string",
				"description": "Search pattern (literal text or regex)"
			},
			"path": {
				"type": "string",
				"description": "File or directory to search in. Defaults to current directory."
			},
			"include": {
				"type": "string",
				"description": "Glob pattern for file inclusion (e.g. '*.go', '*.ts')"
			},
			"is_regex": {
				"type": "boolean",
				"description": "Treat pattern as a regex. Defaults to false (literal)."
			},
			"case_sensitive": {
				"type": "boolean",
				"description": "Case-sensitive search. Defaults to false (smart case)."
			},
			"context_before": {
				"type": "integer",
				"description": "Number of lines to show before each match (-B). Useful for seeing context without a follow-up read."
			},
			"context_after": {
				"type": "integer",
				"description": "Number of lines to show after each match (-A). Useful for seeing context without a follow-up read."
			}
		},
		"required": ["pattern"]
	}`),
}

// NewGrepTool creates the built-in grep tool.
func NewGrepTool(cwd string) Tool {
	return Tool{
		Schema:  grepSchema,
		Source:  "builtin",
		Execute: makeGrepExecutor(cwd),
	}
}

func makeGrepExecutor(cwd string) Executor {
	return func(ctx context.Context, params json.RawMessage, _ UIBridge) (Result, error) {
		var p GrepParams
		if err := json.Unmarshal(params, &p); err != nil {
			return Result{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
		}

		if strings.TrimSpace(p.Pattern) == "" {
			return Result{Content: "pattern is required", IsError: true}, nil
		}

		ctx, cancel := context.WithTimeout(ctx, DefaultToolTimeout)
		defer cancel()

		searchPath := cwd
		if p.Path != "" {
			searchPath = resolvePath(cwd, p.Path)
		}

		if !isConfined(cwd, searchPath) {
			return Result{Content: "error: path escapes working directory", IsError: true}, nil
		}

		args := buildGrepArgs(p)
		args = append(args, searchPath)

		binary, err := grepBinary()
		if err != nil {
			// No external binary available — use pure-Go fallback.
			output, err := grepFallback(ctx, p, searchPath, cwd)
			if err != nil {
				return Result{Content: fmt.Sprintf("grep error: %v", err), IsError: true}, nil
			}
			if output == "" {
				return Result{Content: "no matches found"}, nil
			}
			tr := TruncateHead(output, DefaultMaxLines, DefaultMaxBytes)
			return Result{Content: tr.Content}, nil
		}

		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = cwd

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err = cmd.Run()

		output := stdout.String()
		if output == "" && err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				return Result{Content: "no matches found"}, nil
			}
			errMsg := stderr.String()
			if errMsg == "" {
				errMsg = err.Error()
			}
			return Result{Content: fmt.Sprintf("grep error: %s", errMsg), IsError: true}, nil
		}

		tr := TruncateHead(output, DefaultMaxLines, DefaultMaxBytes)
		return Result{Content: tr.Content}, nil
	}
}

// grepFallback performs a pure-Go file scan for when ripgrep is not available.
// Works on all platforms including Windows.
func grepFallback(ctx context.Context, p GrepParams, searchPath, cwd string) (string, error) {
	info, err := os.Stat(searchPath)
	if err != nil {
		return "", err
	}

	var matcher func(line string) bool
	if p.IsRegex {
		var re *regexp.Regexp
		if p.CaseSensitive || hasUppercase(p.Pattern) {
			re, err = regexp.Compile(p.Pattern)
		} else {
			re, err = regexp.Compile("(?i:" + p.Pattern + ")")
		}
		if err != nil {
			return "", fmt.Errorf("invalid regex: %w", err)
		}
		matcher = func(line string) bool {
			return re.MatchString(line)
		}
	} else {
		if p.CaseSensitive {
			matcher = func(line string) bool {
				return strings.Contains(line, p.Pattern)
			}
		} else {
			lowerPattern := strings.ToLower(p.Pattern)
			matcher = func(line string) bool {
				return strings.Contains(strings.ToLower(line), lowerPattern)
			}
		}
	}

	var results []grepResult

	if !info.IsDir() {
		res, err := grepFile(ctx, searchPath, matcher, p.ContextBefore, p.ContextAfter)
		if err != nil {
			return "", err
		}
		for i := range res {
			res[i].relPath, _ = filepath.Rel(cwd, searchPath)
			if res[i].relPath == "" || res[i].relPath == "." {
				res[i].relPath = filepath.Base(searchPath)
			}
		}
		results = append(results, res...)
	} else {
		err = filepath.WalkDir(searchPath, func(walkPath string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // skip inaccessible
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if d.IsDir() {
				// Skip hidden directories.
				if d.Name() != "" && d.Name()[0] == '.' && walkPath != searchPath {
					return filepath.SkipDir
				}
				return nil
			}

			// Skip hidden files.
			if d.Name() != "" && d.Name()[0] == '.' {
				return nil
			}

			// Include filter.
			if p.Include != "" {
				if matched, _ := filepath.Match(p.Include, d.Name()); !matched {
					return nil
				}
			}

			res, err := grepFile(ctx, walkPath, matcher, p.ContextBefore, p.ContextAfter)
			if err != nil {
				return nil // skip files we can't read
			}
			for i := range res {
				res[i].relPath, _ = filepath.Rel(cwd, walkPath)
				if res[i].relPath == "" || res[i].relPath == "." {
					res[i].relPath = filepath.Base(walkPath)
				}
			}
			results = append(results, res...)
			return nil
		})
		if err != nil && err != ctx.Err() {
			return "", err
		}
	}

	if len(results) == 0 {
		return "", nil
	}

	// Format results similarly to ripgrep: path:line:content
	var lines []string
	for _, r := range results {
		if r.isContext {
			lines = append(lines, fmt.Sprintf("%s:%d:%s", r.relPath, r.lineNum, r.content))
		} else {
			lines = append(lines, fmt.Sprintf("%s:%d:%s", r.relPath, r.lineNum, r.content))
		}
	}

	// Remove duplicate lines that can occur from overlapping context.
	lines = dedupLines(lines)

	return strings.Join(lines, "\n"), nil
}

type grepResult struct {
	relPath   string
	lineNum   int
	content   string
	isContext bool
}

func grepFile(ctx context.Context, path string, matcher func(string) bool, ctxBefore, ctxAfter int) ([]grepResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	str := string(data)
	allLines := strings.Split(str, "\n")

	var results []grepResult
	for i, line := range allLines {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if matcher(line) {
			// Add context lines before.
			start := max(i-ctxBefore, 0)
			for j := start; j < i; j++ {
				results = append(results, grepResult{
					lineNum:   j + 1,
					content:   allLines[j],
					isContext: true,
				})
			}

			// Add match line.
			results = append(results, grepResult{
				lineNum:   i + 1,
				content:   line,
				isContext: false,
			})

			// Add context lines after.
			end := i + ctxAfter
			if end >= len(allLines) {
				end = len(allLines) - 1
			}
			for j := i + 1; j <= end; j++ {
				results = append(results, grepResult{
					lineNum:   j + 1,
					content:   allLines[j],
					isContext: true,
				})
			}
		}
	}

	return results, nil
}

func dedupLines(lines []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, l := range lines {
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	return out
}

func buildGrepArgs(p GrepParams) []string {
	args := []string{"--line-number", "--no-heading", "--color=never"}

	if !p.IsRegex {
		args = append(args, "--fixed-strings")
	}

	if !p.CaseSensitive {
		args = append(args, "--smart-case")
	} else {
		args = append(args, "--case-sensitive")
	}

	if p.Include != "" {
		args = append(args, "--glob", p.Include)
	}

	if p.ContextBefore > 0 {
		args = append(args, fmt.Sprintf("-B%d", p.ContextBefore))
	}
	if p.ContextAfter > 0 {
		args = append(args, fmt.Sprintf("-A%d", p.ContextAfter))
	}

	args = append(args, "--", p.Pattern)
	return args
}

func grepBinary() (string, error) {
	// Prefer ripgrep; signal fallback if not found.
	if path, err := exec.LookPath("rg"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("ripgrep (rg) not found in PATH")
}

// hasUppercase reports whether s contains any uppercase ASCII letter.
func hasUppercase(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			return true
		}
	}
	return false
}
