package tui2

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// paletteWidthFrac/paletteMinWidth/paletteMaxWidth size the shared Ctrl+P
// command palette and Ctrl+L model picker. The Ctrl+O session tree reuses the
// width helper, but remains a separate component with its own state and
// rendering.
const (
	paletteWidthFrac = 0.6
	paletteMinWidth  = 40
	paletteMaxWidth  = 70
)

type paletteKind int

const (
	paletteCommands paletteKind = iota
	paletteModels
	paletteProviders
)

// paletteState owns the floating picker's input and selection. Keeping this
// separate from model.input is what lets the palette present a real search
// field instead of masquerading as a slash command in the main prompt.
type paletteState struct {
	kind           paletteKind
	picker         listPicker
	providerAction string
}

func (m *model) openCommandPalette() tea.Cmd {
	if m.inResponse() || m.bashRunning {
		return nil
	}
	m.closeOtherExclusiveOverlays(overlayPalette)
	query := ""
	if strings.HasPrefix(m.input, "/") && !strings.ContainsAny(m.input, " \n") {
		query = strings.TrimPrefix(m.input, "/")
	}
	picker := newListPicker("Command Palette")
	picker.SetQuery(query)
	m.palette = &paletteState{kind: paletteCommands, picker: picker}
	return nil
}

func (m *model) openModelPalette() tea.Cmd {
	if len(m.availableModels) == 0 {
		return m.setNotification("no models available - try /refresh")
	}
	if m.inResponse() || m.bashRunning {
		return nil
	}
	m.closeOtherExclusiveOverlays(overlayPalette)
	m.palette = &paletteState{kind: paletteModels, picker: newListPicker("Model Selector")}
	return nil
}

func (m *model) openProviderPalette(action string) tea.Cmd {
	if m.inResponse() || m.bashRunning {
		return nil
	}
	m.closeOtherExclusiveOverlays(overlayPalette)
	title := "Provider Selector"
	switch action {
	case "login":
		title = "Provider Login"
	case "logout":
		title = "Provider Logout"
	}
	m.palette = &paletteState{
		kind:           paletteProviders,
		picker:         newListPicker(title),
		providerAction: action,
	}
	return nil
}

func (m *model) paletteRows() []compRow {
	if m.palette == nil {
		return nil
	}

	var groups []compGroup
	switch m.palette.kind {
	case paletteCommands:
		// commandGroups still owns discovery, aliases, grouping, and argument
		// metadata. Only the palette-facing labels lose their slash prefix.
		groups = m.commandGroups(m.palette.picker.Query())
		for gi := range groups {
			for mi := range groups[gi].Matches {
				groups[gi].Matches[mi].Word = strings.TrimPrefix(groups[gi].Matches[mi].Word, "/")
			}
		}
	case paletteModels:
		groups = m.modelCompletions(0)
	case paletteProviders:
		fields := []string{"/provider"}
		argsBefore := 0
		if action := m.palette.providerAction; action != "" {
			fields = append(fields, action)
			argsBefore = 1
		}
		groups = m.providerCompletions(fields, argsBefore)
	}
	return filterAndRankRows(groups, m.palette.picker.Query())
}

// handlePaletteKey owns every key while the palette is open so search input
// never leaks into the chat composer behind it.
func (m *model) handlePaletteKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.palette == nil {
		return nil
	}

	switch msg.String() {
	case "ctrl+p":
		// Repeating the active shortcut intentionally resets its query;
		// switching shortcuts replaces it with a fresh picker of the other kind.
		// Esc is the close gesture for both palettes.
		return m.openCommandPalette()
	case "ctrl+l":
		return m.openModelPalette()
	}

	rows := m.paletteRows()
	action, handled := m.palette.picker.HandleKey(msg, len(rows))
	if !handled {
		return nil
	}
	switch action {
	case pickerActionClose:
		m.palette = nil
	case pickerActionSelect:
		return m.acceptPaletteRow(rows[m.palette.picker.ClampSelection(len(rows))])
	}
	return nil
}

func (m *model) acceptPaletteRow(row compRow) tea.Cmd {
	kind := m.palette.kind
	providerAction := m.palette.providerAction
	m.palette = nil

	switch kind {
	case paletteModels:
		selection := row.Value
		if selection == "" {
			selection = row.Word
		}
		return m.cmdModel(selection)
	case paletteProviders:
		if providerAction == "" && (row.Word == "login" || row.Word == "logout") {
			return m.openProviderPalette(row.Word)
		}
		return m.cmdProvider(strings.TrimSpace(providerAction + " " + row.Word))
	case paletteCommands:
		if entry, ok := slashIndex[row.Word]; ok {
			switch entry.name {
			case "model":
				return m.openModelPalette()
			case "provider":
				return m.openProviderPalette("")
			}
			if entry.isAgent && entry.modeSwitch {
				m.inputModeCommand = entry.name
				return nil
			}
		}
		m.input = "/" + row.Word + " "
		m.inputCursor = utf8.RuneCountInString(m.input)
		if row.RequiresArg {
			m.compDismissed = false
			m.compSelected = 0
			return nil
		}
		return m.submitInput()
	default:
		return nil
	}
}

// maybeOpenInputPalette promotes picker-backed slash-command scaffolds into
// the same independent overlay used by their keyboard shortcuts. It runs
// after input editing, so typing or completing the trailing space never
// exposes a second argument-selection UI.
func (m *model) maybeOpenInputPalette() (tea.Cmd, bool) {
	if m.palette != nil || m.inResponse() || m.bashRunning ||
		m.inputCursor != utf8.RuneCountInString(m.input) || !strings.HasSuffix(m.input, " ") {
		return nil, false
	}

	var open func() tea.Cmd
	switch strings.TrimSpace(m.input) {
	case "/model":
		open = m.openModelPalette
	case "/provider":
		open = func() tea.Cmd { return m.openProviderPalette("") }
	case "/provider login":
		open = func() tea.Cmd { return m.openProviderPalette("login") }
	case "/provider logout":
		open = func() tea.Cmd { return m.openProviderPalette("logout") }
	default:
		return nil, false
	}

	m.clearInput()
	return open(), true
}

// paletteOverlayWidth returns the box width for floating list overlays and
// clamps it to the terminal so narrow windows do not render off-screen.
func paletteOverlayWidth(termWidth int) int {
	if termWidth <= 0 {
		return paletteMinWidth
	}
	w := int(float64(termWidth) * paletteWidthFrac)
	w = max(paletteMinWidth, min(w, paletteMaxWidth))
	return min(w, max(termWidth-2, 4))
}

func (m *model) renderPaletteOverlay() string {
	width := paletteOverlayWidth(m.width)
	rows := m.paletteRows()
	return m.palette.picker.Render(rows, width)
}

func (m *model) compositePaletteOverlay(base string) string {
	rendered := m.renderPaletteOverlay()
	bx, by := centerRect(m.width, m.height, lipgloss.Width(rendered), lipgloss.Height(rendered))

	compositor := lipgloss.NewCompositor(
		lipgloss.NewLayer(base).X(0).Y(0),
		lipgloss.NewLayer(rendered).X(bx).Y(by).Z(1),
	)
	canvas := lipgloss.NewCanvas(m.width, m.height)
	canvas.Compose(compositor)
	return canvas.Render()
}
