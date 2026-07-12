package agent

import (
	_ "embed"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"text/template"
	"time"

	agentspec "github.com/samcharles93/tau/internal/agent/spec"
	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/skills"
)

//go:embed templates/agent.md.tpl
var agentPromptTpl string

// PromptConfig holds all inputs for building the system prompt.
type PromptConfig struct {
	// Tools are registered capability metadata exposed to the LLM. Tool
	// descriptions do not outrank the prompt's behavioral instructions.
	Tools []tools.Schema

	// Skills are the active skill catalog discovered at session start. Full
	// instructions are loaded separately when a skill applies.
	Skills []*skills.Skill

	// ContextFiles are trusted project-level instruction documents (AGENTS.md,
	// etc.) whose precedence is below the current user request.
	ContextFiles []ContextFile

	// Guidelines are additional instruction lines from trusted extensions.
	Guidelines []string

	// CWD is the working directory for the agent session.
	CWD string

	// CustomPrompt replaces the default template if non-empty.
	// Must be a valid Go text/template string.
	CustomPrompt string

	// AppendPrompt contains trusted host-level system instructions appended in a
	// clearly delimited block after the generated prompt.
	// Deprecated: removed per P0.3 decision — trusted-host injection is not a
	// use case worth building a mechanism for. Write a custom agent spec instead.
	AppendPrompt string

	// ModelName is the model identifier for this session (optional metadata).
	ModelName string

	// SessionID is the current session identifier (optional metadata).
	SessionID string
}

// ContextFile is a project context document read from disk.
type ContextFile struct {
	Path    string
	Content string
}

// promptData is the template execution context.
type promptData struct {
	AgentBody     string
	Tools         []tools.Schema
	ContextFiles  []ContextFile
	Guidelines    []string
	SkillsIndex   string
	WorkingDir    string
	WorkspaceTree string
	Platform      string
	Shell         string
	Date          string
	IsGitRepo     bool
	ModelName     string
	SessionID     string
}

// BuildSystemPrompt constructs the full system prompt from the given config
// by executing the agent.md.tpl template (or a custom template).
func BuildSystemPrompt(cfg PromptConfig) string {
	tplSource := agentPromptTpl
	if cfg.CustomPrompt != "" {
		tplSource = cfg.CustomPrompt
	}

	data := buildPromptData(cfg)
	return renderPromptTemplate("agent", tplSource, data)
}

func buildPromptData(cfg PromptConfig) promptData {
	var skillsIndex string
	if len(cfg.Skills) > 0 {
		skillsIndex = skills.ToPromptIndex(cfg.Skills)
	}

	// Render the tau.agent.md spec body into the agent-character slot.
	// It receives the same env/workspace vars as any other spec body.
	// Use renderPromptTemplate directly (not RenderAgentPrompt) to avoid
	// the infinite recursion RenderAgentPrompt → buildPromptData → here.
	tauAgent, ok := agentspec.Lookup("tau")
	agentBody := ""
	if ok {
		agentBody = renderPromptTemplate("tau", tauAgent.Body, promptData{
			WorkingDir:    filepath.ToSlash(cfg.CWD),
			WorkspaceTree: buildWorkspaceTree(cfg.CWD),
			Platform:      runtime.GOOS,
			Shell:         shellName(),
			Date:          time.Now().Format("2006-01-02"),
			IsGitRepo:     isGitRepo(cfg.CWD),
			ModelName:     cfg.ModelName,
			SessionID:     cfg.SessionID,
		})
	}

	return promptData{
		AgentBody:     agentBody,
		Tools:         cfg.Tools,
		ContextFiles:  cfg.ContextFiles,
		Guidelines:    cfg.Guidelines,
		SkillsIndex:   skillsIndex,
		WorkingDir:    filepath.ToSlash(cfg.CWD),
		WorkspaceTree: buildWorkspaceTree(cfg.CWD),
		Platform:      runtime.GOOS,
		Shell:         shellName(),
		Date:          time.Now().Format("2006-01-02"),
		IsGitRepo:     isGitRepo(cfg.CWD),
		ModelName:     cfg.ModelName,
		SessionID:     cfg.SessionID,
	}
}

func renderPromptTemplate(templateName, source string, data promptData) string {
	t, err := template.New(templateName).Funcs(template.FuncMap{
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

// RenderAgentPrompt renders a built-in agent definition's prompt body against
// the given working directory. def is expected to come from
// [agentspec.Builtins] or [agentspec.Lookup].
func RenderAgentPrompt(def *agentspec.Definition, cwd string) string {
	return renderPromptTemplate(def.Name, def.Body, buildPromptData(PromptConfig{CWD: cwd}))
}

// DiscoverContextFiles finds project-level context documents (AGENTS.md, etc.)
// by walking parent directories from CWD up to the filesystem root and
// collecting every matching file in root→CWD order.
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

	// Build the list of directories from filesystem root down to CWD (inclusive).
	var dirs []string
	for d := cwd; ; {
		dirs = append(dirs, d)
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
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
