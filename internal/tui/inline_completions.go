package tui

import (
	"slices"
	"strings"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/pkg/taui"
)

// completionSet is the dynamic completion provider for the inline input. It
// emits taui.CompletionSet values; the Completions widget does the fuzzy
// filtering against the token under the cursor. Slash commands resolve their
// command name first, then argument candidates per command.
func (c *inlineChat) completionSet(ctx taui.CompletionContext) *taui.CompletionSet {
	full := []rune(ctx.Text)
	cur := min(ctx.Cursor, len(full))
	before := string(full[:cur])
	if !strings.HasPrefix(before, "/") {
		return nil
	}

	endsWithSpace := strings.HasSuffix(before, " ")
	fields := strings.Fields(before)

	// The token under the cursor (empty when a space was just typed, meaning a
	// fresh argument slot). replaceStart marks where a chosen completion is
	// spliced in — the start of that token, or the cursor for an empty slot.
	token := ""
	if !endsWithSpace && len(fields) > 0 {
		token = fields[len(fields)-1]
	}
	replaceStart := cur - len([]rune(token))

	var (
		title   string
		matches []taui.Match
	)

	// Still typing the command name itself (no argument slot yet).
	if len(fields) <= 1 && !endsWithSpace {
		title, matches = "Commands", c.commandMatches()
	} else {
		// argsBefore counts fully-typed argument tokens before the cursor,
		// excluding any token currently being edited. This is what stops a
		// completed argument (trailing space) from re-opening the dropdown.
		argsBefore := len(fields) - 1
		if !endsWithSpace {
			argsBefore--
		}
		name := strings.TrimPrefix(strings.ToLower(fields[0]), "/")
		cmd, ok := slashByName[name]
		if !ok || cmd.complete == nil {
			return nil
		}
		title, matches = cmd.complete(c, fields, argsBefore)
	}

	if len(matches) == 0 {
		return nil
	}
	return &taui.CompletionSet{
		ReplaceStart: replaceStart,
		ReplaceEnd:   cur,
		Groups:       []taui.MatchGroup{{Title: title, Matches: matches}},
	}
}

// commandMatches lists slash-command name completions: the built-in command
// table (the single source of truth) merged with registry and extension
// commands. Built-ins are always offered so completion works even when the
// registry snapshot is empty.
func (c *inlineChat) commandMatches() []taui.Match {
	seen := make(map[string]struct{})
	out := make([]taui.Match, 0, len(slashCommands))
	add := func(m taui.Match) {
		key := strings.ToLower(m.Word)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, m)
	}
	for i := range slashCommands {
		cmd := &slashCommands[i]
		add(taui.Match{Word: "/" + cmd.name, Description: cmd.description})
	}

	c.mu.Lock()
	registry := slices.Clone(c.registryCommands)
	debug := c.debug
	exts := make([]tauchat.ExtensionCommand, 0, len(c.extensionCommands))
	for _, ext := range c.extensionCommands {
		exts = append(exts, ext)
	}
	c.mu.Unlock()

	for _, ref := range registry {
		if ref.Name == "debug" && !debug {
			continue
		}
		word := ref.Label
		if word == "" {
			word = "/" + ref.Name
		}
		if !strings.HasPrefix(word, "/") {
			word = "/" + word
		}
		add(taui.Match{Word: word, Description: ref.Description})
	}

	slices.SortFunc(exts, func(a, b tauchat.ExtensionCommand) int { return strings.Compare(a.Name, b.Name) })
	for _, ext := range exts {
		add(taui.Match{Word: "/" + ext.Name, Description: ext.Description})
	}
	return out
}

func (c *inlineChat) modelMatches() []taui.Match {
	c.mu.Lock()
	models := slices.Clone(c.availableModels)
	c.mu.Unlock()
	slices.SortFunc(models, func(a, b tauchat.ChatModelRef) int { return strings.Compare(a.ID, b.ID) })
	out := make([]taui.Match, 0, len(models))
	for _, m := range models {
		out = append(out, taui.Match{Word: m.ID, Description: m.URL})
	}
	return out
}

func (c *inlineChat) sessionMatches() []taui.Match {
	c.mu.Lock()
	summaries := slices.Clone(c.sessionSummaries)
	c.mu.Unlock()
	out := make([]taui.Match, 0, len(summaries))
	for _, s := range summaries {
		out = append(out, taui.Match{Word: s.ID, Description: s.ModelID})
	}
	return out
}

// sessionArgs resolves completions for /session: subcommands for the first
// argument, then session IDs for info/export/delete on the second. A completed
// second argument (argsBefore >= 2) offers nothing so the line can be submitted.
func (c *inlineChat) sessionArgs(fields []string, argsBefore int) (string, []taui.Match) {
	if argsBefore == 0 {
		return "Session", sessionSubMatches
	}
	if argsBefore == 1 {
		sub := ""
		if len(fields) >= 2 {
			sub = strings.ToLower(fields[1])
		}
		if sub == "info" || sub == "export" || sub == "delete" {
			return "Sessions", c.sessionMatches()
		}
	}
	return "", nil
}

var reasoningMatches = []taui.Match{
	{Word: "on", Description: "show reasoning before responses"},
	{Word: "off", Description: "hide reasoning"},
	{Word: "toggle", Description: "toggle reasoning visibility"},
}

var effortMatches = []taui.Match{
	{Word: "off", Description: "no reasoning effort"},
	{Word: "low", Description: "low effort (~512 tokens)"},
	{Word: "medium", Description: "medium effort (~2048 tokens)"},
	{Word: "high", Description: "high effort (~8192 tokens)"},
	{Word: "max", Description: "maximum effort (unlimited)"},
}

var sessionSubMatches = []taui.Match{
	{Word: "list", Description: "show saved sessions"},
	{Word: "info", Description: "show session details"},
	{Word: "export", Description: "export a session"},
	{Word: "delete", Description: "delete a session"},
}
