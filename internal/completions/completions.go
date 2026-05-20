package completions

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const maxVisible = 10

// SelectionMsg is sent when a completion is selected.
type SelectionMsg struct {
	Command CommandDef
}

// ClosedMsg is sent when the completions are closed.
type ClosedMsg struct{}

// Completions is a lightweight popup that shows matching slash commands.
type Completions struct {
	open     bool
	query    string
	selected int
	keyMap   KeyMap

	commands []CommandDef
	filtered []CommandDef

	normalStyle   lipgloss.Style
	selectedStyle lipgloss.Style
	descStyle     lipgloss.Style
}

// New creates a new completions component with the given styles.
func New(normalStyle, selectedStyle, descStyle lipgloss.Style) *Completions {
	return &Completions{
		keyMap:        DefaultKeyMap(),
		normalStyle:   normalStyle,
		selectedStyle: selectedStyle,
		descStyle:     descStyle,
	}
}

// SetCommands configures the available commands for completion.
func (c *Completions) SetCommands(commands []CommandDef) {
	c.commands = commands
}

// IsOpen returns whether the completions popup is visible.
func (c *Completions) IsOpen() bool {
	return c.open
}

// Selected returns the currently highlighted command, if any.
func (c *Completions) Selected() (CommandDef, bool) {
	if !c.open || len(c.filtered) == 0 {
		return CommandDef{}, false
	}
	if c.selected < 0 || c.selected >= len(c.filtered) {
		return CommandDef{}, false
	}
	return c.filtered[c.selected], true
}

// Open shows the popup and applies the given filter query.
func (c *Completions) Open(query string) {
	c.open = true
	c.applyFilter(query)
}

// Close hides the popup.
func (c *Completions) Close() {
	c.open = false
	c.query = ""
	c.selected = 0
	c.filtered = nil
}

// Sync updates the filter from the current input text.
// If the input starts with "/" it opens/filters; otherwise it closes.
func (c *Completions) Sync(input string) {
	trimmed := strings.TrimSpace(input)

	if !strings.HasPrefix(trimmed, "/") {
		if c.open {
			c.Close()
		}
		return
	}

	// Don't show completions if there's already a space (user is typing args).
	if strings.Contains(trimmed, " ") {
		if c.open {
			c.Close()
		}
		return
	}

	query := strings.TrimPrefix(trimmed, "/")
	if !c.open {
		c.Open(query)
	} else {
		c.applyFilter(query)
	}
}

// Update handles key events when the popup is open.
// Returns a SelectionMsg if a command was chosen, and whether the key was consumed.
func (c *Completions) Update(msg tea.KeyPressMsg) (tea.Msg, bool) {
	if !c.open {
		return nil, false
	}

	switch {
	case key.Matches(msg, c.keyMap.Up):
		c.selectPrev()
		return nil, true

	case key.Matches(msg, c.keyMap.Down):
		c.selectNext()
		return nil, true

	case key.Matches(msg, c.keyMap.Select):
		if len(c.filtered) == 0 {
			return nil, true
		}
		cmd := c.filtered[c.selected]
		c.Close()
		return SelectionMsg{Command: cmd}, true

	case key.Matches(msg, c.keyMap.Cancel):
		c.Close()
		return ClosedMsg{}, true
	}

	return nil, false
}

// Render renders the completions popup.
func (c *Completions) Render(width int) string {
	if !c.open || len(c.filtered) == 0 {
		return ""
	}

	visible := c.visibleItems()
	var b strings.Builder

	for i, cmd := range visible {
		if i > 0 {
			b.WriteByte('\n')
		}

		line := c.renderLine(cmd, cmd.Name == c.filtered[c.selected].Name, width)
		b.WriteString(line)
	}

	return b.String()
}

// Height returns the number of visible lines the popup will render.
func (c *Completions) Height() int {
	if !c.open {
		return 0
	}
	return len(c.visibleItems())
}

func (c *Completions) applyFilter(query string) {
	if query == c.query && c.filtered != nil {
		return
	}

	c.query = query
	queryLower := strings.ToLower(query)

	c.filtered = c.filtered[:0]
	for _, cmd := range c.commands {
		nameLower := strings.ToLower(strings.TrimPrefix(cmd.Name, "/"))
		if query == "" || strings.HasPrefix(nameLower, queryLower) || strings.Contains(nameLower, queryLower) {
			c.filtered = append(c.filtered, cmd)
		}
	}

	// Clamp selection.
	if c.selected >= len(c.filtered) {
		c.selected = max(0, len(c.filtered)-1)
	}
}

func (c *Completions) selectPrev() {
	if len(c.filtered) == 0 {
		return
	}
	c.selected--
	if c.selected < 0 {
		c.selected = len(c.filtered) - 1
	}
}

func (c *Completions) selectNext() {
	if len(c.filtered) == 0 {
		return
	}
	c.selected++
	if c.selected >= len(c.filtered) {
		c.selected = 0
	}
}

func (c *Completions) visibleItems() []CommandDef {
	if len(c.filtered) <= maxVisible {
		return c.filtered
	}
	return c.filtered[:maxVisible]
}

func (c *Completions) renderLine(cmd CommandDef, focused bool, width int) string {
	style := c.normalStyle
	if focused {
		style = c.selectedStyle
	}

	text := cmd.Name
	if cmd.Description != "" {
		text += "  " + c.descStyle.Render(cmd.Description)
	}

	return style.Width(width).Render(text)
}
