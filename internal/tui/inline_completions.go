package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

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
	//
	// IMPORTANT: replaceEnd covers the full word under the cursor, not just the
	// characters before it. When the cursor is mid-word (e.g. /mo|del),
	// using cur as replaceEnd would leave the trailing characters outside the
	// replace range, causing duplication when LCP extension or full replace
	// reconstructs the text.
	token := ""
	if !endsWithSpace && len(fields) > 0 {
		token = fields[len(fields)-1]
	}
	replaceStart := cur - len([]rune(token))

	// Compute the end of the word under/after the cursor so that when
	// ReplaceEnd covers the entire token, not just the part before the cursor.
	endOfWord := cur
	for endOfWord < len(full) && full[endOfWord] != ' ' {
		endOfWord++
	}

	// Still typing the command name itself (no argument slot yet): offer the
	// full command list split into groups by origin (core / agents /
	// extensions) rather than one flat "Commands" list.
	if len(fields) <= 1 && !endsWithSpace {
		groups := c.commandGroups(token)
		if len(groups) == 0 {
			return nil
		}
		return &taui.CompletionSet{
			ReplaceStart: replaceStart,
			ReplaceEnd:   endOfWord,
			Groups:       groups,
		}
	}

	// argsBefore counts fully-typed argument tokens before the cursor,
	// excluding any token currently being edited. This is what stops a
	// completed argument (trailing space) from re-opening the dropdown.
	argsBefore := len(fields) - 1
	if !endsWithSpace {
		argsBefore--
	}
	name := strings.TrimPrefix(strings.ToLower(fields[0]), "/")
	var title string
	var matches []taui.Match
	if cmd, ok := slashByName[name]; ok && cmd.complete != nil {
		title, matches = cmd.complete(c, fields, argsBefore)
	} else if argsBefore == 0 {
		// Completing the first argument slot of an extension command group:
		// offer its sub-actions (e.g. /mcp <list|reconnect|reload>).
		title, matches = c.extensionSubcommandMatches(name)
	}

	if len(matches) == 0 {
		return nil
	}
	return &taui.CompletionSet{
		ReplaceStart: replaceStart,
		ReplaceEnd:   endOfWord,
		Groups:       []taui.MatchGroup{{Title: title, Matches: matches}},
	}
}

// commandGroups lists slash-command name completions split into groups by
// origin — core built-ins, built-in agents (/plan, /research, ...), and
// extension commands — so the dropdown reads as categories instead of one
// undifferentiated list. Registry commands mirror the core built-ins (they
// exist for the web UI) and fold into the "Commands" group, deduped against
// it. Built-ins are always offered so completion works even when the
// registry snapshot is empty.
//
// token is the partial command being typed (e.g. "/", "/ne", "/clear").
// When it's just "/" (empty after stripping) alias entries are suppressed so
// they don't clutter the browsing view. When the user has typed enough to
// match an alias prefix (e.g. "/ne" for "new"), the alias is surfaced with
// that alias as the completion word.
func (c *inlineChat) commandGroups(token string) []taui.MatchGroup {
	cmdToken := strings.ToLower(strings.TrimPrefix(token, "/"))

	seen := make(map[string]struct{})
	add := func(dst *[]taui.Match, m taui.Match) {
		key := strings.ToLower(m.Word)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		*dst = append(*dst, m)
	}

	var commands, agents []taui.Match
	for i := range slashCommands {
		cmd := &slashCommands[i]

		desc := cmd.description
		if len(cmd.aliases) > 0 {
			aliasStrs := make([]string, len(cmd.aliases))
			for j, a := range cmd.aliases {
				aliasStrs[j] = "/" + a
			}
			desc += " [alias: " + strings.Join(aliasStrs, ", ") + "]"
		}

		m := taui.Match{Word: "/" + cmd.name, Description: desc}
		if cmd.isAgent {
			add(&agents, m)
		} else {
			add(&commands, m)
		}

		// Surface aliases as matches only when the user has typed enough
		// to match an alias prefix. Prevents aliases from cluttering the
		// dropdown when browsing the full command list.
		if cmdToken != "" && len(cmd.aliases) > 0 {
			for _, alias := range cmd.aliases {
				if strings.HasPrefix(strings.ToLower(alias), cmdToken) {
					aliasMatch := taui.Match{Word: "/" + alias, Description: desc}
					if cmd.isAgent {
						add(&agents, aliasMatch)
					} else {
						add(&commands, aliasMatch)
					}
				}
			}
		}
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
		add(&commands, taui.Match{Word: word, Description: ref.Description})
	}

	slices.SortFunc(exts, func(a, b tauchat.ExtensionCommand) int { return strings.Compare(a.Name, b.Name) })
	var extensions []taui.Match
	for _, ext := range exts {
		add(&extensions, taui.Match{Word: "/" + ext.Name, Description: ext.Description})
	}

	var groups []taui.MatchGroup
	if len(commands) > 0 {
		groups = append(groups, taui.MatchGroup{Title: "Commands", Matches: commands})
	}
	if len(agents) > 0 {
		groups = append(groups, taui.MatchGroup{Title: "Agents", Matches: agents})
	}
	if len(extensions) > 0 {
		groups = append(groups, taui.MatchGroup{Title: "Extensions", Matches: extensions})
	}
	return groups
}

// extensionSubcommandMatches lists the sub-actions of an extension command
// group (e.g. `list`, `reconnect`, `reload` for `/mcp`), used when completing
// the first argument slot after a group name. Returns no matches when the named
// command has no sub-actions.
func (c *inlineChat) extensionSubcommandMatches(name string) (string, []taui.Match) {
	c.mu.Lock()
	ext, ok := c.extensionCommands[name]
	c.mu.Unlock()
	if !ok || len(ext.Subcommands) == 0 {
		return "", nil
	}
	subs := slices.Clone(ext.Subcommands)
	slices.SortFunc(subs, func(a, b tauchat.ExtensionCommand) int { return strings.Compare(a.Name, b.Name) })
	matches := make([]taui.Match, 0, len(subs))
	for _, sub := range subs {
		desc := sub.Description
		if sub.ArgsHint != "" {
			desc = strings.TrimSpace(sub.ArgsHint + "  " + desc)
		}
		matches = append(matches, taui.Match{Word: sub.Name, Description: desc})
	}
	return "/" + ext.Name, matches
}

func (c *inlineChat) modelMatches() []taui.Match {
	c.mu.Lock()
	models := slices.Clone(c.availableModels)
	c.mu.Unlock()
	slices.SortFunc(models, func(a, b tauchat.ChatModelRef) int { return strings.Compare(a.ID, b.ID) })
	out := make([]taui.Match, 0, len(models))
	for _, m := range models {
		// Show the provider the model belongs to (dimmed, in its own column)
		// rather than the raw base URL.
		desc := m.Provider
		if desc == "" {
			desc = m.URL
		}
		out = append(out, taui.Match{Word: m.ID, Description: desc})
	}
	return out
}

func (c *inlineChat) sessionMatches() []taui.Match {
	c.mu.Lock()
	summaries := slices.Clone(c.sessionSummaries)
	c.mu.Unlock()
	now := time.Now()
	out := make([]taui.Match, 0, len(summaries))
	for _, s := range summaries {
		parts := []string{s.ModelID}
		if s.MessageCount > 0 {
			parts = append(parts, fmt.Sprintf("%d msgs", s.MessageCount))
		}
		if !s.UpdatedAt.IsZero() {
			parts = append(parts, relativeTime(now, s.UpdatedAt))
		}
		out = append(out, taui.Match{Word: s.ID, Description: strings.Join(parts, " · ")})
	}
	return out
}

// relativeTime returns a human-friendly relative timestamp like "2h ago"
// or "3d ago" based on the time elapsed since t.
func relativeTime(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", h)
	case d < 30*24*time.Hour:
		dy := int(d.Hours() / 24)
		if dy == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", dy)
	default:
		return t.Format("Jan 2")
	}
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

// argEffort builds completion matches for the /effort command from the
// current model's advertised reasoning effort levels.
func argEffort(c *inlineChat, _ []string, argsBefore int) (string, []taui.Match) {
	if argsBefore > 0 {
		return "", nil
	}
	model := c.currentModelRef()
	levels := model.Config.ReasoningEfforts
	matches := make([]taui.Match, 0, len(levels)+1)
	for _, l := range levels {
		matches = append(matches, taui.Match{Word: l, Description: "effort: " + l})
	}
	matches = append(matches, taui.Match{Word: "auto", Description: "provider default"})
	return "Effort", matches
}

var sessionSubMatches = []taui.Match{
	{Word: "list", Description: "show saved sessions"},
	{Word: "info", Description: "show session details"},
	{Word: "export", Description: "export a session"},
	{Word: "delete", Description: "delete a session"},
}
