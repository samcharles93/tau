package agent

import (
	"embed"
	"os"
	"path/filepath"
	"runtime"
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
	Tools        []tools.Schema
	ContextFiles []ContextFile
	Guidelines   []string
	SkillsXML    string
	WorkingDir   string
	Platform     string
	Date         string
	IsGitRepo    bool
	AppendPrompt string
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
		Tools:        cfg.Tools,
		ContextFiles: cfg.ContextFiles,
		Guidelines:   cfg.Guidelines,
		SkillsXML:    skillsXML,
		WorkingDir:   filepath.ToSlash(cfg.CWD),
		Platform:     runtime.GOOS,
		Date:         time.Now().Format("2006-01-02"),
		IsGitRepo:    isGitRepo(cfg.CWD),
		AppendPrompt: cfg.AppendPrompt,
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
// by searching standard locations relative to the working directory.
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

	var found []ContextFile
	for _, relPath := range candidates {
		fullPath := filepath.Join(cwd, relPath)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		trimmed := strings.TrimSpace(string(content))
		if trimmed == "" {
			continue
		}
		found = append(found, ContextFile{
			Path:    relPath,
			Content: trimmed,
		})
	}
	return found
}

func isGitRepo(dir string) bool {
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}
