package agent

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/skills"
)

//go:embed templates/agent.md.tpl
var agentPromptTpl string

//go:embed templates/*.md.tpl
var templateFS embed.FS

// BuiltinCommand defines a built-in slash command backed by a template.
type BuiltinCommand struct {
	Name        string
	Description string
	Template    string // filename within templates/ (e.g. "plan.md.tpl")
}

// BuiltinCommands returns the set of template-backed built-in commands.
// These inject a system/user prompt for a specific task mode.
func BuiltinCommands() []BuiltinCommand {
	return []BuiltinCommand{
		{Name: "init", Description: "Generate an AGENTS.md for the current project", Template: "init.md.tpl"},
		{Name: "plan", Description: "Create an implementation plan before coding", Template: "plan.md.tpl"},
		{Name: "research", Description: "Run a deep research investigation", Template: "research.md.tpl"},
		{Name: "rubber-duck", Description: "Get an independent critique of current work", Template: "rubber-duck.md.tpl"},
		{Name: "compact", Description: "Compact conversation to preserve context", Template: "compact.md.tpl"},
		{Name: "summarise", Description: "Summarize conversation for later continuation", Template: "summarise.md.tpl"},
	}
}

// PromptConfig holds all inputs for building the system prompt.
type PromptConfig struct {
	// Tools are the registered tool schemas to expose to the LLM.
	Tools []tools.Schema

	// Skills are the active skills discovered at session start.
	Skills []*skills.Skill

	// ContextFiles are project-level context documents (AGENTS.md, etc.).
	ContextFiles []ContextFile

	// Guidelines are additional instruction lines (from extensions via hooks).
	Guidelines []string

	// CWD is the working directory for the agent session.
	CWD string

	// CustomPrompt replaces the default template if non-empty.
	// Must be a valid Go text/template string.
	CustomPrompt string

	// AppendPrompt is appended after the generated prompt.
	AppendPrompt string
}

// ContextFile is a project context document read from disk.
type ContextFile struct {
	Path    string
	Content string
}

// promptData is the template execution context.
type promptData struct {
	Tools         []tools.Schema
	ContextFiles  []ContextFile
	Guidelines    []string
	SkillsXML     string
	WorkingDir    string
	WorkspaceTree string
	Platform      string
	Shell         string
	Date          string
	IsGitRepo     bool
	AppendPrompt  string
}

// BuildSystemPrompt constructs the full system prompt from the given config
// by executing the agent.md.tpl template (or a custom template).
func BuildSystemPrompt(cfg PromptConfig) string {
	tplSource := agentPromptTpl
	if cfg.CustomPrompt != "" {
		tplSource = cfg.CustomPrompt
	}

	t, err := template.New("agent").Parse(tplSource)
	if err != nil {
		// Fallback: return the raw template source on parse failure.
		return tplSource
	}

	var skillsXML string
	if len(cfg.Skills) > 0 {
		skillsXML = skills.ToPromptXML(cfg.Skills)
	}

	data := promptData{
		Tools:         cfg.Tools,
		ContextFiles:  cfg.ContextFiles,
		Guidelines:    cfg.Guidelines,
		SkillsXML:     skillsXML,
		WorkingDir:    filepath.ToSlash(cfg.CWD),
		WorkspaceTree: buildWorkspaceTree(cfg.CWD),
		Platform:      runtime.GOOS,
		Shell:         shellName(),
		Date:          time.Now().Format("2006-01-02"),
		IsGitRepo:     isGitRepo(cfg.CWD),
		AppendPrompt:  cfg.AppendPrompt,
	}

	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return tplSource
	}

	return b.String()
}

// BuildCommandPrompt renders a built-in command template by name.
// The name should match a filename in templates/ (e.g. "plan.md.tpl").
// Returns the rendered prompt, or an error if the template is not found.
func BuildCommandPrompt(templateName, cwd string) (string, error) {
	content, err := templateFS.ReadFile("templates/" + templateName)
	if err != nil {
		return "", err
	}

	t, err := template.New(templateName).Parse(string(content))
	if err != nil {
		return string(content), nil
	}

	data := struct {
		WorkingDir string
		Platform   string
		Date       string
		IsGitRepo  bool
	}{
		WorkingDir: filepath.ToSlash(cwd),
		Platform:   runtime.GOOS,
		Date:       time.Now().Format("2006-01-02"),
		IsGitRepo:  isGitRepo(cwd),
	}

	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return string(content), nil
	}
	return b.String(), nil
}

// DiscoverContextFiles finds project-level context documents (AGENTS.md, etc.)
// by walking from the git root (or CWD) down to the CWD, collecting every
// matching file along the path in root→CWD order. This matches the Codex
// hierarchical discovery spec.
func DiscoverContextFiles(cwd string) []ContextFile {
	if cwd == "" {
		return nil
	}

	candidates := []string{
		"AGENTS.md",
		".tau/AGENTS.md",
		".github/copilot-instructions.md",
		".cursorrules",
	}

	// Walk upward from CWD to find the git root.
	root := cwd
	for d := cwd; d != "" && d != string(filepath.Separator); d = filepath.Dir(d) {
		if isGitRepo(d) {
			root = d
			break
		}
	}

	// Build the list of directories from root down to CWD (inclusive).
	var dirs []string
	for d := cwd; ; d = filepath.Dir(d) {
		dirs = append(dirs, d)
		if d == root || d == string(filepath.Separator) || d == "" {
			break
		}
	}
	// Reverse so we go root→CWD.
	slices.Reverse(dirs)

	var found []ContextFile
	seen := make(map[string]bool)
	for _, d := range dirs {
		for _, relPath := range candidates {
			fullPath := filepath.Join(d, relPath)
			if seen[fullPath] {
				continue
			}
			seen[fullPath] = true
			content, err := os.ReadFile(fullPath)
			if err != nil {
				continue
			}
			trimmed := strings.TrimSpace(string(content))
			if trimmed == "" {
				continue
			}
			displayPath, _ := filepath.Rel(cwd, fullPath)
			if displayPath == "" {
				displayPath = relPath
			}
			found = append(found, ContextFile{
				Path:    displayPath,
				Content: trimmed,
			})
		}
	}

	return found
}

// shellName returns the user's shell from $SHELL, or "unknown" if unset.
func shellName() string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		return "unknown"
	}
	// Return just the basename for readability.
	return filepath.Base(sh)
}

// buildWorkspaceTree returns a bounded directory tree for the given path,
// showing up to 2 levels deep with at most 20 entries per directory.
// Noisy directories (.git, node_modules, etc.) are filtered out.
func buildWorkspaceTree(root string) string {
	if root == "" {
		return ""
	}

	var sb strings.Builder
	collectTreeLines(root, 0, &sb)
	return strings.TrimSpace(sb.String())
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

func collectTreeLines(dir string, depth int, sb *strings.Builder) {
	const maxDepth = 2
	const maxEntries = 20

	if depth >= maxDepth {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	// Directories first, then files.
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
			fmt.Fprintf(sb, "%s- ... %d more entries\n", strings.Repeat("  ", depth), len(entries)-count)
			return
		}
		count++

		suffix := ""
		if entry.IsDir() {
			suffix = "/"
		}
		fmt.Fprintf(sb, "%s- %s%s\n", strings.Repeat("  ", depth), name, suffix)
		if entry.IsDir() {
			collectTreeLines(filepath.Join(dir, name), depth+1, sb)
		}
	}
}

func isGitRepo(dir string) bool {
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}
