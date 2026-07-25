// Package prompttmpl provides the shared template contract for agent spec
// bodies rendered by root and child processes.
package prompttmpl

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"text/template"
	"time"
)

// Data is the environment and session metadata available to every agent spec
// body, regardless of whether it is rendered for a root or child process.
type Data struct {
	WorkingDir    string
	WorkspaceTree string
	Platform      string
	Shell         string
	Date          string
	IsGitRepo     bool
	ModelName     string
	SessionID     string
}

// NewData builds the shared spec-body template data.
func NewData(cwd, modelName, sessionID string, now time.Time) Data {
	return Data{
		WorkingDir:    filepath.ToSlash(cwd),
		WorkspaceTree: buildWorkspaceTree(cwd),
		Platform:      runtime.GOOS,
		Shell:         shellName(),
		Date:          now.Format("2006-01-02"),
		IsGitRepo:     isGitRepo(cwd),
		ModelName:     modelName,
		SessionID:     sessionID,
	}
}

func buildWorkspaceTree(root string) string {
	if root == "" {
		return ""
	}

	var b strings.Builder
	collectTreeLines(root, 0, &b)
	return strings.TrimSpace(b.String())
}

var noisyDirs = map[string]bool{
	".git":          true,
	"node_modules":  true,
	"__pycache__":   true,
	".pytest_cache": true,
	".ruff_cache":   true,
	"dist":          true,
	"build":         true,
	"out":           true,
	"target":        true,
	".next":         true,
}

func collectTreeLines(dir string, depth int, b *strings.Builder) {
	const maxDepth = 2
	const maxEntries = 20

	if depth >= maxDepth {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name() < entries[j].Name()
	})

	count := 0
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || noisyDirs[name] {
			continue
		}
		if count >= maxEntries {
			fmt.Fprintf(b, "%s- ... %d more entries\n", strings.Repeat("  ", depth), len(entries)-count)
			return
		}
		count++

		suffix := ""
		if entry.IsDir() {
			suffix = "/"
		}
		fmt.Fprintf(b, "%s- %s%s\n", strings.Repeat("  ", depth), name, suffix)
		if entry.IsDir() {
			collectTreeLines(filepath.Join(dir, name), depth+1, b)
		}
	}
}

// RenderSpec executes an agent spec body with the shared function map and
// preserves the source in-band when parsing or execution fails.
func RenderSpec(name, source string, data any) string {
	t, err := template.New(name).Funcs(template.FuncMap{
		"xml": html.EscapeString,
	}).Parse(source)
	if err != nil {
		return fmt.Sprintf("<!-- prompt template parse error: %v -->\n%s", err, source)
	}

	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return fmt.Sprintf("<!-- prompt template error: %v -->\n%s", err, source)
	}
	return b.String()
}

func shellName() string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		return "unknown"
	}
	return filepath.Base(sh)
}

func isGitRepo(dir string) bool {
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}
