package tui2

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// --- completion types ------------------------------------------------------

// compMatch is a single completion candidate.
type compMatch struct {
	Word        string
	Description string
}

// compGroup is a named group of matches (e.g. "Commands", "Models").
type compGroup struct {
	Title   string
	Matches []compMatch
}

// --- completion entry point ------------------------------------------------

// computeCompletions returns completion groups for the current input, or nil
// if completions are not applicable (not a slash command, etc.).
func (m *model) computeCompletions() []compGroup {
	text := m.input
	if !strings.HasPrefix(text, "/") {
		return nil
	}

	before := text // input, not cursor-relative (simplified — full line)
	fields := strings.Fields(before)
	endsWithSpace := strings.HasSuffix(before, " ")

	// Still typing the command name.
	if len(fields) <= 1 && !endsWithSpace {
		return m.commandGroups(strings.ToLower(strings.TrimPrefix(fields[0], "/")))
	}

	// Completing an argument.
	argsBefore := len(fields) - 1
	if !endsWithSpace {
		argsBefore--
	}
	name := strings.TrimPrefix(strings.ToLower(fields[0]), "/")

	// Extension subcommand completion.
	if ext, ok := m.extensionCommands[name]; ok && len(ext.Subcommands) > 0 && argsBefore == 0 {
		return m.extensionSubcommandGroups(ext)
	}

	// Built-in command argument completers.
	switch name {
	case "model":
		return m.modelCompletions()
	case "session", "resume":
		return m.sessionCompletions(argsBefore)
	case "effort":
		return m.effortCompletions()
	case "login", "logout":
		return m.providerCompletions()
	}

	return nil
}

// --- command name completions ----------------------------------------------

func (m *model) commandGroups(token string) []compGroup {
	seen := make(map[string]struct{})
	add := func(dst *[]compMatch, mc compMatch) {
		key := strings.ToLower(mc.Word)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		*dst = append(*dst, mc)
	}

	var commands, agents []compMatch
	for i := range slashTable {
		e := &slashTable[i]

		desc := e.description
		if len(e.aliases) > 0 {
			aliasStrs := make([]string, len(e.aliases))
			for j, a := range e.aliases {
				aliasStrs[j] = "/" + a
			}
			desc += " [alias: " + strings.Join(aliasStrs, ", ") + "]"
		}

		mc := compMatch{Word: "/" + e.name, Description: desc}
		if e.isAgent {
			add(&agents, mc)
		} else {
			add(&commands, mc)
		}

		if token != "" && len(e.aliases) > 0 {
			for _, alias := range e.aliases {
				if strings.HasPrefix(strings.ToLower(alias), token) {
					am := compMatch{Word: "/" + alias, Description: desc}
					if e.isAgent {
						add(&agents, am)
					} else {
						add(&commands, am)
					}
				}
			}
		}
	}

	var extensions []compMatch
	for _, ext := range m.extensionCommands {
		add(&extensions, compMatch{Word: "/" + ext.Name, Description: ext.Description})
	}

	var groups []compGroup
	if len(commands) > 0 {
		groups = append(groups, compGroup{Title: "Commands", Matches: commands})
	}
	if len(agents) > 0 {
		groups = append(groups, compGroup{Title: "Agents", Matches: agents})
	}
	if len(extensions) > 0 {
		groups = append(groups, compGroup{Title: "Extensions", Matches: extensions})
	}
	return groups
}

func (m *model) extensionSubcommandGroups(ext tauchat.ExtensionCommand) []compGroup {
	subs := slices.Clone(ext.Subcommands)
	slices.SortFunc(subs, func(a, b tauchat.ExtensionCommand) int {
		return strings.Compare(a.Name, b.Name)
	})
	matches := make([]compMatch, 0, len(subs))
	for _, sub := range subs {
		desc := sub.Description
		if sub.ArgsHint != "" {
			desc = strings.TrimSpace(sub.ArgsHint + "  " + desc)
		}
		matches = append(matches, compMatch{Word: sub.Name, Description: desc})
	}
	return []compGroup{{Title: "/" + ext.Name, Matches: matches}}
}

func (m *model) modelCompletions() []compGroup {
	models := slices.Clone(m.availableModels)
	slices.SortFunc(models, func(a, b tauchat.ChatModelRef) int {
		return strings.Compare(a.ID, b.ID)
	})
	matches := make([]compMatch, 0, len(models))
	for _, ref := range models {
		desc := ref.Provider
		if desc == "" {
			desc = ref.URL
		}
		matches = append(matches, compMatch{Word: ref.ID, Description: desc})
	}
	return []compGroup{{Title: "Models", Matches: matches}}
}

func (m *model) sessionCompletions(argsBefore int) []compGroup {
	if argsBefore > 0 {
		return nil // only first arg
	}
	summaries := slices.Clone(m.sessionSummaries)
	now := time.Now()
	matches := make([]compMatch, 0, len(summaries))
	for _, s := range summaries {
		age := now.Sub(s.UpdatedAt).Truncate(time.Second)
		parts := []string{s.ModelID}
		if s.MessageCount > 0 {
			parts = append(parts, fmt.Sprintf("%d msgs", s.MessageCount))
		}
		if age > 0 {
			parts = append(parts, age.String())
		}
		matches = append(matches, compMatch{Word: s.ID, Description: strings.Join(parts, " · ")})
	}
	return []compGroup{{Title: "Sessions", Matches: matches}}
}

func (m *model) effortCompletions() []compGroup {
	ref := m.currentModelRef()
	levels := effortLevels(ref)
	matches := make([]compMatch, 0, len(levels))
	for _, l := range levels {
		matches = append(matches, compMatch{Word: l})
	}
	return []compGroup{{Title: "Effort", Matches: matches}}
}

func (m *model) providerCompletions() []compGroup {
	return nil // TODO: wire provider list from config
}

// --- fuzzy filter ----------------------------------------------------------

// filterCompletions filters matches by a token prefix (case-insensitive).
// Returns matches whose Word starts with the token.
func filterCompletions(matches []compMatch, token string) []compMatch {
	if token == "" {
		return matches
	}
	token = strings.ToLower(token)
	var out []compMatch
	for _, m := range matches {
		if strings.HasPrefix(strings.ToLower(m.Word), token) {
			out = append(out, m)
		}
	}
	return out
}

// longestCommonPrefix returns the longest prefix shared by all words.
func longestCommonPrefix(words []string) string {
	if len(words) == 0 {
		return ""
	}
	prefix := words[0]
	for _, w := range words[1:] {
		for !strings.HasPrefix(w, prefix) && len(prefix) > 0 {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

// --- tab completion logic --------------------------------------------------

// applyTabCompletion applies tab completion to the current input. Returns
// the new input value. Handles LCP extension on first tab, cycling on
// subsequent tabs within the same completion session.
func (m *model) applyTabCompletion() {
	groups := m.computeCompletions()
	if len(groups) == 0 {
		return
	}

	// Flatten all matches.
	var all []compMatch
	for _, g := range groups {
		all = append(all, g.Matches...)
	}
	if len(all) == 0 {
		return
	}

	// Determine the current token being completed.
	fields := strings.Fields(m.input)
	if len(fields) == 0 {
		return
	}
	token := ""
	endsWithSpace := strings.HasSuffix(m.input, " ")
	if !endsWithSpace {
		token = fields[len(fields)-1]
	}

	filtered := filterCompletions(all, token)
	if len(filtered) == 0 {
		return
	}

	// LCP extension: if only one match or first tab, extend to longest
	// common prefix.
	if len(filtered) == 1 {
		// Replace current token with full match.
		m.input = replaceLastToken(m.input, filtered[0].Word)
		return
	}

	lcp := longestCommonPrefix(collectWords(filtered))
	if len(lcp) > len(token) {
		m.input = replaceLastToken(m.input, lcp)
		return
	}

	// Cycle through matches if LCP can't extend further.
	// Store completion state for cycling.
	words := collectWords(filtered)
	if m.compIdx >= len(words) || !slices.Equal(m.compWords, words) {
		m.compWords = words
		m.compIdx = 0
	} else {
		m.compIdx = (m.compIdx + 1) % len(words)
	}
	m.input = replaceLastToken(m.input, words[m.compIdx])
}

func collectWords(matches []compMatch) []string {
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m.Word
	}
	return out
}

func replaceLastToken(input, replacement string) string {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return replacement
	}
	// Replace the last token.
	fields[len(fields)-1] = replacement
	result := strings.Join(fields, " ")
	if strings.HasSuffix(input, " ") {
		result += " "
	}
	return result
}
