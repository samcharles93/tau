package dialog

import (
	"strings"

	"bitbucket.srv.westpac.com.au/m055731/aim/internal/theme"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const modelsID = "model-picker"

// ModelItem is a selectable model entry in the model picker.
type ModelItem struct {
	ID    string
	URL   string
	Ready bool
}

// ModelSelectedAction is emitted when a model is picked.
type ModelSelectedAction struct {
	ID  string
	URL string
}

func (ModelSelectedAction) isDialogAction() {}

// ModelPicker is a dialog for browsing and selecting models.
type ModelPicker struct {
	models   []ModelItem
	filtered []ModelItem
	query    string
	selected int

	normalStyle   lipgloss.Style
	selectedStyle lipgloss.Style
	readyStyle    lipgloss.Style
	notReadyStyle lipgloss.Style
	queryStyle    lipgloss.Style
	dimStyle      lipgloss.Style

	keyMap paletteKeyMap
}

// NewModelPicker creates a model picker with the given models.
func NewModelPicker(models []ModelItem) *ModelPicker {
	mp := &ModelPicker{
		models:   models,
		filtered: models,
		normalStyle: lipgloss.NewStyle().
			PaddingLeft(1),
		selectedStyle: lipgloss.NewStyle().
			PaddingLeft(1).
			Bold(true).
			Foreground(theme.ColorWestpacRed),
		readyStyle: lipgloss.NewStyle().
			Foreground(theme.ColorGreen),
		notReadyStyle: lipgloss.NewStyle().
			Foreground(theme.ColorWestpacRed).
			Faint(true),
		queryStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.ColorNavyBlue),
		dimStyle: lipgloss.NewStyle().
			Faint(true),
		keyMap: paletteKeyMap{
			Up:     key.NewBinding(key.WithKeys("up", "ctrl+p")),
			Down:   key.NewBinding(key.WithKeys("down", "ctrl+n")),
			Select: key.NewBinding(key.WithKeys("enter")),
		},
	}
	return mp
}

// ID implements Dialog.
func (mp *ModelPicker) ID() string {
	return modelsID
}

// Update implements Dialog.
func (mp *ModelPicker) Update(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, mp.keyMap.Up):
		mp.selectPrev()
		return nil
	case key.Matches(msg, mp.keyMap.Down):
		mp.selectNext()
		return nil
	case key.Matches(msg, mp.keyMap.Select):
		if len(mp.filtered) == 0 {
			return nil
		}
		item := mp.filtered[mp.selected]
		if !item.Ready {
			return nil // can't select unavailable models
		}
		return ModelSelectedAction{ID: item.ID, URL: item.URL}
	case msg.Key().Code == tea.KeyBackspace:
		if len(mp.query) > 0 {
			mp.query = mp.query[:len(mp.query)-1]
			mp.applyFilter()
		}
		return nil
	default:
		if msg.Text != "" {
			mp.query += msg.Text
			mp.applyFilter()
		}
		return nil
	}
}

// View implements Dialog.
func (mp *ModelPicker) View(width, height int) string {
	var b strings.Builder

	// Title + filter.
	title := mp.queryStyle.Render("Select Model")
	if mp.query != "" {
		title += " " + mp.queryStyle.Render(mp.query) + "▏"
	} else {
		title += " ▏"
	}
	b.WriteString(title)
	b.WriteByte('\n')

	// List.
	maxItems := max(height-2, 1)

	visible := mp.visibleItems(maxItems)
	for i, item := range visible {
		if i > 0 {
			b.WriteByte('\n')
		}
		focused := i == mp.visibleIndex(maxItems)
		b.WriteString(mp.renderItem(item, focused, width))
	}

	if len(mp.filtered) == 0 {
		b.WriteString(mp.dimStyle.Render("  No matching models"))
	}

	return b.String()
}

func (mp *ModelPicker) applyFilter() {
	if mp.query == "" {
		mp.filtered = mp.models
	} else {
		queryLower := strings.ToLower(mp.query)
		mp.filtered = mp.filtered[:0]
		for _, item := range mp.models {
			if strings.Contains(strings.ToLower(item.ID), queryLower) {
				mp.filtered = append(mp.filtered, item)
			}
		}
	}
	if mp.selected >= len(mp.filtered) {
		mp.selected = max(0, len(mp.filtered)-1)
	}
}

func (mp *ModelPicker) selectPrev() {
	if len(mp.filtered) == 0 {
		return
	}
	mp.selected--
	if mp.selected < 0 {
		mp.selected = len(mp.filtered) - 1
	}
}

func (mp *ModelPicker) selectNext() {
	if len(mp.filtered) == 0 {
		return
	}
	mp.selected++
	if mp.selected >= len(mp.filtered) {
		mp.selected = 0
	}
}

func (mp *ModelPicker) visibleItems(maxItems int) []ModelItem {
	if len(mp.filtered) <= maxItems {
		return mp.filtered
	}
	start := max(mp.selected-maxItems/2, 0)
	end := start + maxItems
	if end > len(mp.filtered) {
		end = len(mp.filtered)
		start = max(0, end-maxItems)
	}
	return mp.filtered[start:end]
}

func (mp *ModelPicker) visibleIndex(maxItems int) int {
	if len(mp.filtered) <= maxItems {
		return mp.selected
	}
	start := max(mp.selected-maxItems/2, 0)
	return mp.selected - start
}

func (mp *ModelPicker) renderItem(item ModelItem, focused bool, width int) string {
	style := mp.normalStyle
	if focused {
		style = mp.selectedStyle
	}

	statusIcon := mp.readyStyle.Render("●")
	if !item.Ready {
		statusIcon = mp.notReadyStyle.Render("○")
	}

	label := style.Render(item.ID)
	line := statusIcon + " " + label

	if !item.Ready && focused {
		line += mp.dimStyle.Render(" (not ready)")
	}

	_ = width // available for future truncation
	return line
}
