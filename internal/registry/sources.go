package registry

import (
	cc "github.com/samcharles93/tau/internal/chat/commands"
	"github.com/samcharles93/tau/internal/skills"
)

// builtinCommands returns the set of built-in TUI slash commands.
// These were previously hard-coded in internal/tui/completions.go.

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
