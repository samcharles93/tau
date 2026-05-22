package dialog

import (
	"strings"

	"bitbucket.srv.westpac.com.au/m055731/aim/internal/theme"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const paletteID = "command-palette"

// PaletteItem is a selectable entry in the command palette.
type PaletteItem struct {
	ID          string
	Label       string
	Description string
	Value       any
}

// Palette is a filterable command palette dialog.
type Palette struct {
	title    string
	items    []PaletteItem
	filtered []PaletteItem
	query    string
	selected int

	normalStyle   lipgloss.Style
	selectedStyle lipgloss.Style
	queryStyle    lipgloss.Style
	descStyle     lipgloss.Style

	keyMap paletteKeyMap
}

type paletteKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
}

// NewPalette creates a command palette with the given items.
func NewPalette(title string, items []PaletteItem) *Palette {
	p := &Palette{
		title:    title,
		items:    items,
		filtered: items,
		normalStyle: lipgloss.NewStyle().
			PaddingLeft(1),
		selectedStyle: lipgloss.NewStyle().
			PaddingLeft(1).
			Bold(true).
			Foreground(theme.ColorWestpacRed),
		queryStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.ColorNavyBlue),
		descStyle: lipgloss.NewStyle().
			Faint(true).
			PaddingLeft(1),
		keyMap: paletteKeyMap{
			Up:     key.NewBinding(key.WithKeys("up", "ctrl+p")),
			Down:   key.NewBinding(key.WithKeys("down", "ctrl+n")),
			Select: key.NewBinding(key.WithKeys("enter")),
		},
	}
	return p
}

// ID implements Dialog.
func (p *Palette) ID() string {
	return paletteID
}

// Update implements Dialog.
func (p *Palette) Update(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, p.keyMap.Up):
		p.selectPrev()
		return nil
	case key.Matches(msg, p.keyMap.Down):
		p.selectNext()
		return nil
	case key.Matches(msg, p.keyMap.Select):
		if len(p.filtered) == 0 {
			return nil
		}
		item := p.filtered[p.selected]
		return SelectAction{ID: item.ID, Label: item.Label, Value: item.Value}
	case msg.Key().Code == tea.KeyBackspace:
		if len(p.query) > 0 {
			p.query = p.query[:len(p.query)-1]
			p.applyFilter()
		}
		return nil
	default:
		// Printable character — append to query.
		if msg.Text != "" {
			p.query += msg.Text
			p.applyFilter()
		}
		return nil
	}
}

// View implements Dialog.
func (p *Palette) View(width, height int) string {
	var b strings.Builder

	// Title + query.
	titleLine := p.queryStyle.Render(p.title)
	if p.query != "" {
		titleLine += " " + p.queryStyle.Render(p.query) + "▏"
	} else {
		titleLine += " ▏"
	}
	b.WriteString(titleLine)
	b.WriteByte('\n')

	// List items.
	maxItems := max(height-2, 1)

	visible := p.visibleItems(maxItems)
	for i, item := range visible {
		if i > 0 {
			b.WriteByte('\n')
		}
		line := p.renderItem(item, i == p.visibleIndex(maxItems), width)
		b.WriteString(line)
	}

	if len(p.filtered) == 0 {
		b.WriteString(p.descStyle.Render("  No matches"))
	}

	return b.String()
}

func (p *Palette) applyFilter() {
	if p.query == "" {
		p.filtered = p.items
	} else {
		queryLower := strings.ToLower(p.query)
		p.filtered = p.filtered[:0]
		for _, item := range p.items {
			labelLower := strings.ToLower(item.Label)
			descLower := strings.ToLower(item.Description)
			if strings.Contains(labelLower, queryLower) || strings.Contains(descLower, queryLower) {
				p.filtered = append(p.filtered, item)
			}
		}
	}
	if p.selected >= len(p.filtered) {
		p.selected = max(0, len(p.filtered)-1)
	}
}

func (p *Palette) selectPrev() {
	if len(p.filtered) == 0 {
		return
	}
	p.selected--
	if p.selected < 0 {
		p.selected = len(p.filtered) - 1
	}
}

func (p *Palette) selectNext() {
	if len(p.filtered) == 0 {
		return
	}
	p.selected++
	if p.selected >= len(p.filtered) {
		p.selected = 0
	}
}

func (p *Palette) visibleItems(maxItems int) []PaletteItem {
	if len(p.filtered) <= maxItems {
		return p.filtered
	}
	start := max(p.selected-maxItems/2, 0)
	end := start + maxItems
	if end > len(p.filtered) {
		end = len(p.filtered)
		start = max(0, end-maxItems)
	}
	return p.filtered[start:end]
}

func (p *Palette) visibleIndex(maxItems int) int {
	if len(p.filtered) <= maxItems {
		return p.selected
	}
	start := max(p.selected-maxItems/2, 0)
	return p.selected - start
}

func (p *Palette) renderItem(item PaletteItem, focused bool, width int) string {
	style := p.normalStyle
	if focused {
		style = p.selectedStyle
	}

	label := style.Render(item.Label)
	if item.Description != "" {
		remaining := width - lipgloss.Width(label) - 2
		if remaining > 3 {
			desc := item.Description
			if len(desc) > remaining {
				desc = desc[:remaining-1] + "…"
			}
			label += p.descStyle.Render(desc)
		}
	}
	return label
}
