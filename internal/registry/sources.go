package registry

import (
	agentspec "github.com/samcharles93/tau/internal/agent/spec"
	cc "github.com/samcharles93/tau/internal/chat/commands"
	"github.com/samcharles93/tau/internal/skills"
)

// builtinCommands returns the set of built-in slash commands shared across
// TUI and web UI. These are the single source of truth for command registration;
// the TUI's inline_commands.go hardcodes the same set for dispatch but the
// registry feed is what drives the web UI completion menu and /help output.
func builtinCommands() []Command {
	cmds := []Command{
		{Name: "model", Label: "/model", Description: "switch model", AcceptsArgs: true},
		{Name: "system", Label: "/system", Description: "set the system prompt", AcceptsArgs: true},
		{Name: "effort", Label: "/effort", Description: "set reasoning effort", AcceptsArgs: true},
		{Name: "session", Label: "/session", Description: "manage saved sessions (list, info, export, delete, load)", AcceptsArgs: true},
		{Name: "resume", Label: "/resume", Description: "resume a saved session", AcceptsArgs: true},
		{Name: "refresh", Label: "/refresh", Description: "re-discover available models", AcceptsArgs: false},
		{Name: "reload", Label: "/reload", Description: "reload extensions", AcceptsArgs: false},
		{Name: "clear", Label: "/clear", Description: "start a fresh session [alias: /new, /reset]", AcceptsArgs: false},
		{Name: "help", Label: "/help", Description: "show available commands", AcceptsArgs: false},
	}
	cmds = append(cmds, agentCommands()...)
	return cmds
}

// agentCommands adapts tau's built-in agent definitions (internal/agent/spec)
// into registry Commands, skipping any marked user-invocable: false.
func agentCommands() []Command {
	defs, err := agentspec.Builtins()
	if err != nil {
		return nil
	}
	cmds := make([]Command, 0, len(defs))
	for _, def := range defs {
		if !def.UserInvocable {
			continue
		}
		cmds = append(cmds, Command{
			Name:        def.Name,
			Label:       "/" + def.Name,
			Description: def.Description,
			AcceptsArgs: true,
		})
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
