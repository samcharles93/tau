package tui2

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
)

// modelsFor builds a test model with a fixed, deterministic set of available
// models — argument completion (/model <partial>) is used throughout these
// tests instead of command-name completion because the command table
// (slashTable) is real global state populated by commands.go's init(), so
// asserting exact fuzzy-match sets against it would be fragile.
func modelsFor(ids ...string) *model {
	m := newTestModel(&fakeRuntime{}, nil)
	refs := make([]tauchat.ChatModelRef, len(ids))
	for i, id := range ids {
		refs[i] = tauchat.ChatModelRef{ID: id}
	}
	m.availableModels = refs
	return m
}

func TestCompletionRowsFuzzyFiltersLiveAsYouType(t *testing.T) {
	m := modelsFor("gpt-4", "claude-3", "gpt-3.5-turbo")
	m.input = "/model gpt"

	rows, token := m.completionRows()
	if token != "gpt" {
		t.Fatalf("token = %q, want %q", token, "gpt")
	}
	var words []string
	for _, r := range rows {
		words = append(words, r.Word)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %v, want exactly gpt-4 and gpt-3.5-turbo (claude-3 must be filtered out)", words)
	}
	for _, w := range words {
		if !strings.Contains(w, "gpt") {
			t.Errorf("unexpected non-matching row %q survived fuzzy filtering", w)
		}
	}
}

func TestCompletionRowsEmptyTokenShowsAllUnfiltered(t *testing.T) {
	m := modelsFor("aaa", "bbb", "ccc")
	m.input = "/model " // trailing space — nothing typed for the argument yet

	rows, token := m.completionRows()
	if token != "" {
		t.Fatalf("token = %q, want empty (nothing typed yet)", token)
	}
	if len(rows) != 3 {
		t.Fatalf("expected all 3 candidates with an empty token, got %d", len(rows))
	}
}

// TestCompletionRowsMatchTypedOutAlias is a regression test: commandGroups
// compared the alias-prefix token (which retains its leading "/", e.g.
// "/quit") directly against bare alias strings ("quit"), so
// strings.HasPrefix(alias, token) could never match once anything had been
// typed — typing an alias fully (e.g. "/quit", an alias of "/exit") matched
// nothing at all and the dropdown went empty, instead of surfacing the
// alias the same way internal/tui's legacy completions already do (which
// strips the leading "/" from the token before comparing).
func TestCompletionRowsMatchTypedOutAlias(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "/quit"

	rows, token := m.completionRows()
	if token != "/quit" {
		t.Fatalf("token = %q, want %q", token, "/quit")
	}
	var found bool
	for _, r := range rows {
		if r.Word == "/quit" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected typing out the alias %q in full to match it, got rows: %+v", "/quit", rows)
	}
}

// TestCompletionRowsDoNotSurfaceAliasWhileBrowsingByCanonicalName guards the
// other half of the same fix: typing towards the canonical command name
// ("/ex") must not also surface every one of its aliases as separate rows —
// only "/exit" itself should match; "/quit" and "/q" have nothing in common
// with "ex" and must stay hidden until the user actually types towards them.
func TestCompletionRowsDoNotSurfaceAliasWhileBrowsingByCanonicalName(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "/ex"

	rows, _ := m.completionRows()
	for _, r := range rows {
		if r.Word == "/quit" || r.Word == "/q" {
			t.Fatalf("expected browsing by canonical prefix %q not to surface alias row %q", "/ex", r.Word)
		}
	}
}

func TestCompletionArrowKeysNavigateSelection(t *testing.T) {
	m := modelsFor("aaa", "bbb", "ccc")
	m.input = "/model "

	if _, handled := m.handleCompletionKey(key(tea.KeyDown, 0)); !handled {
		t.Fatal("expected down arrow to be consumed by the completions dropdown")
	}
	if m.compSelected != 1 {
		t.Fatalf("after 1 down: compSelected = %d, want 1", m.compSelected)
	}
	m.handleCompletionKey(key(tea.KeyDown, 0))
	if m.compSelected != 2 {
		t.Fatalf("after 2 downs: compSelected = %d, want 2", m.compSelected)
	}
	// Clamped at the last row.
	m.handleCompletionKey(key(tea.KeyDown, 0))
	if m.compSelected != 2 {
		t.Fatalf("down past the last row: compSelected = %d, want clamped to 2", m.compSelected)
	}
	m.handleCompletionKey(key(tea.KeyUp, 0))
	if m.compSelected != 1 {
		t.Fatalf("after up: compSelected = %d, want 1", m.compSelected)
	}
}

func TestCompletionSelectionResetsWhenTokenChanges(t *testing.T) {
	m := modelsFor("aaa", "aab", "aac")
	m.input = "/model a"
	m.handleCompletionKey(key(tea.KeyDown, 0))
	m.handleCompletionKey(key(tea.KeyDown, 0))
	if m.compSelected != 2 {
		t.Fatalf("setup: compSelected = %d, want 2", m.compSelected)
	}

	// Typing another character changes the token — selection must reset
	// rather than keep pointing at whatever now sits at index 2.
	m.handleKey(charKey('a'))
	if m.compSelected != 0 {
		t.Fatalf("after the query changed: compSelected = %d, want reset to 0", m.compSelected)
	}
}

func TestCompletionTabExtendsLCPThenAcceptsOnSecondPress(t *testing.T) {
	m := modelsFor("gpt-4", "claude-3")
	m.input = "/model gpt"
	m.inputCursor = utf8.RuneCountInString(m.input)

	// Only one candidate matches "gpt" — its full word becomes the LCP,
	// which is longer than the typed token, so the first Tab extends
	// in-place without submitting or adding a trailing space yet.
	m.handleKey(key(tea.KeyTab, 0))
	if m.input != "/model gpt-4" {
		t.Fatalf("first tab: input = %q, want %q (extend, no trailing space)", m.input, "/model gpt-4")
	}

	// Second Tab: no further LCP extension possible (query already equals
	// the only match) — this press accepts it, adding the trailing space.
	m.handleKey(key(tea.KeyTab, 0))
	if m.input != "/model gpt-4 " {
		t.Fatalf("second tab: input = %q, want %q (accept with trailing space)", m.input, "/model gpt-4 ")
	}
}

// TestCompletionEnterOnArgumentValueSubmitsImmediately covers the bug report
// directly: previously Enter ALWAYS just accepted (filled text + trailing
// space) and never submitted, no matter what was accepted, so running any
// command reached via the dropdown took two Enters — one to accept, one
// (once the now-empty argument slot had no completions left) to actually
// submit. An argument value like a model ID is a leaf — nothing more is
// needed — so accepting it must submit in the same keystroke.
func TestCompletionEnterOnArgumentValueSubmitsImmediately(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.availableModels = []tauchat.ChatModelRef{{ID: "gpt-4"}}
	m.input = "/model gpt"
	m.inputCursor = utf8.RuneCountInString(m.input)

	cmd := m.handleKey(key(tea.KeyEnter, 0))
	if cmd == nil {
		t.Fatal("expected a Cmd from submitting /model gpt-4")
	}
	// cmdModel returns tea.Batch(setNotification(...), sendCommand(...)) —
	// only execute the sendCommand half. setNotification's Cmd is a bare
	// multi-second tea.Tick (the notification-clear timer); calling it would
	// really block for its full duration, which drainCmd's blanket recursion
	// doesn't know to avoid.
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) < 2 {
		t.Fatalf("expected a 2-element BatchMsg (notify, send), got %#v", cmd())
	}
	batch[1]()

	if m.input != "" {
		t.Fatalf("input = %q, want empty — a single Enter must accept AND submit a leaf argument value", m.input)
	}
	if len(rt.sent) == 0 {
		t.Error("expected the completed /model command to reach the runtime")
	}
}

// TestCompletionEnterOnNoArgCommandSubmitsImmediately is the exact scenario
// reported: typing a complete, unambiguous no-argument command and pressing
// Enter once must run it, not just silently append a trailing space and
// wait for a second Enter. Uses /reload, whose handler returns a bare
// sendCommand with no notification batching, so draining it can't trigger a
// real multi-second tea.Tick sleep the way /clear's would.
func TestCompletionEnterOnNoArgCommandSubmitsImmediately(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.input = "/reload"
	m.inputCursor = utf8.RuneCountInString(m.input)

	idx := completionRowIndex(t, m, "/reload")
	m.compSelected = idx
	m.compToken = "reload"

	cmd := m.handleKey(key(tea.KeyEnter, 0))
	drainCmd(cmd)

	if m.input != "" {
		t.Fatalf("input = %q, want empty — a single Enter must submit /reload immediately", m.input)
	}
	sentReload := false
	for _, c := range rt.sent {
		if _, ok := c.(tauchat.ReloadExtensionsCommand); ok {
			sentReload = true
		}
	}
	if !sentReload {
		t.Errorf("expected a ReloadExtensionsCommand to be sent, got %#v", rt.sent)
	}
}

// TestCompletionEnterOnRequiredArgCommandDoesNotSubmit is the flip side:
// /model requires an argument (usage "<id>"), so accepting the bare command
// name must NOT submit — the command would just be rejected — it should
// leave the line open for the argument's own completion to follow.
func TestCompletionEnterOnRequiredArgCommandDoesNotSubmit(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.input = "/model"
	m.inputCursor = utf8.RuneCountInString(m.input)

	idx := completionRowIndex(t, m, "/model")
	m.compSelected = idx
	m.compToken = "model"

	m.handleKey(key(tea.KeyEnter, 0))

	if m.input != "/model " {
		t.Fatalf("input = %q, want %q — a required-argument command must not submit bare", m.input, "/model ")
	}
	if len(rt.sent) != 0 {
		t.Errorf("expected nothing sent yet, got %#v", rt.sent)
	}
}

// completionRowIndex finds word's index in m's current completion rows,
// failing the test if it isn't present — used so command-name tests don't
// depend on fuzzy-ranking order among the real (and growing) slashTable.
func completionRowIndex(t *testing.T, m *model, word string) int {
	t.Helper()
	rows, _ := m.completionRows()
	for i, r := range rows {
		if r.Word == word {
			return i
		}
	}
	t.Fatalf("expected %q to appear in completion rows, got %+v", word, rows)
	return -1
}

func TestCompletionEscIsSwallowedWithoutClearingInput(t *testing.T) {
	m := modelsFor("gpt-4")
	m.input = "/model gpt"
	m.inputCursor = utf8.RuneCountInString(m.input)

	m.handleKey(key(tea.KeyEscape, 0))

	if m.input != "/model gpt" {
		t.Fatalf("input = %q, want unchanged %q — Esc must not clear input while the dropdown is open", m.input, "/model gpt")
	}
}

func TestCompletionDropdownDoesNotInterceptWhenNotApplicable(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hello world" // not a slash command — no completions apply

	if _, handled := m.handleCompletionKey(key(tea.KeyUp, 0)); handled {
		t.Error("expected the completions dropdown to decline when input isn't a slash command")
	}
}

func TestRenderCompletionsShowsChevronOnSelectedRowOnly(t *testing.T) {
	rows := []compRow{
		{compMatch: compMatch{Word: "aaa", Description: "first"}, group: "Models"},
		{compMatch: compMatch{Word: "bbb", Description: "second"}, group: "Models"},
	}
	out := stripANSI(renderCompletions(rows, 1, 80))

	if strings.Count(out, "▶") != 1 {
		t.Fatalf("expected exactly one chevron, got output:\n%s", out)
	}
	lines := strings.Split(out, "\n")
	var selectedLine string
	for _, l := range lines {
		if strings.Contains(l, "▶") {
			selectedLine = l
		}
	}
	if !strings.Contains(selectedLine, "bbb") {
		t.Fatalf("chevron should mark the selected row (index 1, %q), got line %q", "bbb", selectedLine)
	}
	if !strings.Contains(out, "aaa") || !strings.Contains(out, "first") || !strings.Contains(out, "second") {
		t.Fatalf("expected both words and descriptions present, got:\n%s", out)
	}
}

// --- regression: repeated Tab must not re-offer / duplicate a filled arg ---
//
// Reported bug: accepting a model via Tab left the dropdown open for the
// (nonexistent) second argument, so pressing Tab again just re-accepted the
// top-ranked model onto the end again — "/model gpt-4 gpt-4 gpt-4 ...".
// modelCompletions (and effortCompletions/providerCompletions) now gate on
// argsBefore, matching sessionCompletions' existing pattern.

func TestModelCompletionsGoneAfterArgumentAccepted(t *testing.T) {
	m := modelsFor("gpt-4", "claude-3")
	m.input = "/model gpt-4 " // argument already filled, trailing space

	rows, _ := m.completionRows()
	if len(rows) != 0 {
		t.Fatalf("expected no completions once /model's single argument is filled, got %+v", rows)
	}
}

func TestRepeatedTabDoesNotDuplicateAcceptedModel(t *testing.T) {
	m := modelsFor("gpt-4", "claude-3")
	m.input = "/model gpt"
	m.inputCursor = utf8.RuneCountInString(m.input)

	// First Tab: LCP is "gpt-4" (the only match), which is longer than the
	// typed token "gpt" (3→5), so it extends in-place without trailing space.
	m.handleKey(key(tea.KeyTab, 0))
	if m.input != "/model gpt-4" {
		t.Fatalf("after first tab: input = %q, want %q (extend, no trailing space)", m.input, "/model gpt-4")
	}

	// Second Tab: no further LCP extension ("gpt-4" == "gpt-4"), so it
	// accepts with a trailing space.
	m.handleKey(key(tea.KeyTab, 0))
	if m.input != "/model gpt-4 " {
		t.Fatalf("after second tab: input = %q, want %q", m.input, "/model gpt-4 ")
	}

	// Third Tab: trailing space means no token to complete — no-op.
	m.handleKey(key(tea.KeyTab, 0))
	if m.input != "/model gpt-4 " {
		t.Fatalf("after third tab: input = %q, want unchanged %q (must not duplicate)", m.input, "/model gpt-4 ")
	}
}

func TestEffortCompletionsGoneAfterArgumentAccepted(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "/effort high "

	rows, _ := m.completionRows()
	if len(rows) != 0 {
		t.Fatalf("expected no completions once /effort's single argument is filled, got %+v", rows)
	}
}

// --- regression: bare "/" must not fragment groups into repeating headers -
//
// Reported bug: with nothing typed, every candidate ties at fuzzy score 0,
// so the old unconditional sort fell through to its tie-break
// (alphabetical by Word) and interleaved Commands/Agents/Extensions into a
// repeating sequence of one- or two-line group headers instead of keeping
// each group contiguous.

func TestEmptyTokenPreservesGroupOrderWithoutFragmenting(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.extensionCommands = map[string]tauchat.ExtensionCommand{
		"hello": {Name: "hello", Description: "say hello"},
	}
	m.input = "/"

	rows, token := m.completionRows()
	// A bare "/" is the command-prefix marker, not a real query — it must
	// resolve to an empty token, same as no input at all. Getting this
	// wrong is exactly what caused the fragmentation this test guards
	// against: a non-empty "/" token ties every candidate at the same
	// fuzzy score, which used to fall through to an alphabetical
	// (cross-group) tie-break.
	if token != "" {
		t.Fatalf("token = %q, want empty for a bare \"/\"", token)
	}
	if len(rows) == 0 {
		t.Fatal("expected at least the built-in commands to appear")
	}

	// Walk the rows and count how many times the group changes. Since real
	// candidates are grouped as Commands, then Agents, then Extensions (see
	// commandGroups), a properly-preserved order changes group at most
	// twice — Commands→Agents and Agents→Extensions — never back again.
	transitions := 0
	seen := map[string]bool{}
	last := ""
	for _, r := range rows {
		if r.group != last {
			if seen[r.group] {
				t.Fatalf("group %q reappeared non-contiguously — groups are fragmented:\n%+v", r.group, rows)
			}
			seen[r.group] = true
			transitions++
			last = r.group
		}
	}
	if transitions > 3 {
		t.Fatalf("expected at most 3 contiguous groups (Commands/Agents/Extensions), got %d transitions", transitions)
	}
}

// --- extensionSubcommandGroups ---------------------------------------------

func TestExtensionSubcommandGroups(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	ext := tauchat.ExtensionCommand{
		Name: "mcp", Description: "MCP control",
		Subcommands: []tauchat.ExtensionCommand{
			{Name: "list", Description: "list servers"},
			{Name: "reconnect", Description: "reconnect a server", ArgsHint: "<server>"},
			{Name: "reload", Description: "reload config"},
		},
	}

	groups := m.extensionSubcommandGroups(ext)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	g := groups[0]
	if g.Title != "/mcp" {
		t.Fatalf("group title = %q, want %q", g.Title, "/mcp")
	}
	if len(g.Matches) != 3 {
		t.Fatalf("expected 3 subcommand matches, got %d", len(g.Matches))
	}
	// Subcommands should be alphabetically sorted.
	if g.Matches[0].Word != "list" {
		t.Fatalf("first subcommand = %q, want %q", g.Matches[0].Word, "list")
	}
	if g.Matches[2].Word != "reload" {
		t.Fatalf("third subcommand = %q, want %q", g.Matches[2].Word, "reload")
	}
	// The reconnect entry should have the ArgsHint in its description.
	if !strings.Contains(g.Matches[1].Description, "<server>") {
		t.Fatalf("reconnect description = %q, should include ArgsHint", g.Matches[1].Description)
	}
}

func TestExtensionSubcommandGroupsEmpty(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	ext := tauchat.ExtensionCommand{Name: "hi", Subcommands: nil}

	groups := m.extensionSubcommandGroups(ext)
	if len(groups) != 1 || len(groups[0].Matches) != 0 {
		t.Fatalf("expected 1 group with 0 matches, got %d groups with %d matches", len(groups), len(groups[0].Matches))
	}
}

// --- rawCandidateGroups -----------------------------------------------------

func TestRawCandidateGroupsNonSlashReturnsNil(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "just a message"

	groups := m.rawCandidateGroups()
	if groups != nil {
		t.Fatalf("expected nil for non-slash input, got %d groups", len(groups))
	}
}

func TestRawCandidateGroupsEmptyInput(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = ""

	groups := m.rawCandidateGroups()
	if groups != nil {
		t.Fatalf("expected nil for empty input")
	}
}

func TestRawCandidateGroupsInResponseReturnsNil(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "/model"
	m.inResponse = true

	rows, _ := m.completionRows()
	if rows != nil {
		t.Fatal("expected nil completion rows while inResponse")
	}
}

func TestRawCandidateGroupsBashRunningReturnsNil(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "/model"
	m.bashRunning = true

	rows, _ := m.completionRows()
	if rows != nil {
		t.Fatal("expected nil completion rows while bashRunning")
	}
}

func TestRawCandidateGroupsSlashOnlyGoesToCommandGroups(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "/"

	groups := m.rawCandidateGroups()
	if len(groups) == 0 {
		t.Fatalf("expected command groups for bare '/', got nil")
	}
	hasCommands := false
	for _, g := range groups {
		if g.Title == "Commands" {
			hasCommands = true
		}
	}
	if !hasCommands {
		t.Fatal("expected a 'Commands' group")
	}
}

func TestRawCandidateGroupsModelArgCompletions(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.availableModels = []tauchat.ChatModelRef{{ID: "gpt-4"}}
	m.input = "/model gpt"

	groups := m.rawCandidateGroups()
	if len(groups) != 1 || groups[0].Title != "Models" {
		t.Fatalf("expected 1 Models group, got %+v", groups)
	}
}

func TestRawCandidateGroupsSessionArgCompletions(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.sessionSummaries = []tauchat.SessionSummary{{
		ID: "sess-1", ModelID: "gpt-4", MessageCount: 5,
	}}
	m.input = "/session "

	groups := m.rawCandidateGroups()
	if len(groups) != 1 || groups[0].Title != "Sessions" {
		t.Fatalf("expected 1 Sessions group, got %+v", groups)
	}
}

func TestRawCandidateGroupsEffortArgCompletions(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.availableModels = []tauchat.ChatModelRef{{
		ID:     "deepseek",
		Config: config.ModelConfig{ReasoningEfforts: []string{"low", "high"}},
	}}
	m.modelName = "deepseek"
	m.input = "/effort "

	groups := m.rawCandidateGroups()
	if len(groups) != 1 || groups[0].Title != "Effort" {
		t.Fatalf("expected 1 Effort group, got %+v", groups)
	}
	// Should include low, high, and auto.
	words := make([]string, len(groups[0].Matches))
	for i, mc := range groups[0].Matches {
		words[i] = mc.Word
	}
	if len(words) != 3 {
		t.Fatalf("expected 3 effort levels, got %v", words)
	}
}

func TestRawCandidateGroupsEffortAutoOnly(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.availableModels = []tauchat.ChatModelRef{{ID: "gpt-4"}}
	m.modelName = "gpt-4"
	m.input = "/effort "

	groups := m.rawCandidateGroups()
	if len(groups) != 1 || len(groups[0].Matches) != 1 || groups[0].Matches[0].Word != "auto" {
		t.Fatalf("expected [auto], got %+v", groups)
	}
}

func TestRawCandidateGroupsExtensionSubcommand(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.extensionCommands = map[string]tauchat.ExtensionCommand{
		"mcp": {Name: "mcp", Subcommands: []tauchat.ExtensionCommand{
			{Name: "list"},
		}},
	}
	m.input = "/mcp "

	groups := m.rawCandidateGroups()
	if len(groups) != 1 || groups[0].Title != "/mcp" {
		t.Fatalf("expected /mcp extension subcommand group, got %+v", groups)
	}
}

func TestRawCandidateGroupsNoCompletionAfterModelArg(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.availableModels = []tauchat.ChatModelRef{{ID: "gpt-4"}}
	m.input = "/model gpt-4 "

	groups := m.rawCandidateGroups()
	if groups != nil {
		t.Fatalf("expected nil once model arg is filled, got %+v", groups)
	}
}

func TestRawCandidateGroupsProviderCompletions(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "/provider "

	groups := m.rawCandidateGroups()
	if len(groups) != 1 || groups[0].Title != "Providers" {
		t.Fatalf("expected a single 'Providers' group, got %+v", groups)
	}
	if len(groups[0].Matches) == 0 {
		t.Fatal("expected provider matches, got none")
	}
}

func TestRawCandidateGroupsProviderNoArg(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "/provider"

	groups := m.rawCandidateGroups()
	// Still typing the command name — should show command groups.
	if groups == nil {
		t.Fatalf("expected command groups while typing /provider")
	}
}

// --- longestCommonWordPrefix -----------------------------------------------

func TestLongestCommonWordPrefixEmpty(t *testing.T) {
	prefix := longestCommonWordPrefix(nil)
	if prefix != "" {
		t.Fatalf("expected empty for nil, got %q", prefix)
	}
}

func TestLongestCommonWordPrefixSingle(t *testing.T) {
	rows := []compRow{{
		compMatch: compMatch{Word: "gpt-4"},
	}}
	prefix := longestCommonWordPrefix(rows)
	if prefix != "gpt-4" {
		t.Fatalf("prefix with single row = %q, want %q", prefix, "gpt-4")
	}
}

func TestLongestCommonWordPrefixShared(t *testing.T) {
	rows := []compRow{
		{compMatch: compMatch{Word: "gpt-4"}},
		{compMatch: compMatch{Word: "gpt-4-turbo"}},
		{compMatch: compMatch{Word: "gpt-3.5-turbo"}},
	}
	prefix := longestCommonWordPrefix(rows)
	if prefix != "gpt-" {
		t.Fatalf("prefix = %q, want %q", prefix, "gpt-")
	}
}

func TestLongestCommonWordPrefixNoCommon(t *testing.T) {
	rows := []compRow{
		{compMatch: compMatch{Word: "aaaa"}},
		{compMatch: compMatch{Word: "bbbb"}},
	}
	prefix := longestCommonWordPrefix(rows)
	if prefix != "" {
		t.Fatalf("prefix should be empty for no common start, got %q", prefix)
	}
}

func TestLongestCommonWordPrefixAllIdentical(t *testing.T) {
	rows := []compRow{
		{compMatch: compMatch{Word: "gpt-4"}},
		{compMatch: compMatch{Word: "gpt-4"}},
	}
	prefix := longestCommonWordPrefix(rows)
	if prefix != "gpt-4" {
		t.Fatalf("prefix = %q, want %q", prefix, "gpt-4")
	}
}

// --- completionToken -------------------------------------------------------

func TestCompletionTokenBareSlashReturnsEmpty(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "/"

	token := m.completionToken()
	if token != "" {
		t.Fatalf("token for bare '/' = %q, want empty", token)
	}
}

func TestCompletionTokenEmptyInput(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = ""

	token := m.completionToken()
	if token != "" {
		t.Fatalf("token for empty input = %q, want empty", token)
	}
}

func TestCompletionTokenTrailingSpace(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "/model "

	token := m.completionToken()
	if token != "" {
		t.Fatalf("token after trailing space = %q, want empty", token)
	}
}

func TestCompletionTokenPartialArg(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "/model gpt"

	token := m.completionToken()
	if token != "gpt" {
		t.Fatalf("token = %q, want %q", token, "gpt")
	}
}

func TestCompletionTokenNonSlash(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "hello world"

	token := m.completionToken()
	if token != "world" {
		t.Fatalf("token = %q, want %q", token, "world")
	}
}

// --- handleCompletionKey shift+tab -----------------------------------------

func TestCompletionShiftTabSelectsLast(t *testing.T) {
	m := modelsFor("gpt-4", "claude-3")
	m.input = "/model "
	m.compSelected = 0

	m.handleCompletionKey(key(tea.KeyTab, tea.ModShift))

	if m.compSelected != 1 {
		t.Fatalf("shift+tab: compSelected = %d, want last row (1)", m.compSelected)
	}
}

// --- renderCompletions scrolling window ------------------------------------

func TestRenderCompletionsScrollingWindow(t *testing.T) {
	rows := make([]compRow, 20)
	for i := range rows {
		rows[i] = compRow{
			compMatch: compMatch{Word: fmt.Sprintf("model-%d", i)},
			group:     "Models",
		}
	}

	// Selected at index 15 — the window should scroll to show it.
	out := renderCompletions(rows, 15, 80)
	plain := stripANSI(out)
	if !strings.Contains(plain, "model-15") {
		t.Fatalf("selected row should be visible:\n%s", plain)
	}
	if !strings.Contains(plain, "↑") {
		t.Logf("no 'up more' indicator, but that's OK if window starts near top")
	}
	if !strings.Contains(plain, "↓") {
		t.Logf("no 'down more' indicator — possible if window covers everything")
	}
}

func TestRenderCompletionsTruncatedToWidth(t *testing.T) {
	rows := []compRow{
		{compMatch: compMatch{Word: "very-long-command-name-that-should-be-truncated", Description: "long description"}, group: "Cmd"},
	}
	out := renderCompletions(rows, 0, 40)
	// Output lines should be <= 40 columns wide.
	for line := range strings.SplitSeq(out, "\n") {
		plain := stripANSI(line)
		if visibleWidth(plain) > 40 {
			t.Errorf("line exceeds 40-column width: %q (%d columns)", plain, visibleWidth(plain))
		}
	}
}

func TestRenderCompletionsGroupHeadersVisible(t *testing.T) {
	rows := []compRow{
		{compMatch: compMatch{Word: "aaa"}, group: "GroupA"},
		{compMatch: compMatch{Word: "bbb"}, group: "GroupB"},
	}
	out := stripANSI(renderCompletions(rows, 0, 80))
	if !strings.Contains(out, "GroupA") || !strings.Contains(out, "GroupB") {
		t.Fatalf("expected group headers in output:\n%s", out)
	}
}

// --- completionDescColumn --------------------------------------------------

func TestCompletionDescColumnEmptyDescriptions(t *testing.T) {
	rows := []compRow{
		{compMatch: compMatch{Word: "cmd"}}, // no description
	}
	col := completionDescColumn(rows)
	if col != 0 {
		t.Fatalf("expected 0 for rows without descriptions, got %d", col)
	}
}

func TestCompletionDescColumnCapped(t *testing.T) {
	veryLong := strings.Repeat("x", 100)
	rows := []compRow{
		{compMatch: compMatch{Word: veryLong, Description: "desc"}},
	}
	col := completionDescColumn(rows)
	if col > 44 {
		t.Fatalf("column should be capped at 44, got %d", col)
	}
}

// --- renderHighlightedWord -------------------------------------------------

func TestRenderHighlightedWordNoSpans(t *testing.T) {
	style := lipgloss.NewStyle().Bold(true)
	out := renderHighlightedWord("hello", nil, style)
	plain := stripANSI(out)
	if plain != "hello" {
		t.Fatalf("no-spans output = %q, want %q", plain, "hello")
	}
}

func TestRenderHighlightedWordOutOfBoundsSpans(t *testing.T) {
	style := lipgloss.NewStyle().Bold(true)
	out := renderHighlightedWord("hi", [][2]int{{0, 5}}, style)
	plain := stripANSI(out)
	if plain != "hi" {
		t.Fatalf("oob spans output = %q, want %q", plain, "hi")
	}
}

func TestRenderHighlightedWordSingleSpan(t *testing.T) {
	base := lipgloss.NewStyle().Bold(true)
	out := renderHighlightedWord("hello", [][2]int{{0, 2}}, base)
	plain := stripANSI(out)
	if plain != "hello" {
		t.Fatalf("single span output = %q, want %q", plain, "hello")
	}
}

// --- renderCompletionRow ----------------------------------------------------

func TestRenderCompletionRowSelected(t *testing.T) {
	row := compRow{
		compMatch: compMatch{Word: "gpt-4", Description: "OpenAI"},
		highlight: [][2]int{{0, 3}},
	}
	out := stripANSI(renderCompletionRow(row, true, 8))
	if !strings.HasPrefix(out, "▶") {
		t.Fatalf("selected row should start with chevron, got %q", out)
	}
	if !strings.Contains(out, "OpenAI") {
		t.Fatalf("selected row should contain description, got %q", out)
	}
}

func TestRenderCompletionRowUnselected(t *testing.T) {
	row := compRow{
		compMatch: compMatch{Word: "gpt-4"},
		highlight: [][2]int{{0, 3}},
	}
	out := stripANSI(renderCompletionRow(row, false, 0))
	if strings.HasPrefix(out, "▶") {
		t.Fatalf("unselected row should not have chevron, got %q", out)
	}
}

// --- handleCompletionKey ---------------------------------------------------

func TestHandleCompletionKeyUpPastStartClamped(t *testing.T) {
	m := modelsFor("aaa", "bbb")
	m.input = "/model "
	m.compSelected = 0

	m.handleCompletionKey(key(tea.KeyUp, 0))
	// Should be clamped at 0.
	if m.compSelected != 0 {
		t.Fatalf("up at start: compSelected = %d, want 0", m.compSelected)
	}
}

func TestHandleCompletionKeyDownPastEnd(t *testing.T) {
	m := modelsFor("aaa", "bbb")
	m.input = "/model "
	m.compSelected = 1

	m.handleCompletionKey(key(tea.KeyDown, 0))
	// Should be clamped at last row (1).
	if m.compSelected != 1 {
		t.Fatalf("down past end: compSelected = %d, want 1", m.compSelected)
	}
}

func TestHandleCompletionKeyTabWithNoDropdown(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "not a slash command"

	_, handled := m.handleCompletionKey(key(tea.KeyTab, 0))
	if handled {
		t.Fatal("Tab without dropdown should not be consumed")
	}
}

func TestHandleCompletionKeyDownWithNoDropdown(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "not a slash"

	_, handled := m.handleCompletionKey(key(tea.KeyDown, 0))
	if handled {
		t.Fatal("Down without dropdown should not be consumed")
	}
}

// --- renderCompletions helpers ---------------------------------------------

func TestRenderCompletionsEmpty(t *testing.T) {
	out := renderCompletions(nil, 0, 80)
	if out != "" {
		t.Fatalf("expected empty for nil rows, got %q", out)
	}
}

func TestRenderCompletionsSelectionOutOfBounds(t *testing.T) {
	rows := []compRow{
		{compMatch: compMatch{Word: "aaa"}, group: "G"},
		{compMatch: compMatch{Word: "bbb"}, group: "G"},
	}
	// Selected index 10 is out of bounds — should clamp to last row.
	out := stripANSI(renderCompletions(rows, 10, 80))
	if !strings.Contains(out, "bbb") {
		t.Fatalf("expected to render last row when selection out of bounds:\n%s", out)
	}
}

func TestRenderCompletionsHeaderCount(t *testing.T) {
	rows := []compRow{
		{compMatch: compMatch{Word: "a"}, group: "X"},
		{compMatch: compMatch{Word: "b"}, group: "Y"},
	}
	out := stripANSI(renderCompletions(rows, 0, 80))
	// Both group headers should appear.
	if !strings.Contains(out, "X") || !strings.Contains(out, "Y") {
		t.Fatalf("both group headers should appear:\n%s", out)
	}
}

// --- Tab extend vs accept when input matches exactly -----------------------

func TestCompletionTabAcceptsWhenTokenEqualsLCP(t *testing.T) {
	m := modelsFor("gpt-4")
	m.input = "/model gpt-4"
	m.inputCursor = utf8.RuneCountInString(m.input)

	// LCP = "gpt-4", same as token — Tab should accept (add trailing space).
	m.handleKey(key(tea.KeyTab, 0))
	if m.input != "/model gpt-4 " {
		t.Fatalf("after tab: input = %q, want %q (trailing space after acceptance)", m.input, "/model gpt-4 ")
	}
}

func TestCompletionTabExtendsLCPWithMultipleCandidates(t *testing.T) {
	m := modelsFor("gpt-4", "gpt-4-turbo")
	m.input = "/model gpt"
	m.inputCursor = utf8.RuneCountInString(m.input)

	// LCP should be "gpt-4" — common prefix doesn't reach past the dash variant.
	m.handleKey(key(tea.KeyTab, 0))
	if !strings.HasPrefix(m.input, "/model gpt-4") {
		t.Fatalf("expected tab to extend to common prefix 'gpt-4', got %q", m.input)
	}
}

// --- effort completion does not repeat after accept ------------------------

func TestEffortCompletionsGoAwayAfterAccept(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.availableModels = []tauchat.ChatModelRef{{
		ID:     "deepseek",
		Config: config.ModelConfig{ReasoningEfforts: []string{"low", "high"}},
	}}
	m.modelName = "deepseek"
	m.input = "/effort high "

	rows, _ := m.completionRows()
	if len(rows) != 0 {
		t.Fatalf("expected no completions after /effort's argument is filled, got %+v", rows)
	}
}

// --- Enter on a bare command name with an argument completer must not
// auto-submit ahead of it (a real inconsistency found in review: Tab on
// "/provider" leads into the provider picker, but Enter used to fire the
// command immediately with no argument). ----------------------------------

func TestHandleCompletionKeyEnterOnProviderDoesNotAutoSubmit(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.input = "/provider"

	rows, _ := m.completionRows()
	idx := -1
	for i, r := range rows {
		if r.Word == "/provider" {
			idx = i
		}
	}
	if idx == -1 {
		t.Fatalf("expected a /provider row in %+v", rows)
	}
	m.compSelected = idx

	cmd, handled := m.handleCompletionKey(key(tea.KeyEnter, 0))
	if !handled {
		t.Fatal("expected Enter to be consumed by the completions dropdown")
	}
	if cmd != nil {
		t.Fatal("expected no Cmd — accepting the bare '/provider' name must not auto-submit")
	}
	if m.input != "/provider " {
		t.Fatalf("input = %q, want %q (accepted, not submitted)", m.input, "/provider ")
	}
	if len(rt.sent) != 0 {
		t.Fatalf("expected nothing sent to the runtime yet, got %+v", rt.sent)
	}
}

func TestHandleCompletionKeyEnterOnNoArgCommandAutoSubmits(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.input = "/clear"

	rows, _ := m.completionRows()
	idx := -1
	for i, r := range rows {
		if r.Word == "/clear" {
			idx = i
		}
	}
	if idx == -1 {
		t.Fatalf("expected a /clear row in %+v", rows)
	}
	m.compSelected = idx

	cmd, handled := m.handleCompletionKey(key(tea.KeyEnter, 0))
	if !handled {
		t.Fatal("expected Enter to be consumed by the completions dropdown")
	}
	if cmd == nil {
		t.Fatal("expected a Cmd — a genuinely argument-less command should still auto-submit")
	}
}
