package completions

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const maxVisible = 10

// SelectionMsg is sent when a completion is selected.
// Text is the full replacement value to insert into the input.
type SelectionMsg struct {
	Text    string
	Command CommandDef
	IsArg   bool
}

// ClosedMsg is sent when the completions are closed.
type ClosedMsg struct{}

// Completions is a lightweight popup that shows matching slash commands
// and argument-level sub-completions.
type Completions struct {
	open     bool
	query    string
	selected int
	keyMap   KeyMap

	commands       []CommandDef
	argCompletions map[string][]string
	filtered       []completionEntry

	normalStyle   lipgloss.Style
	selectedStyle lipgloss.Style
	descStyle     lipgloss.Style
}

// completionEntry is a display item in the filtered list.
type completionEntry struct {
	label       string
	description string
	replacement string
	command     CommandDef
	isArg       bool
}

// New creates a new completions component with the given styles.
func New(normalStyle, selectedStyle, descStyle lipgloss.Style) *Completions {
	return &Completions{
		keyMap:         DefaultKeyMap(),
		normalStyle:    normalStyle,
		selectedStyle:  selectedStyle,
		descStyle:      descStyle,
		argCompletions: make(map[string][]string),
	}
}

// SetCommands configures the available commands for completion.
func (c *Completions) SetCommands(commands []CommandDef) {
	c.commands = commands
}

// SetArgumentCompletions registers a list of valid argument values for a command.
func (c *Completions) SetArgumentCompletions(command string, values []string) {
	c.argCompletions[command] = values
}

// IsOpen returns whether the completions popup is visible.
func (c *Completions) IsOpen() bool {
	return c.open
}

// Selected returns the currently highlighted entry, if any.
func (c *Completions) Selected() (CommandDef, bool) {
	if !c.open || len(c.filtered) == 0 {
		return CommandDef{}, false
	}
	if c.selected < 0 || c.selected >= len(c.filtered) {
		return CommandDef{}, false
	}
	return c.filtered[c.selected].command, true
}

// Open shows the popup and applies the given filter query.
func (c *Completions) Open(query string) {
	c.open = true
	c.applyCommandFilter(query)
}

// Close hides the popup.
func (c *Completions) Close() {
	c.open = false
	c.query = ""
	c.selected = 0
	c.filtered = nil
}

// Sync updates the popup state from the current input text.
// It derives command vs argument mode from the input structure.
func (c *Completions) Sync(input string) {
	// Only trim leading whitespace to preserve trailing space detection.
	trimmed := strings.TrimLeft(input, " \t")

	if !strings.HasPrefix(trimmed, "/") {
		if c.open {
			c.Close()
		}
		return
	}

	// Split into command token and the rest.
	parts := strings.SplitN(trimmed, " ", 2)
	cmdToken := parts[0] // e.g. "/model"

	if len(parts) == 2 {
		// There's a space after the command — check for argument completions.
		argValues, hasArgs := c.argCompletions[strings.ToLower(cmdToken)]
		if !hasArgs {
			// Command doesn't have registered arg completions (e.g. /system).
			if c.open {
				c.Close()
			}
			return
		}
		argQuery := parts[1]
		c.open = true
		c.applyArgFilter(cmdToken, argQuery, argValues)
		return
	}

	// No space yet — command-level completions.
	query := strings.TrimPrefix(trimmed, "/")
	if !c.open {
		c.Open(query)
	} else {
		c.applyCommandFilter(query)
	}
}

// Update handles key events when the popup is open.
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
		entry := c.filtered[c.selected]
		c.Close()
		return SelectionMsg{
			Text:    entry.replacement,
			Command: entry.command,
			IsArg:   entry.isArg,
		}, true

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

	for i, entry := range visible {
		if i > 0 {
			b.WriteByte('\n')
		}
		focused := i == c.selected
		if c.selected >= maxVisible {
			focused = false
		}
		line := c.renderLine(entry, focused, width)
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

func (c *Completions) applyCommandFilter(query string) {
	newQuery := "cmd:" + query
	if newQuery == c.query && c.filtered != nil {
		return
	}
	c.query = newQuery

	queryLower := strings.ToLower(query)
	c.filtered = c.filtered[:0]
	for _, cmd := range c.commands {
		nameLower := strings.ToLower(strings.TrimPrefix(cmd.Name, "/"))
		if query == "" || strings.HasPrefix(nameLower, queryLower) || strings.Contains(nameLower, queryLower) {
			c.filtered = append(c.filtered, completionEntry{
				label:       cmd.Name,
				description: cmd.Description,
				replacement: cmd.Name,
				command:     cmd,
				isArg:       false,
			})
		}
	}

	c.clampSelection()
}

func (c *Completions) applyArgFilter(cmdToken, argQuery string, values []string) {
	newQuery := "arg:" + cmdToken + ":" + argQuery
	if newQuery == c.query && c.filtered != nil {
		return
	}
	c.query = newQuery

	queryLower := strings.ToLower(argQuery)
	cmd := c.findCommand(cmdToken)

	c.filtered = c.filtered[:0]
	for _, val := range values {
		valLower := strings.ToLower(val)
		if argQuery == "" || strings.HasPrefix(valLower, queryLower) || strings.Contains(valLower, queryLower) {
			c.filtered = append(c.filtered, completionEntry{
				label:       val,
				description: "",
				replacement: cmdToken + " " + val,
				command:     cmd,
				isArg:       true,
			})
		}
	}

	c.clampSelection()
}

func (c *Completions) findCommand(name string) CommandDef {
	nameLower := strings.ToLower(name)
	for _, cmd := range c.commands {
		if strings.ToLower(cmd.Name) == nameLower {
			return cmd
		}
	}
	return CommandDef{Name: name}
}

func (c *Completions) clampSelection() {
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

func (c *Completions) visibleItems() []completionEntry {
	if len(c.filtered) <= maxVisible {
		return c.filtered
	}
	return c.filtered[:maxVisible]
}

func (c *Completions) renderLine(entry completionEntry, focused bool, width int) string {
	style := c.normalStyle
	if focused {
		style = c.selectedStyle
	}

	text := entry.label
	if entry.description != "" {
		text += "  " + c.descStyle.Render(entry.description)
	}

	return style.Width(width).Render(text)
}

