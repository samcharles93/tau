package registry

import (
	cc "github.com/samcharles93/tau/internal/chat/commands"
	"github.com/samcharles93/tau/internal/skills"
)

// builtinCommands returns the set of built-in TUI slash commands.
// These were previously hard-coded in internal/tui/completions.go.
func builtinCommands() []Command {
	cmds := []Command{
		{Name: "new", Label: "/new", Description: "start a new conversation"},
		{Name: "system", Label: "/system", Description: "set system prompt", AcceptsArgs: true},
		{Name: "model", Label: "/model", Description: "switch model", AcceptsArgs: true},
		{Name: "refresh", Label: "/refresh", Description: "refresh models"},
		{Name: "reload", Label: "/reload", Description: "reload extensions while idle"},
		{Name: "reasoning", Label: "/reasoning", Description: "show or hide reasoning", AcceptsArgs: true},
		{Name: "settings", Label: "/settings", Description: "show settings help"},
		{Name: "session", Label: "/session", Description: "manage saved sessions", AcceptsArgs: true},
		{Name: "resume", Label: "/resume", Description: "resume a saved session"},
		{Name: "debug", Label: "/debug", Description: "preview TUI components (developer)", AcceptsArgs: true},
		{Name: "exit", Label: "/exit", Description: "quit"},
		// Aliases.
		{Name: "quit", Label: "/quit", Description: "quit"},
		{Name: "q", Label: "/q", Description: "quit"},
		{Name: "help", Label: "/help", Description: "toggle help"},
		{Name: "?", Label: "/?", Description: "toggle help"},
		{Name: "clear", Label: "/clear", Description: "start a new conversation"},
		{Name: "reset", Label: "/reset", Description: "start a new conversation"},
		{Name: "models", Label: "/models", Description: "refresh models"},
	}
	return cmds
}

// mergeCustomCommands loads custom commands from ~/.tau/commands/ and
// <project>/.tau/commands/ and adds them to the registry.
func (r *Registry) mergeCustomCommands() {
	customCmds, err := cc.LoadCustomCommands(r.cwd)
	if err != nil {
		return
	}
	for _, c := range customCmds {
		if c.Name == "" {
			continue
		}
		if _, exists := r.commands[c.Name]; exists {
			continue // built-in takes precedence
		}
		desc := "custom command"
		if c.Skill != nil && c.Skill.Description != "" {
			desc = c.Skill.Description
		}
		r.commands[c.Name] = Command{
			Name:        c.Name,
			Label:       "/" + c.Name,
			Description: desc,
			AcceptsArgs: len(c.Arguments) > 0,
		}
	}
}

const (
	userCommandPrefix    = "user:"
	projectCommandPrefix = "project:"
)

// skillCommandName returns the full registry name for a skill.
func skillCommandName(s *skills.Skill) string {
	switch s.Scope {
	case skills.ScopeProject:
		return projectCommandPrefix + s.Name
	default:
		return userCommandPrefix + s.Name
	}
}

// skillCommandLabel returns the display label for a skill command.
// Uses a "/skill:" prefix so that skill commands are grouped under
// the skill namespace in completions (e.g. "/skill:pdf").
func skillCommandLabel(s *skills.Skill) string {
	return "/skill:" + s.Name
}
