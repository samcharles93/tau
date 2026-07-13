package tui2

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/providers"
)

// --- completion types ------------------------------------------------------

// compMatch is a single completion candidate.
type compMatch struct {
	Word        string
	Description string
	// RequiresArg is true for slash commands whose usage is a required
	// argument (e.g. "<id>", "<prompt>") — accepting one of these must not
	// auto-submit, since the command is invalid without more typing. Left
	// false for optional-argument/no-argument commands and for every
	// argument-value completion (a model ID, session ID, etc.), which are
	// leaf values that already make the command valid to run.
	RequiresArg bool
}

// compGroup is a named group of matches (e.g. "Commands", "Models").
type compGroup struct {
	Title   string
	Matches []compMatch
}

// compRow is one fuzzy-scored, highlight-annotated completion candidate,
// flattened out of compGroups and ranked — this is what's actually shown and
// navigated in the dropdown. Mirrors pkg/taui/completions.go's filteredRow so
// the two frontends' dropdowns behave identically (same live narrowing, same
// scoring, same bold match highlighting) rather than tui2 reinventing a
// weaker interaction model.
type compRow struct {
	compMatch
	group     string
	groupPrio int // sort order: 0=Commands, 1=Agents, 2=Extensions, 3+=custom
	score     float64
	highlight [][2]int // rune spans within Word to bold
}

// groupPriority maps group titles to sort precedence (lower = first).
var groupPriority = map[string]int{
	"Commands":   0,
	"Agents":     1,
	"Extensions": 2,
}

// --- completion entry point ------------------------------------------------

// rawCandidateGroups returns the unfiltered candidate pool for the current
// input (which command/argument kind applies), or nil if completions don't
// apply at all (not a slash command, etc). Filtering/ranking against what's
// actually been typed happens uniformly afterwards in completionRows.
func (m *model) rawCandidateGroups() []compGroup {
	text := m.input
	if !strings.HasPrefix(text, "/") {
		return nil
	}

	fields := strings.Fields(text)
	endsWithSpace := strings.HasSuffix(text, " ")

	// Still typing the command name.
	if len(fields) <= 1 && !endsWithSpace {
		return m.commandGroups(m.completionToken())
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

	// Built-in command argument completers. Each takes exactly one argument,
	// so completions only apply at argsBefore == 0 (the argument itself) —
	// without this gate, accepting a value leaves a trailing space, argsBefore
	// becomes 1, and the SAME completer fires again for what is now a
	// nonexistent second argument slot. A repeated Tab there would then just
	// re-accept the top-ranked candidate onto the end of the line again and
	// again (e.g. "/model gpt-4 gpt-4 gpt-4 ...") instead of the dropdown
	// correctly having nothing left to offer.
	switch name {
	case "model":
		return m.modelCompletions(argsBefore)
	case "session", "resume":
		return m.sessionCompletions(argsBefore)
	case "effort":
		return m.effortCompletions(argsBefore)
	case "provider":
		return m.providerCompletions(fields, argsBefore)
	}

	return nil
}

// hasArgCompleter names the commands handled by the switch above — i.e.
// every command whose (possibly optional) argument has its own completer.
// Used by commandGroups to mark these RequiresArg even when their usage
// string uses "[...]" (optional) rather than "<...>" (required), so
// accepting the bare command name from the dropdown never auto-submits
// ahead of that argument completer.
var hasArgCompleter = map[string]bool{
	"model":    true,
	"session":  true,
	"resume":   true,
	"effort":   true,
	"provider": true,
}

// completionToken returns the partial word currently being completed (the
// last field of m.input), or "" when the cursor is right after a trailing
// space (nothing typed yet for this token).
func (m *model) completionToken() string {
	if strings.HasSuffix(m.input, " ") || m.input == "" {
		return ""
	}
	fields := strings.Fields(m.input)
	if len(fields) == 0 {
		return ""
	}
	token := fields[len(fields)-1]
	if token == "/" {
		// A bare "/" with nothing typed after it is the command-prefix
		// marker, not a real query — treat it as no token at all. Otherwise
		// every candidate "matches" it trivially (every Word contains "/")
		// and ties at an identical fuzzy score, which forces the sort
		// below to fall back to its alphabetical tie-break — exactly the
		// bug that fragmented the just-opened dropdown into repeating
		// "Commands"/"Agents"/"Commands" headers instead of one contiguous
		// group per category.
		return ""
	}
	return token
}

// completionRows returns the fuzzy-filtered, score-ranked completion rows for
// the current input, and the token being completed. Returns (nil, "") when
// completions don't apply (not a slash command, a response in flight, etc) —
// this is the single source of truth both View() (rendering) and
// handleCompletionKey (navigation/accept) use, so what's shown is always
// exactly what's selectable.
func (m *model) completionRows() ([]compRow, string) {
	if m.inResponse || m.bashRunning {
		return nil, ""
	}
	groups := m.rawCandidateGroups()
	if len(groups) == 0 {
		return nil, ""
	}
	token := m.completionToken()

	var rows []compRow
	for _, g := range groups {
		for _, mc := range g.Matches {
			res := FuzzyMatch(token, mc.Word)
			if token != "" && !res.Match {
				continue
			}
			rows = append(rows, compRow{
				compMatch: mc,
				group:     g.Title,
				groupPrio: groupPriority[g.Title],
				score:     res.Score,
				highlight: fuzzyHighlightSpans(res.Positions, mc.Word),
			})
		}
	}
	// Sort by group order first, then fuzzy score, then alphabetically.
	// Group-first matters whenever rows tie on score — e.g. with nothing
	// typed yet every row ties at 0 (FuzzyMatch's empty-query case) — since
	// sorting by score/word alone would interleave Commands/Agents/
	// Extensions into a fragmented, repeating sequence of one- or two-line
	// group headers instead of one contiguous block per category.
	slices.SortStableFunc(rows, func(a, b compRow) int {
		switch {
		case a.groupPrio < b.groupPrio:
			return -1
		case a.groupPrio > b.groupPrio:
			return 1
		case a.score < b.score:
			return -1
		case a.score > b.score:
			return 1
		default:
			return strings.Compare(a.Word, b.Word)
		}
	})
	return rows, token
}

// --- command name completions ----------------------------------------------

// commandGroups returns the full, unfiltered command/agent/extension
// candidate pool — completionRows applies fuzzy filtering against whatever
// has actually been typed, so this doesn't need to filter by token itself.
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
		// "<...>" usage means a required argument (e.g. "/system <prompt>");
		// "[...]", a bare example word (e.g. "/skills" usage "list"), or no
		// usage at all normally mean the command is already valid to run
		// as-is. The exception is a command whose optional argument has its
		// own completer (provider -> providers, model -> models, etc):
		// accepting the bare command name there must not auto-submit either,
		// or Enter short-circuits straight past the argument picker that Tab
		// would otherwise lead into (e.g. /provider's provider list).
		requiresArg := strings.HasPrefix(e.usage, "<") || hasArgCompleter[e.name]

		mc := compMatch{Word: "/" + e.name, Description: desc, RequiresArg: requiresArg}
		if e.isAgent {
			add(&agents, mc)
		} else {
			add(&commands, mc)
		}

		// token is the raw field including its leading "/" (e.g. "/quit"),
		// but aliases are bare ("quit") — strip it before comparing, or the
		// prefix check below never matches anything once a command name is
		// typed. Mirrors internal/tui/inline_completions.go:107,139-143.
		cmdToken := strings.TrimPrefix(strings.ToLower(token), "/")
		for _, alias := range e.aliases {
			// Surface aliases as matches only when the user has typed enough
			// to match an alias prefix, preventing aliases from cluttering the
			// dropdown when browsing the full command list (e.g. "/?" and
			// "/usage" should not appear until the user types "?" or "u").
			if cmdToken == "" || !strings.HasPrefix(strings.ToLower(alias), cmdToken) {
				continue
			}
			am := compMatch{Word: "/" + alias, Description: desc, RequiresArg: requiresArg}
			if e.isAgent {
				add(&agents, am)
			} else {
				add(&commands, am)
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

func (m *model) modelCompletions(argsBefore int) []compGroup {
	if argsBefore > 0 {
		return nil // /model takes exactly one argument
	}
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

// maybePrefetchSessions silently refreshes m.sessionSummaries the first
// time the /session or /resume argument completer needs it and finds it
// empty. Without this, the very first time a user tab-completes a session
// ID in a fresh TUI process the dropdown has nothing to show — the cache is
// otherwise only ever populated as a side effect of an explicit "/session"
// or "/resume" submission. Silent so the SessionsListedEvent handler
// doesn't also print a "Sessions: ..." dump to scrollback the way an
// explicit command does (see ListSessionsCommand.Silent).
func (m *model) maybePrefetchSessions() tea.Cmd {
	if m.sessionsFetchInFlight || len(m.sessionSummaries) > 0 {
		return nil
	}
	for _, g := range m.rawCandidateGroups() {
		if g.Title == "Sessions" && len(g.Matches) == 0 {
			m.sessionsFetchInFlight = true
			return sendCommand(m.runtime, tauchat.ListSessionsCommand{Silent: true})
		}
	}
	return nil
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

func (m *model) effortCompletions(argsBefore int) []compGroup {
	if argsBefore > 0 {
		return nil // /effort takes exactly one argument
	}
	ref := m.currentModelRef()
	levels := effortLevels(ref)
	matches := make([]compMatch, 0, len(levels))
	for _, l := range levels {
		matches = append(matches, compMatch{Word: l})
	}
	return []compGroup{{Title: "Effort", Matches: matches}}
}

// providerCompletions offers completions for /provider's argument(s):
// argsBefore==0 offers the catalog provider IDs directly (a bare
// "/provider <name>" toggles it) plus the "login"/"logout" sub-verbs;
// argsBefore==1, when the first word is "login" or "logout", offers the
// catalog provider IDs again as that sub-verb's argument. Mirrors
// internal/tui/inline_providers.go's argProvider.
func (m *model) providerCompletions(fields []string, argsBefore int) []compGroup {
	menu := providers.Menu(providerCfg(m.ctx), providerState(), nil)
	providerMatches := make([]compMatch, 0, len(menu))
	for _, e := range menu {
		desc := e.DisplayName
		if e.Enabled {
			desc += " (enabled)"
		}
		providerMatches = append(providerMatches, compMatch{Word: e.ID, Description: desc})
	}

	if argsBefore == 0 {
		matches := append([]compMatch{
			{Word: "login", Description: "OAuth sign-in for a provider"},
			{Word: "logout", Description: "disable a provider / remove its login"},
		}, providerMatches...)
		return []compGroup{{Title: "Providers", Matches: matches}}
	}
	if argsBefore == 1 && len(fields) >= 2 {
		switch strings.ToLower(fields[1]) {
		case "login", "logout":
			return []compGroup{{Title: "Providers", Matches: providerMatches}}
		}
	}
	return nil
}

// --- dropdown navigation / accept --------------------------------------------

// handleCompletionKey gives the completions dropdown first refusal on a
// keystroke while it's visible — matches taui's OverlayStack precedence
// (pkg/taui's Completions is a "soft" overlay: it consumes the keys it
// recognizes — arrows, tab, enter, esc — and anything else falls through to
// normal input handling unchanged). Returns (cmd, true) when consumed.
func (m *model) handleCompletionKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	rows, token := m.completionRows()
	if len(rows) == 0 {
		return nil, false
	}
	if m.compToken != token {
		m.compToken = token
		m.compSelected = 0
	}
	if m.compDismissed && m.compDismissedToken == token {
		return nil, false
	}
	if m.compSelected < 0 || m.compSelected >= len(rows) {
		m.compSelected = 0
	}

	switch msg.String() {
	case "up":
		if m.compSelected > 0 {
			m.compSelected--
		}
		return nil, true
	case "down":
		if m.compSelected < len(rows)-1 {
			m.compSelected++
		}
		return nil, true

	case "tab":
		// LCP extension: if the shared prefix across all matches extends
		// further than what's typed, extend to it (matches taui's Tab
		// behaviour). Otherwise accept the highlighted row.
		//
		// When the LCP is already present at the end of the input
		// (e.g. token is "" for a bare "/" and LCP is "/"), extending
		// is a no-op that would just reset compSelected to 0 — fall
		// through to accepting the highlighted row instead so the
		// viewport doesn't jump away from the item the user targeted.
		lcp := longestCommonWordPrefix(rows)
		if len([]rune(lcp)) > len([]rune(token)) && !strings.HasSuffix(m.input, lcp) {
			m.replaceCompletionToken(lcp)
		} else {
			m.acceptCompletionRow(rows[m.compSelected])
		}
		return nil, true

	case "shift+tab":
		// Navigation only (like up/down), not an accept — jumps the highlight
		// straight to the last row instead of stepping one row at a time.
		m.compSelected = len(rows) - 1
		return nil, true

	case "enter":
		// Enter always accepts the highlighted row. Unlike Tab, it also
		// submits immediately when the accepted row doesn't require further
		// typing (RequiresArg false: a no/optional-argument command, or any
		// argument-value completion, which is always a leaf value) — so
		// completing an unambiguous command like /help or /clear takes one
		// Enter, not two. A required-argument command (RequiresArg true,
		// e.g. /model, /system) instead leaves the line open for its
		// argument, since submitting it bare would just be rejected anyway.
		row := rows[m.compSelected]
		m.acceptCompletionRow(row)
		if !row.RequiresArg {
			return m.submitInput(), true
		}
		return nil, true

	case "esc":
		// First Esc for this token hides the dropdown without touching the
		// input. It stays hidden (checked above) until the token changes,
		// so a second Esc falls through to the normal
		// input-clearing/turn-cancelling handler instead of being
		// swallowed forever.
		m.compDismissed = true
		m.compDismissedToken = token
		return nil, true
	}

	return nil, false
}

// acceptCompletionRow replaces the current token with row's word (plus a
// trailing space so the user can keep typing the next argument) and resets
// selection state.
func (m *model) acceptCompletionRow(row compRow) {
	m.replaceCompletionToken(row.Word + " ")
}

// replaceCompletionToken replaces the last whitespace-delimited token of
// m.input with replacement, moves the cursor to the end, and resets
// selection state so the next keystroke re-derives it against the new text.
func (m *model) replaceCompletionToken(replacement string) {
	fields := strings.Fields(m.input)
	if len(fields) == 0 {
		m.input = replacement
	} else if strings.HasSuffix(m.input, " ") {
		m.input += replacement
	} else {
		fields[len(fields)-1] = strings.TrimRight(replacement, " ")
		m.input = strings.Join(fields, " ")
		if strings.HasSuffix(replacement, " ") {
			m.input += " "
		}
	}
	m.inputCursor = utf8.RuneCountInString(m.input)
	m.compSelected = 0
	m.compToken = m.completionToken()
}

// longestCommonWordPrefix returns the longest prefix shared by every row's
// Word (case-sensitive, matching taui's Tab-extension behaviour). Operates on
// runes, not bytes, so a multi-byte UTF-8 candidate can't be cut mid-rune.
func longestCommonWordPrefix(rows []compRow) string {
	if len(rows) == 0 {
		return ""
	}
	prefix := []rune(rows[0].Word)
	for _, r := range rows[1:] {
		w := []rune(r.Word)
		n := 0
		for n < len(prefix) && n < len(w) && prefix[n] == w[n] {
			n++
		}
		prefix = prefix[:n]
		if len(prefix) == 0 {
			break
		}
	}
	return string(prefix)
}
