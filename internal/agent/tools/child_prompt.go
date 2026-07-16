package tools

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"

	"github.com/samcharles93/tau/internal/agent/spec"
)

// childPromptData mirrors internal/agent.promptData's field names exactly
// (a subset - WorkspaceTree/Tools/ContextFiles/Guidelines/SkillsIndex/
// AgentBody are root-system-prompt composition slots, not used by a plain
// spec body), so the same {{.WorkingDir}}-style template vars in spec
// bodies render identically whether the spec is the root's own identity or
// a spawned child's. Duplicated here rather than imported: internal/agent
// imports internal/agent/tools (for the tool registry), so tools importing
// internal/agent back would be a cycle.
type childPromptData struct {
	WorkingDir string
	Platform   string
	Shell      string
	Date       string
	IsGitRepo  bool
	ModelName  string
	SessionID  string
}

// renderChildSystemPrompt renders a spawned child's own spec body into its
// system prompt - the child's identity, not the parent's. Mirrors
// internal/agent.RenderAgentPrompt's template semantics (same xml funcmap,
// same field names) so spec bodies behave identically regardless of
// whether they're resolved as the root or as a spawned child.
func renderChildSystemPrompt(def *spec.Definition, cwd, modelName, sessionID string) string {
	data := childPromptData{
		WorkingDir: filepath.ToSlash(cwd),
		Platform:   runtime.GOOS,
		Shell:      shellName(),
		Date:       time.Now().Format("2006-01-02"),
		IsGitRepo:  isGitRepo(cwd),
		ModelName:  modelName,
		SessionID:  sessionID,
	}

	t, err := template.New(def.Name).Funcs(template.FuncMap{
		"xml": html.EscapeString,
	}).Parse(def.Body)
	if err != nil {
		return fmt.Sprintf("<!-- prompt template parse error: %v -->\n%s", err, def.Body)
	}

	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return fmt.Sprintf("<!-- prompt template error: %v -->\n%s", err, def.Body)
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
