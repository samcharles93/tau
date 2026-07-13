package tui2

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

func TestCtrlPOpensCommandPaletteByPrefillingSlash(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.dispatchKey(key('p', tea.ModCtrl))

	if m.input != "/" {
		t.Fatalf("input = %q, want %q", m.input, "/")
	}
	rows, _, ok := m.completionsVisible()
	if !ok || len(rows) == 0 {
		t.Fatal("expected the completions dropdown to be visible after Ctrl+P")
	}
}

func TestCtrlPDoesNotClobberExistingSlashInput(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.input = "/mod"
	m.inputCursor = 4

	m.dispatchKey(key('p', tea.ModCtrl))

	if m.input != "/mod" {
		t.Fatalf("input = %q, want unchanged %q", m.input, "/mod")
	}
}

func TestPaletteTypingNarrowsRowsAndSupportsWordDelete(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.dispatchKey(key('p', tea.ModCtrl))

	for _, r := range "clear" {
		m.dispatchKey(charKey(r))
	}
	if m.input != "/clear" {
		t.Fatalf("input = %q, want %q", m.input, "/clear")
	}
	rows, _, ok := m.completionsVisible()
	if !ok || len(rows) == 0 {
		t.Fatal("expected /clear to still match a row")
	}
	for _, row := range rows {
		if row.Word != "/clear" {
			t.Fatalf("unexpected row %q matched query 'clear'", row.Word)
		}
	}

	// Ctrl+Backspace (delete word before cursor) falls through to the exact
	// same input-editing path the main input box uses (deleteWordBeforeCursor,
	// input.go) — the whole point of driving the palette off m.input
	// directly. "/" isn't its own word boundary there (only space/newline
	// are, see wordLeft), so this deletes the whole "/clear" in one go —
	// same as it always has for the main input box.
	m.dispatchKey(key(tea.KeyBackspace, tea.ModCtrl))
	if m.input != "" {
		t.Fatalf("input = %q after Ctrl+Backspace, want empty", m.input)
	}
}

func TestPaletteAcceptRequiresArgLeavesInputOpen(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.dispatchKey(key('p', tea.ModCtrl))

	for _, r := range "model" {
		m.dispatchKey(charKey(r))
	}
	m.dispatchKey(key(tea.KeyEnter, 0))

	if m.input != "/model " {
		t.Fatalf("input = %q, want %q (left open for argument completion)", m.input, "/model ")
	}
}

func TestPaletteAcceptNoArgSubmitsImmediately(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.dispatchKey(key('p', tea.ModCtrl))

	for _, r := range "clear" {
		m.dispatchKey(charKey(r))
	}
	m.dispatchKey(key(tea.KeyEnter, 0))

	if m.input != "" {
		t.Fatalf("input = %q, want empty (submitted immediately)", m.input)
	}
}

func TestPaletteEscDismissesThenClearsInput(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.dispatchKey(key('p', tea.ModCtrl))
	if _, _, ok := m.completionsVisible(); !ok {
		t.Fatal("setup: expected the dropdown to be visible")
	}

	// First Esc dismisses the dropdown without touching input.
	m.dispatchKey(key(tea.KeyEscape, 0))
	if _, _, ok := m.completionsVisible(); ok {
		t.Fatal("expected Esc to dismiss the (now floating) dropdown")
	}
	if m.input != "/" {
		t.Fatalf("input = %q after first Esc, want unchanged %q", m.input, "/")
	}

	// Second Esc reaches the normal input-clearing handler.
	m.dispatchKey(key(tea.KeyEscape, 0))
	if m.input != "" {
		t.Fatalf("input = %q after second Esc, want cleared", m.input)
	}
}

func TestCtrlLNoModelsNotifies(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.availableModels = nil

	cmd := m.dispatchKey(key('l', tea.ModCtrl))
	drainCmd(cmd)

	if m.input != "" {
		t.Fatalf("input = %q, want unchanged/empty when no models are available", m.input)
	}
	if m.notification == "" {
		t.Fatal("expected a notification when no models are available")
	}
}

func TestCtrlLOpensModelPaletteAndApplies(t *testing.T) {
	rt := &fakeRuntime{}
	m := newTestModel(rt, nil)
	m.availableModels = []tauchat.ChatModelRef{
		{ID: "gpt-4", Provider: "openai"},
		{ID: "claude-3", Provider: "anthropic"},
	}

	m.dispatchKey(key('l', tea.ModCtrl))
	if m.input != "/model " {
		t.Fatalf("input = %q, want %q", m.input, "/model ")
	}
	rows, _, ok := m.completionsVisible()
	if !ok || len(rows) != 2 {
		t.Fatalf("expected 2 model rows visible, got %d (ok=%v)", len(rows), ok)
	}
	if title := completionsOverlayTitle(rows); title != "Models" {
		t.Fatalf("completionsOverlayTitle = %q, want %q", title, "Models")
	}

	cmd := m.dispatchKey(key(tea.KeyEnter, 0))
	drainCmd(cmd)

	if m.modelName != rows[0].Word {
		t.Fatalf("modelName = %q, want %q", m.modelName, rows[0].Word)
	}
	if len(rt.sent) != 1 {
		t.Fatalf("expected 1 UpdateChatSessionCommand, got %d", len(rt.sent))
	}
	if _, ok := rt.sent[0].(tauchat.UpdateChatSessionCommand); !ok {
		t.Fatalf("expected UpdateChatSessionCommand, got %T", rt.sent[0])
	}
}

func TestCtrlLWhileCommandPaletteOpenSwitchesToModels(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)
	m.availableModels = []tauchat.ChatModelRef{{ID: "gpt-4", Provider: "openai"}}

	m.dispatchKey(key('p', tea.ModCtrl)) // opens Commands
	rows, _, _ := m.completionsVisible()
	if title := completionsOverlayTitle(rows); title != "Commands" {
		t.Fatalf("completionsOverlayTitle = %q, want %q", title, "Commands")
	}

	// Regression: Ctrl+L must switch straight to the model picker even
	// though the command palette is already open — it must not get
	// swallowed as a keystroke by whatever's currently showing.
	m.dispatchKey(key('l', tea.ModCtrl))
	if m.input != "/model " {
		t.Fatalf("input = %q, want %q after Ctrl+L while Commands was open", m.input, "/model ")
	}
	rows, _, ok := m.completionsVisible()
	if !ok {
		t.Fatal("expected the dropdown to still be visible")
	}
	if title := completionsOverlayTitle(rows); title != "Models" {
		t.Fatalf("completionsOverlayTitle = %q, want %q", title, "Models")
	}
}
