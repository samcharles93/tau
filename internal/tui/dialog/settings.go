package dialog

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/samcharles93/tau/internal/theme"
)

const settingsID = "settings"

// SettingKind describes the type of a setting entry.
type SettingKind int

const (
	SettingToggle SettingKind = iota
	SettingChoice
)

// SettingEntry represents one configurable setting.
type SettingEntry struct {
	Key         string
	Label       string
	Kind        SettingKind
	Value       string // current display value
	BoolVal     bool   // for toggles
	Choices     []string
	ChoiceIndex int
}

// SettingsChangedAction is emitted when the user confirms settings.
type SettingsChangedAction struct {
	Settings []SettingEntry
}

func (SettingsChangedAction) isDialogAction() {}

// Settings is a dialog for viewing and editing app settings.
type Settings struct {
	entries  []SettingEntry
	selected int

	titleStyle    lipgloss.Style
	normalStyle   lipgloss.Style
	selectedStyle lipgloss.Style
	keyStyle      lipgloss.Style
	valueStyle    lipgloss.Style
	activeStyle   lipgloss.Style
	dimStyle      lipgloss.Style

	keyMap settingsKeyMap
}

type settingsKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Left   key.Binding
	Right  key.Binding
	Toggle key.Binding
	Close  key.Binding
}

// NewSettings creates a settings dialog with the given entries.
func NewSettings(entries []SettingEntry) *Settings {
	return &Settings{
		entries:  entries,
		selected: 0,
		titleStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.ColorDarkNavy),
		normalStyle: lipgloss.NewStyle().
			PaddingLeft(1),
		selectedStyle: lipgloss.NewStyle().
			PaddingLeft(1).
			Bold(true).
			Foreground(theme.ColorRed),
		keyStyle: lipgloss.NewStyle().
			Bold(true),
		valueStyle: lipgloss.NewStyle().
			Foreground(theme.ColorNavyBlue),
		activeStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.ColorGreen),
		dimStyle: lipgloss.NewStyle().
			Faint(true),
		keyMap: settingsKeyMap{
			Up:     key.NewBinding(key.WithKeys("up", "k")),
			Down:   key.NewBinding(key.WithKeys("down", "j")),
			Left:   key.NewBinding(key.WithKeys("left", "h")),
			Right:  key.NewBinding(key.WithKeys("right", "l")),
			Toggle: key.NewBinding(key.WithKeys("enter", " ")),
			Close:  key.NewBinding(key.WithKeys("esc", "q")),
		},
	}
}

// ID implements Dialog.
func (s *Settings) ID() string {
	return settingsID
}

// Update handles key events for the settings dialog.
func (s *Settings) Update(msg tea.KeyPressMsg) Action {
	switch {
	case key.Matches(msg, s.keyMap.Up):
		s.selected--
		if s.selected < 0 {
			s.selected = len(s.entries) - 1
		}
		return nil

	case key.Matches(msg, s.keyMap.Down):
		s.selected++
		if s.selected >= len(s.entries) {
			s.selected = 0
		}
		return nil

	case key.Matches(msg, s.keyMap.Toggle):
		s.toggleOrCycle(1)
		return SettingsChangedAction{Settings: s.entries}

	case key.Matches(msg, s.keyMap.Right):
		s.toggleOrCycle(1)
		return SettingsChangedAction{Settings: s.entries}

	case key.Matches(msg, s.keyMap.Left):
		s.toggleOrCycle(-1)
		return SettingsChangedAction{Settings: s.entries}

	case key.Matches(msg, s.keyMap.Close):
		return CloseAction{}
	}
	return nil
}

func (s *Settings) toggleOrCycle(direction int) {
	if s.selected < 0 || s.selected >= len(s.entries) {
		return
	}
	entry := &s.entries[s.selected]
	switch entry.Kind {
	case SettingToggle:
		entry.BoolVal = !entry.BoolVal
		if entry.BoolVal {
			entry.Value = "on"
		} else {
			entry.Value = "off"
		}
	case SettingChoice:
		if len(entry.Choices) == 0 {
			return
		}
		entry.ChoiceIndex += direction
		if entry.ChoiceIndex < 0 {
			entry.ChoiceIndex = len(entry.Choices) - 1
		}
		if entry.ChoiceIndex >= len(entry.Choices) {
			entry.ChoiceIndex = 0
		}
		entry.Value = entry.Choices[entry.ChoiceIndex]
	}
}

// View renders the settings dialog.
func (s *Settings) View(width, height int) string {
	var b strings.Builder

	title := s.titleStyle.Render("Settings")
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", min(width, lipgloss.Width(title)+10)))
	b.WriteString("\n")

	maxItems := max(height-3, 1)
	visible := s.entries
	if len(visible) > maxItems {
		start := max(s.selected-maxItems/2, 0)
		end := start + maxItems
		if end > len(visible) {
			end = len(visible)
			start = max(0, end-maxItems)
		}
		visible = visible[start:end]
	}

	for i, entry := range visible {
		if i > 0 {
			b.WriteByte('\n')
		}

		globalIdx := i
		if len(s.entries) > maxItems {
			globalIdx = i + max(s.selected-maxItems/2, 0)
		}
		focused := globalIdx == s.selected

		line := s.renderEntry(entry, focused, width)
		b.WriteString(line)
	}

	b.WriteString("\n")
	b.WriteString(s.dimStyle.Render("  ←/→ change • enter toggle • esc close"))

	return b.String()
}

func (s *Settings) renderEntry(entry SettingEntry, focused bool, width int) string {
	style := s.normalStyle
	if focused {
		style = s.selectedStyle
	}

	label := entry.Label
	value := s.formatValue(entry)

	padding := max(width-lipgloss.Width(label)-lipgloss.Width(value)-4, 1)
	line := fmt.Sprintf("%s%s%s", label, strings.Repeat(" ", padding), value)

	return style.Width(width).Render(line)
}

func (s *Settings) formatValue(entry SettingEntry) string {
	switch entry.Kind {
	case SettingToggle:
		if entry.BoolVal {
			return s.activeStyle.Render("● on")
		}
		return s.dimStyle.Render("○ off")
	case SettingChoice:
		if len(entry.Choices) <= 1 {
			return s.valueStyle.Render(entry.Value)
		}
		return s.valueStyle.Render(fmt.Sprintf("◂ %s ▸", entry.Value))
	default:
		return s.valueStyle.Render(entry.Value)
	}
}
