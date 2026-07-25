package tui2

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

func TestCtrlPOpensIndependentCommandPalette(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "draft prompt"
	m.inputCursor = len([]rune(m.input))

	m.dispatchKey(key('p', tea.ModCtrl))

	if m.palette == nil || m.palette.kind != paletteCommands {
		t.Fatal("expected command palette to be open")
	}
	if m.input != "draft prompt" {
		t.Fatalf("input = %q, want draft preserved", m.input)
	}
	if rows := m.paletteRows(); len(rows) == 0 {
		t.Fatal("expected command rows")
	}
}

func TestCommandPalettePrefillsQueryFromSlashInput(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "/mod"
	m.inputCursor = 4

	m.dispatchKey(key('p', tea.ModCtrl))

	if got := m.palette.picker.Query(); got != "mod" {
		t.Fatalf("query = %q, want %q", got, "mod")
	}
	if m.input != "/mod" {
		t.Fatalf("input = %q, want unchanged", m.input)
	}
}

func TestPaletteTypingNarrowsRowsWithoutEditingComposer(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "keep me"
	m.dispatchKey(key('p', tea.ModCtrl))

	for _, r := range "clear" {
		m.dispatchKey(charKey(r))
	}
	if got := m.palette.picker.Query(); got != "clear" {
		t.Fatalf("query = %q, want %q", got, "clear")
	}
	if m.input != "keep me" {
		t.Fatalf("input = %q, want composer untouched", m.input)
	}
	rows := m.paletteRows()
	if len(rows) != 1 || rows[0].Word != "clear" {
		t.Fatalf("rows = %+v, want one clean clear label", rows)
	}

	m.dispatchKey(key(tea.KeyBackspace, tea.ModCtrl))
	if got := m.palette.picker.Query(); got != "" {
		t.Fatalf("query = %q after Ctrl+Backspace, want empty", got)
	}
}

func TestPaletteAcceptModelCommandOpensModelSelector(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.availableModels = []tauchat.ChatModelRef{{ID: "gpt-4", Provider: "openai"}}
	m.dispatchKey(key('p', tea.ModCtrl))
	for _, r := range "model" {
		m.dispatchKey(charKey(r))
	}

	m.dispatchKey(key(tea.KeyEnter, 0))

	if m.palette == nil || m.palette.kind != paletteModels {
		t.Fatal("expected command palette to transition to model selector")
	}
	if m.input != "" {
		t.Fatalf("input = %q, want composer untouched", m.input)
	}
}

func TestTypingPickerBackedCommandPromotesToSelector(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.availableModels = []tauchat.ChatModelRef{{ID: "gpt-4", Provider: "openai"}}
	m.input = "/model"
	m.inputCursor = len([]rune(m.input))

	m.handleKey(charKey(' '))

	if m.palette == nil || m.palette.kind != paletteModels {
		t.Fatal("expected trailing space to open model selector")
	}
	if m.input != "" {
		t.Fatalf("input = %q, want command scaffold removed", m.input)
	}
}

func TestProviderSelectorUsesSharedPickerAndSupportsActions(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.openProviderPalette("")

	rows := m.paletteRows()
	login := -1
	for i, row := range rows {
		if row.Word == "login" {
			login = i
			break
		}
	}
	if login < 0 {
		t.Fatalf("provider rows = %+v, want login action", rows)
	}
	m.palette.picker.selected = login
	m.handlePaletteKey(key(tea.KeyEnter, 0))

	if m.palette == nil || m.palette.kind != paletteProviders || m.palette.providerAction != "login" {
		t.Fatal("expected login action to transition to provider login selector")
	}
	if title := m.palette.picker.title; title != "Provider Login" {
		t.Fatalf("title = %q, want Provider Login", title)
	}
}

func TestBareProviderCommandOpensSharedSelector(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.cmdProvider("")

	if m.palette == nil || m.palette.kind != paletteProviders {
		t.Fatal("expected bare /provider to open provider selector")
	}
	if out := stripANSI(m.renderPaletteOverlay()); !strings.Contains(out, "Search") {
		t.Fatalf("provider selector missing shared search field:\n%s", out)
	}
}

func TestPaletteAgentSelectionChangesModeWithoutEditingComposer(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "draft prompt"
	m.inputCursor = len([]rune(m.input))
	m.dispatchKey(key('p', tea.ModCtrl))
	for _, r := range "plan" {
		m.dispatchKey(charKey(r))
	}

	m.dispatchKey(key(tea.KeyEnter, 0))

	if m.inputModeCommand != "plan" {
		t.Fatalf("mode = %q, want plan", m.inputModeCommand)
	}
	if m.input != "draft prompt" {
		t.Fatalf("input = %q, want composer unchanged", m.input)
	}
	if m.palette != nil {
		t.Fatal("expected palette to close")
	}
}

func TestPaletteAcceptNoArgSubmitsImmediately(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.dispatchKey(key('p', tea.ModCtrl))
	for _, r := range "clear" {
		m.dispatchKey(charKey(r))
	}

	m.dispatchKey(key(tea.KeyEnter, 0))

	if m.palette != nil {
		t.Fatal("expected palette to close")
	}
	if m.input != "" {
		t.Fatalf("input = %q, want cleared after submission", m.input)
	}
}

func TestPaletteEscClosesWithoutChangingComposer(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "draft"
	m.dispatchKey(key('p', tea.ModCtrl))
	m.dispatchKey(charKey('x'))

	m.dispatchKey(key(tea.KeyEscape, 0))

	if m.palette != nil {
		t.Fatal("expected Esc to close palette")
	}
	if m.input != "draft" {
		t.Fatalf("input = %q, want preserved", m.input)
	}
}

func TestCtrlLNoModelsNotifies(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.availableModels = nil

	cmd := m.dispatchKey(key('l', tea.ModCtrl))
	drainCmd(cmd)

	if m.palette != nil {
		t.Fatal("expected no palette when no models are available")
	}
	if m.notification == "" {
		t.Fatal("expected a notification when no models are available")
	}
}

func TestCtrlLOpensSearchableModelPaletteAndApplies(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.availableModels = []tauchat.ChatModelRef{
		{ID: "gpt-4", Provider: "openai"},
		{ID: "claude-3", Provider: "anthropic"},
	}
	m.input = "draft"

	m.dispatchKey(key('l', tea.ModCtrl))
	if m.palette == nil || m.palette.kind != paletteModels {
		t.Fatal("expected model palette to be open")
	}
	if m.input != "draft" {
		t.Fatalf("input = %q, want draft preserved while browsing", m.input)
	}
	for _, r := range "gpt" {
		m.dispatchKey(charKey(r))
	}
	rows := m.paletteRows()
	if len(rows) != 1 || rows[0].Word != "gpt-4" {
		t.Fatalf("rows = %+v, want gpt-4", rows)
	}

	cmd := m.dispatchKey(key(tea.KeyEnter, 0))
	drainCmd(cmd)

	if m.modelName != "gpt-4" {
		t.Fatalf("modelName = %q, want %q", m.modelName, "gpt-4")
	}
	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 UpdateChatSessionCommand, got %d", len(rt.sent))
	}
	if _, ok := rt.sent[0].(tauchat.UpdateChatSessionCommand); !ok {
		t.Fatalf("expected UpdateChatSessionCommand, got %T", rt.sent[0])
	}
}

func TestModelPalettePreservesProviderForDuplicateIDs(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.availableModels = []tauchat.ChatModelRef{
		{ID: "gpt-5.6-sol", Provider: "openai"},
		{ID: "gpt-5.6-sol", Provider: "openai-codex"},
	}

	m.dispatchKey(key('l', tea.ModCtrl))
	rows := m.paletteRows()
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want two provider-specific entries", rows)
	}
	if rows[1].Description != "openai-codex" {
		t.Fatalf("second row provider = %q, want openai-codex", rows[1].Description)
	}
	m.palette.picker.selected = 1
	cmd := m.dispatchKey(key(tea.KeyEnter, 0))
	drainCmd(cmd)

	if m.provider != "openai-codex" {
		t.Fatalf("provider = %q, want openai-codex", m.provider)
	}
	if len(rt.sent) != 1 {
		t.Fatalf("sent %d commands, want1", len(rt.sent))
	}
	update, ok := rt.sent[0].(tauchat.UpdateChatSessionCommand)
	if !ok {
		t.Fatalf("command = %T, want UpdateChatSessionCommand", rt.sent[0])
	}
	if update.Patch.Provider == nil || *update.Patch.Provider != "openai-codex" {
		t.Fatalf("provider patch = %v, want openai-codex", update.Patch.Provider)
	}
}

func TestCtrlLSwitchesOpenCommandPaletteToModels(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.availableModels = []tauchat.ChatModelRef{{ID: "gpt-4", Provider: "openai"}}
	m.dispatchKey(key('p', tea.ModCtrl))
	m.dispatchKey(charKey('c'))

	m.dispatchKey(key('l', tea.ModCtrl))

	if m.palette == nil || m.palette.kind != paletteModels {
		t.Fatal("expected Ctrl+L to switch to the model palette")
	}
	if got := m.palette.picker.Query(); got != "" {
		t.Fatalf("model query = %q, want fresh query", got)
	}
}

func TestPaletteRenderShowsSearchAndCleanCommandLabels(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.width = 100
	m.dispatchKey(key('p', tea.ModCtrl))
	for _, r := range "clear" {
		m.dispatchKey(charKey(r))
	}

	out := stripANSI(m.renderPaletteOverlay())
	if !strings.Contains(out, "Search  clear") {
		t.Fatalf("render missing search field:\n%s", out)
	}
	if !strings.Contains(out, "▶ clear") {
		t.Fatalf("render missing clean command label:\n%s", out)
	}
	if strings.Contains(out, "▶ /clear") {
		t.Fatalf("render retained slash-command styling:\n%s", out)
	}
}

func TestListPickerEditingAndNavigationAreReusable(t *testing.T) {
	p := newListPicker("Anything")
	p.SetQuery("alpha beta")

	p.HandleKey(key(tea.KeyBackspace, tea.ModCtrl), 3)
	if got := p.Query(); got != "alpha " {
		t.Fatalf("query = %q, want %q", got, "alpha ")
	}
	p.HandleKey(key(tea.KeyDown, 0), 3)
	p.HandleKey(key(tea.KeyDown, 0), 3)
	p.HandleKey(key(tea.KeyDown, 0), 3)
	if got := p.ClampSelection(3); got != 2 {
		t.Fatalf("selected = %d, want clamped 2", got)
	}
}

func TestPickerSearchLongQueryStaysWithinWidth(t *testing.T) {
	const width = 18
	query := "a very long palette query"
	out := renderPickerSearch(query, len([]rune(query)), width)
	if got := visibleWidth(out); got > width {
		t.Fatalf("search width = %d, want <= %d: %q", got, width, stripANSI(out))
	}
	if !strings.Contains(stripANSI(out), "…") {
		t.Fatalf("search = %q, want leading ellipsis", stripANSI(out))
	}
}
