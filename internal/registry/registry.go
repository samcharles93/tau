// Package registry discovers and publishes the set of available commands
// (built-in, custom, skill-based, and extension) so that consumers such as
// the TUI can render completions without hard-coding command lists.
//
// Extension commands from plugins arrive on the event bus separately via
// [chat.ExtensionCommandsChangedEvent] and are not duplicated here.
package registry

import (
	"slices"
	"sync"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/skills"
)

// Command is a self-describing command registered in the registry.
type Command struct {
	Name        string // unique key, e.g. "model", "session:list"
	Label       string // display label, e.g. "/model"
	Description string // one-line help text
	AcceptsArgs bool   // true if the command takes arguments
}

// Registry collects commands from all sources and publishes the merged
// set on the event bus so that consumers can stay up to date.
type Registry struct {
	mu        sync.RWMutex
	commands  map[string]Command
	pub       *eventbus.Publisher[tauchat.CommandsChangedEvent]
	busClient *eventbus.Client
	cwd       string
}

// New creates a new Registry attached to the given bus client.
// Call [Registry.Discover] to perform the initial scan and publish.
func New(cwd string, client *eventbus.Client) *Registry {
	return &Registry{
		commands:  make(map[string]Command),
		pub:       eventbus.Publish[tauchat.CommandsChangedEvent](client),
		busClient: client,
		cwd:       cwd,
	}
}

// Close closes the registry's bus client. After Close the registry must
// not be used.
func (r *Registry) Close() {
	if r.busClient != nil {
		r.busClient.Close()
	}
}

// All returns a sorted snapshot of every registered command.
func (r *Registry) All() []Command {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Command, 0, len(r.commands))
	for _, c := range r.commands {
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b Command) int {
		if a.Label < b.Label {
			return -1
		}
		return 1
	})
	return out
}

// Lookup returns the command with the given name, if registered.
func (r *Registry) Lookup(name string) (Command, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.commands[name]
	return c, ok
}

// Discover scans all non-skill command sources, merges them, and publishes
// a [chat.CommandsChangedEvent] on the bus. Skill commands are provided
// separately via [Registry.MergeSkills] so the caller can reuse a single
// [skills.Discover] result.
//
// Discover is safe to call multiple times (e.g. after an extension reload).
func (r *Registry) Discover() {
	r.mu.Lock()
	r.commands = make(map[string]Command)
	for _, cmd := range builtinCommands() {
		r.commands[cmd.Name] = cmd
	}
	r.mergeCustomCommands()
	r.mu.Unlock()

	r.publish()
}

// MergeSkills adds user-invocable skills as commands. Callers should
// discover skills once with [skills.Discover] and pass the result to
// both the registry and the prompt builder.
func (r *Registry) MergeSkills(all []*skills.Skill) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, s := range all {
		if s == nil || !s.UserInvocable {
			continue
		}
		label := skillCommandLabel(s)
		name := skillCommandName(s)
		if _, exists := r.commands[name]; exists {
			continue // built-in or custom takes precedence
		}
		r.commands[name] = Command{
			Name:        name,
			Label:       label,
			Description: s.Description,
			AcceptsArgs: false,
		}
	}

	r.publishLocked()
}

// publish publishes the current command set on the bus. Does not hold
// the lock — callers should release r.mu before calling.
func (r *Registry) publish() {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.publishLocked()
}

// publishLocked sends a CommandsChangedEvent on the bus. Must be called
// while holding at least r.mu.RLock.
func (r *Registry) publishLocked() {
	if r.pub == nil {
		return
	}
	refs := make([]tauchat.CommandRef, 0, len(r.commands))
	for _, c := range r.commands {
		refs = append(refs, tauchat.CommandRef{
			Name:        c.Name,
			Label:       c.Label,
			Description: c.Description,
			AcceptsArgs: c.AcceptsArgs,
		})
	}
	slices.SortFunc(refs, func(a, b tauchat.CommandRef) int {
		if a.Label < b.Label {
			return -1
		}
		return 1
	})
	r.pub.Publish(tauchat.CommandsChangedEvent{
		Commands: refs,
	})
}
