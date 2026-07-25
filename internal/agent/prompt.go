package agent

import (
	_ "embed"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/agent/prompttmpl"
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
	// Deprecated: removed per P0.3 decision - trusted-host injection is not a
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
	prompttmpl.Data
	AgentBody    string
	Tools        []tools.Schema
	ContextFiles []ContextFile
	Guidelines   []string
	SkillsIndex  string
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
	specData := prompttmpl.NewData(cfg.CWD, cfg.ModelName, cfg.SessionID, time.Now())

	// Render the tau.agent.md spec body into the agent-character slot.
	// It receives the same env/workspace vars as any other spec body.
	tauAgent, ok := agentspec.Lookup("tau")
	agentBody := ""
	if ok {
		agentBody = prompttmpl.RenderSpec("tau", tauAgent.Body, specData)
	}

	return promptData{
		Data:         specData,
		AgentBody:    agentBody,
		Tools:        cfg.Tools,
		ContextFiles: cfg.ContextFiles,
		Guidelines:   cfg.Guidelines,
		SkillsIndex:  skillsIndex,
	}
}

func renderPromptTemplate(templateName, source string, data promptData) string {
	return prompttmpl.RenderSpec(templateName, source, data)
}

// RenderAgentPrompt renders a built-in agent definition's prompt body against
// the given working directory. def is expected to come from
// [agentspec.Builtins] or [agentspec.Lookup].
func RenderAgentPrompt(def *agentspec.Definition, cwd string) string {
	return prompttmpl.RenderSpec(
		def.Name,
		def.Body,
		prompttmpl.NewData(cwd, "", "", time.Now()),
	)
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
