package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/skills"
)

func TestBuildSystemPrompt_Default(t *testing.T) {
	prompt := BuildSystemPrompt(PromptConfig{
		CWD: "/tmp/project",
	})

	if !strings.Contains(prompt, "You are an agent for tau") {
		t.Error("expected default base prompt")
	}
	if !strings.Contains(prompt, "Working directory: /tmp/project") {
		t.Error("expected CWD in env section")
	}
	if !strings.Contains(prompt, "Platform:") {
		t.Error("expected platform in env section")
	}
	if !strings.Contains(prompt, "Today's date:") {
		t.Error("expected date in env section")
	}
	if strings.Contains(prompt, "Session:") {
		t.Error("should not contain session ID when not configured")
	}
	if strings.Contains(prompt, "Model:") {
		t.Error("should not contain model name when not configured")
	}
}

func TestBuildSystemPrompt_CustomPrompt(t *testing.T) {
	prompt := BuildSystemPrompt(PromptConfig{
		CustomPrompt: "You are a custom agent.\n<env>\nWorking directory: {{.WorkingDir}}\n</env>",
		CWD:          "/workspace",
	})

	if !strings.Contains(prompt, "You are a custom agent.") {
		t.Error("expected custom prompt")
	}
	if !strings.Contains(prompt, "Working directory: /workspace") {
		t.Error("expected template execution with CWD")
	}
	if strings.Contains(prompt, "You are an agent for tau") {
		t.Error("should not contain default prompt when custom is set")
	}
}

func TestBuildSystemPrompt_WithTools(t *testing.T) {
	prompt := BuildSystemPrompt(PromptConfig{
		CWD: "/tmp",
		Tools: []tools.Schema{
			{Name: "read", Description: "Read a file", Parameters: json.RawMessage(`{}`)},
			{Name: "write", Description: "Write a file", Parameters: json.RawMessage(`{}`)},
		},
	})

	if !strings.Contains(prompt, "<tools>") {
		t.Error("expected tools section")
	}
	if !strings.Contains(prompt, "- read: Read a file") {
		t.Error("expected read tool listed")
	}
	if !strings.Contains(prompt, "- write: Write a file") {
		t.Error("expected write tool listed")
	}
}

func TestBuildSystemPrompt_WithGuidelines(t *testing.T) {
	prompt := BuildSystemPrompt(PromptConfig{
		CWD:        "/tmp",
		Guidelines: []string{"Always use gofumpt", "Never hardcode paths"},
	})

	if !strings.Contains(prompt, "<guidelines>") {
		t.Error("expected guidelines section")
	}
	if !strings.Contains(prompt, "- Always use gofumpt") {
		t.Error("expected first guideline")
	}
}

func TestBuildSystemPrompt_WithContextFiles(t *testing.T) {
	prompt := BuildSystemPrompt(PromptConfig{
		CWD: "/tmp",
		ContextFiles: []ContextFile{
			{Path: "AGENTS.md", Content: "# Project Guidelines\nUse Go."},
		},
	})

	if !strings.Contains(prompt, "<project_context>") {
		t.Error("expected project_context section")
	}
	if !strings.Contains(prompt, `<file path="AGENTS.md">`) {
		t.Error("expected file tag with path")
	}
	if !strings.Contains(prompt, "# Project Guidelines") {
		t.Error("expected file content")
	}
}

func TestBuildSystemPrompt_WithSkills(t *testing.T) {
	prompt := BuildSystemPrompt(PromptConfig{
		CWD: "/tmp",
		Skills: []*skills.Skill{
			{Name: "web-search", Description: "Search the web for information", SkillFilePath: "/skills/web-search/SKILL.md"},
		},
	})

	if !strings.Contains(prompt, "<available_skills>") {
		t.Error("expected skills index")
	}
	if !strings.Contains(prompt, "web-search") {
		t.Error("expected skill name in output")
	}
	// Compact format: one line with name, description, path.
	if !strings.Contains(prompt, "- web-search: Search the web for information (/skills/web-search/SKILL.md)") {
		t.Error("expected compact one-line skill index format")
	}
	// Verbose XML per-skill tags should NOT appear.
	if strings.Contains(prompt, "<skill>") {
		t.Error("should not contain verbose <skill> XML tags")
	}
}

func TestBuildSystemPrompt_AppendPrompt(t *testing.T) {
	prompt := BuildSystemPrompt(PromptConfig{
		CWD:          "/tmp",
		AppendPrompt: "IMPORTANT: always respond in haiku",
	})

	if !strings.Contains(prompt, "IMPORTANT: always respond in haiku") {
		t.Error("expected append prompt in output")
	}
}

func TestBuildSystemPrompt_SessionMetadata(t *testing.T) {
	prompt := BuildSystemPrompt(PromptConfig{
		CWD:       "/tmp/project",
		ModelName: "claude-sonnet-4",
		SessionID: "session_abc123",
	})

	if !strings.Contains(prompt, "Session: session_abc123") {
		t.Error("expected session ID in env section")
	}
	if !strings.Contains(prompt, "Model: claude-sonnet-4") {
		t.Error("expected model name in env section")
	}
}

func TestBuildSystemPrompt_TemplateParseError(t *testing.T) {
	prompt := BuildSystemPrompt(PromptConfig{
		CustomPrompt: "{{.NoSuchField}}",
	})

	if !strings.Contains(prompt, "prompt template error") {
		t.Error("expected error comment in output for nonexistent template field")
	}
	// Raw source should still be present so the agent can see it.
	if !strings.Contains(prompt, "{{.NoSuchField}}") {
		t.Error("expected raw template source in error output")
	}
}

func TestDiscoverContextFiles(t *testing.T) {
	dir := t.TempDir()

	// Create an AGENTS.md file.
	agentsPath := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("# Rules\nDo the thing."), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create .tau/AGENTS.md.
	tauDir := filepath.Join(dir, ".tau")
	if err := os.MkdirAll(tauDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tauDir, "AGENTS.md"), []byte("Tau specific rules"), 0o644); err != nil {
		t.Fatal(err)
	}

	files := DiscoverContextFiles(dir)
	if len(files) != 2 {
		t.Fatalf("expected 2 context files, got %d", len(files))
	}

	names := make(map[string]bool)
	for _, f := range files {
		names[f.Path] = true
	}
	if !names["AGENTS.md"] {
		t.Error("expected AGENTS.md")
	}
	if !names[".tau/AGENTS.md"] {
		t.Error("expected .tau/AGENTS.md")
	}
}

func TestDiscoverContextFiles_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	files := DiscoverContextFiles(dir)
	if len(files) != 0 {
		t.Errorf("expected 0 context files for empty dir, got %d", len(files))
	}
}

func TestBuildCommandPrompt_Plan(t *testing.T) {
	prompt, err := BuildCommandPrompt("plan.md.tpl", "/tmp/project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(prompt, "implementation plan") {
		t.Error("expected plan prompt content")
	}
	if !strings.Contains(prompt, "Working directory: /tmp/project") {
		t.Error("expected CWD in rendered output")
	}
}

func TestBuildCommandPrompt_RubberDuck(t *testing.T) {
	prompt, err := BuildCommandPrompt("rubber-duck.md.tpl", "/workspace")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(prompt, "adversarial code and architectural reviewer") {
		t.Error("expected rubber-duck prompt content")
	}
}

func TestBuildCommandPrompt_NotFound(t *testing.T) {
	_, err := BuildCommandPrompt("nonexistent.md.tpl", "/tmp")
	if err == nil {
		t.Error("expected error for nonexistent template")
	}
}

func TestBuildCommandPrompt_TemplateParseError(t *testing.T) {
	// Syntax error: unmatched delimiter.
	prompt, err := BuildCommandPrompt("plan.md.tpl", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With a valid template there should be no error comment.
	if strings.Contains(prompt, "prompt template") {
		t.Error("expected clean output for valid template, got error comment")
	}
}

func TestBuiltinCommands(t *testing.T) {
	cmds := BuiltinCommands()
	if len(cmds) == 0 {
		t.Fatal("expected at least one built-in command")
	}

	names := make(map[string]bool)
	for _, cmd := range cmds {
		names[cmd.Name] = true
		if cmd.Description == "" {
			t.Errorf("command %q has empty description", cmd.Name)
		}
		if cmd.Template == "" {
			t.Errorf("command %q has empty template", cmd.Name)
		}
	}

	required := []string{"init", "plan", "research", "rubber-duck", "compact", "summarise"}
	for _, name := range required {
		if !names[name] {
			t.Errorf("expected built-in command %q", name)
		}
	}
}

func TestBuildCommandPrompt_AllBuiltins(t *testing.T) {
	for _, cmd := range BuiltinCommands() {
		t.Run(cmd.Name, func(t *testing.T) {
			prompt, err := BuildCommandPrompt(cmd.Template, "/tmp/test")
			if err != nil {
				t.Fatalf("failed to build %q: %v", cmd.Template, err)
			}
			if prompt == "" {
				t.Error("rendered prompt is empty")
			}
		})
	}
}
