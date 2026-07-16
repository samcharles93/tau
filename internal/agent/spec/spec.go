// Package spec defines tau's built-in agent commands as declarative
// frontmatter + prompt-template files, modeled on GitHub Copilot's custom
// agent spec (name/description/tools/model/user-invocable) but scoped to
// what tau's coordinator can actually enforce today.
//
// This package is intentionally a leaf: it has no dependency on the agent
// coordinator or the command registry, so both can depend on it without
// creating an import cycle (the coordinator already depends on the registry).
package spec

import (
	"embed"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/samcharles93/tau/internal/skills"
)

//go:embed templates/*.agent.md
var templateFS embed.FS

// builtinFiles is the deterministic load and display order for built-in
// agent definitions.
var builtinFiles = []string{
	"tau.agent.md",
	"init.agent.md",
	"plan.agent.md",
	"research.agent.md",
	"rubber-duck.agent.md",
	"compact.agent.md",
	"summarise.agent.md",
	// task.agent.md was retired in P0.4 - tau is now spawnable and
	// fills the general-purpose child-worker role; task's body was a
	// subset of tau's personality. File kept for reference.
}

// Definition is a built-in agent's declarative spec plus its unrendered
// prompt body.
type Definition struct {
	Name        string
	Description string

	// Tools restricts the tool registry to this set while the agent is
	// active. Nil/empty means every registered tool stays available.
	Tools []string

	// Model is reserved for a future per-agent model override. It is parsed
	// and validated but not yet applied by the coordinator.
	Model string

	// Provider is the provider to use when Model is set to a concrete
	// (non-tier) model name. Only meaningful alongside a concrete Model;
	// tiers carry their own provider via config model_modes.
	Provider string

	// MaxTurns is a structural cap on agentic-loop iterations per assigned
	// task when running as a process. Zero means "defer to config default".
	MaxTurns int

	// Timeout is the default wall-clock limit per assigned task when running
	// as a process. Zero means "defer to config default".
	Timeout time.Duration

	// DisableModelInvocation is reserved for future model-driven agent
	// selection, mirroring skills.Skill.DisableModelInvocation. Tau has no
	// such mechanism today, so this is not yet enforced.
	DisableModelInvocation bool

	// UserInvocable controls whether the agent is offered as a slash
	// command. Defaults to true.
	UserInvocable bool

	// ModeSwitcher controls whether the agent appears in the Shift-Tab mode
	// cycle. Defaults to UserInvocable so ordinary user-facing agents remain
	// available unless they explicitly opt out.
	ModeSwitcher bool

	// ArgumentHint is shown next to the command name in /help, e.g. "<prompt>".
	ArgumentHint string

	// DisplayName is the name shown for this agent's input-mode indicator
	// (the labeled divider surrounding the chat input while the command is
	// being typed), e.g. "Planning" for /plan. Defaults to the title-cased
	// Name when not set.
	DisplayName string

	// Color is an xterm-256 palette index (e.g. "134"), as a string so this
	// leaf package doesn't need a termkit dependency - internal/tui converts
	// it. Empty means "use the default agent-mode accent."
	Color string

	Metadata map[string]string

	// Body is the unrendered Go text/template prompt.
	Body string

	// Scope is set by DiscoverFromDisk for filesystem-loaded definitions
	// (skills.ScopeUser or skills.ScopeProject). Zero value for embedded
	// built-ins.
	Scope skills.Scope

	// SourcePath is the file a filesystem-loaded definition was parsed
	// from. Empty for embedded built-ins.
	SourcePath string
}

// frontmatter mirrors Definition's fields for YAML decoding. UserInvocable is
// a pointer so a missing key can be distinguished from an explicit false and
// defaulted to true.
type frontmatter struct {
	Name                   string            `yaml:"name"`
	Description            string            `yaml:"description"`
	Tools                  []string          `yaml:"tools,omitempty"`
	Model                  string            `yaml:"model,omitempty"`
	Provider               string            `yaml:"provider,omitempty"`
	MaxTurns               int               `yaml:"max-turns,omitempty"`
	Timeout                string            `yaml:"timeout,omitempty"`
	DisableModelInvocation bool              `yaml:"disable-model-invocation,omitempty"`
	UserInvocable          *bool             `yaml:"user-invocable,omitempty"`
	ModeSwitcher           *bool             `yaml:"mode-switcher,omitempty"`
	ArgumentHint           string            `yaml:"argument-hint,omitempty"`
	DisplayName            string            `yaml:"display-name,omitempty"`
	Color                  string            `yaml:"color,omitempty"`
	Metadata               map[string]string `yaml:"metadata,omitempty"`
}

// titleCase uppercases the first rune of s, leaving the rest unchanged - used
// to derive a default DisplayName ("plan" -> "Plan") for agent commands that
// don't set display-name explicitly.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// Parse parses a single agent definition file (YAML frontmatter + Go
// text/template body).
func Parse(content []byte) (*Definition, error) {
	fm, body, err := skills.SplitFrontmatter(string(content))
	if err != nil {
		return nil, err
	}

	var parsed frontmatter
	if err := skills.UnmarshalFrontmatter(fm, &parsed); err != nil {
		return nil, fmt.Errorf("parsing frontmatter: %w", err)
	}

	name := strings.TrimSpace(parsed.Name)
	if name == "" {
		return nil, errors.New("name is required")
	}
	description := strings.TrimSpace(parsed.Description)
	if description == "" {
		return nil, errors.New("description is required")
	}

	userInvocable := true
	if parsed.UserInvocable != nil {
		userInvocable = *parsed.UserInvocable
	}
	modeSwitcher := userInvocable
	if parsed.ModeSwitcher != nil {
		modeSwitcher = *parsed.ModeSwitcher
	}

	displayName := strings.TrimSpace(parsed.DisplayName)
	if displayName == "" {
		displayName = titleCase(name)
	}

	model := strings.TrimSpace(parsed.Model)
	provider := strings.TrimSpace(parsed.Provider)

	// Validate max-turns: negative or zero is rejected.
	if parsed.MaxTurns < 0 {
		return nil, fmt.Errorf("max-turns must be >= 1, got %d", parsed.MaxTurns)
	}

	// Validate timeout: must be a parseable duration string.
	var timeout time.Duration
	if raw := strings.TrimSpace(parsed.Timeout); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("timeout %q: %w", raw, err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("timeout must be positive, got %q", raw)
		}
		timeout = d
	}

	return &Definition{
		Name:                   name,
		Description:            description,
		Tools:                  parsed.Tools,
		Model:                  model,
		Provider:               provider,
		MaxTurns:               parsed.MaxTurns,
		Timeout:                timeout,
		DisableModelInvocation: parsed.DisableModelInvocation,
		UserInvocable:          userInvocable,
		ModeSwitcher:           modeSwitcher,
		ArgumentHint:           strings.TrimSpace(parsed.ArgumentHint),
		DisplayName:            displayName,
		Color:                  strings.TrimSpace(parsed.Color),
		Metadata:               parsed.Metadata,
		Body:                   strings.TrimSpace(body),
	}, nil
}

// Builtins parses and returns every embedded built-in agent definition, in
// deterministic order.
func Builtins() ([]*Definition, error) {
	defs := make([]*Definition, 0, len(builtinFiles))
	for _, name := range builtinFiles {
		content, err := templateFS.ReadFile("templates/" + name)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}
		def, err := Parse(content)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", name, err)
		}
		defs = append(defs, def)
	}
	return defs, nil
}

// Lookup returns the named built-in agent definition, or false if it doesn't
// exist.
func Lookup(name string) (*Definition, bool) {
	defs, err := Builtins()
	if err != nil {
		return nil, false
	}
	for _, def := range defs {
		if def.Name == name {
			return def, true
		}
	}
	return nil, false
}

// userAgentPrefix and projectAgentPrefix mirror internal/registry's
// userCommandPrefix/projectCommandPrefix. Duplicated rather than imported to
// avoid a dependency from this leaf package on the registry, matching the
// existing precedent of skills/commands.go and registry/sources.go each
// keeping their own copies of the same two prefixes.
const (
	userAgentPrefix    = "user:"
	projectAgentPrefix = "project:"
)

// CommandPrefix returns the "user:" or "project:" prefix a
// filesystem-discovered definition's slash command carries, matching
// internal/registry's precedence scheme (agentCommands). Empty for embedded
// built-ins, which are never prefixed.
func (d *Definition) CommandPrefix() string {
	switch d.Scope {
	case skills.ScopeProject:
		return projectAgentPrefix
	case skills.ScopeUser:
		return userAgentPrefix
	default:
		return ""
	}
}

// Resolve looks up an agent definition by the name it was invoked with. A
// bare name (no prefix) only matches a built-in - filesystem-discovered
// definitions are never reachable by their bare name, exactly mirroring how
// internal/registry lists them (prefixed, and skipped entirely when the
// bare name collides with a built-in). A "user:" or "project:" prefixed
// name resolves against the corresponding filesystem source instead.
func Resolve(name, workingDir string) (*Definition, bool) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, false
	}

	if rest, ok := strings.CutPrefix(trimmed, userAgentPrefix); ok {
		return lookupDiscovered(rest, UserSources())
	}
	if rest, ok := strings.CutPrefix(trimmed, projectAgentPrefix); ok {
		return lookupDiscovered(rest, ProjectSources(workingDir))
	}

	return Lookup(trimmed)
}

func lookupDiscovered(name string, sources []Source) (*Definition, bool) {
	defs, _ := DiscoverFromDisk(sources)
	for _, def := range defs {
		if strings.EqualFold(def.Name, name) {
			return def, true
		}
	}
	return nil, false
}
