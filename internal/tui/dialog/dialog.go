package dialog

import (
	"strings"

	"bitbucket.srv.westpac.com.au/m055731/aim/internal/theme"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Action is the result of handling a message in a dialog.
type Action interface{ isDialogAction() }

// CloseAction signals that the dialog should be closed.
type CloseAction struct{}

func (CloseAction) isDialogAction() {}

// SelectAction signals a user selection from the dialog list.
type SelectAction struct {
	ID    string
	Label string
	Value any
}

func (SelectAction) isDialogAction() {}

// Dialog is a component that can be displayed as an overlay on the TUI.
type Dialog interface {
	// ID returns a unique identifier for this dialog.
	ID() string
	// Update handles a message and returns an optional action.
	Update(msg tea.KeyPressMsg) Action
	// View renders the dialog content within the given width/height.
	View(width, height int) string
}

// Overlay manages a stack of active dialogs.
type Overlay struct {
	dialogs []Dialog
	style   OverlayStyle
}

// OverlayStyle holds styling for the dialog overlay.
type OverlayStyle struct {
	Border   lipgloss.Style
	Backdrop lipgloss.Style
	Title    lipgloss.Style
	CloseKey key.Binding
}

// DefaultOverlayStyle returns a default overlay style.
func DefaultOverlayStyle() OverlayStyle {
	return OverlayStyle{
		Border: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(theme.ColorPurple).
			Padding(0, 1),
		Backdrop: lipgloss.NewStyle().
			Faint(true),
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.ColorDarkNavy),
		CloseKey: key.NewBinding(
			key.WithKeys("esc", "alt+esc"),
			key.WithHelp("esc", "close"),
		),
	}
}

// NewOverlay creates a new dialog overlay.
func NewOverlay() *Overlay {
	return &Overlay{
		style: DefaultOverlayStyle(),
	}
}

// HasDialogs returns true if there are active dialogs.
func (o *Overlay) HasDialogs() bool {
	return len(o.dialogs) > 0
}

// Open pushes a dialog onto the stack.
func (o *Overlay) Open(d Dialog) {
	// Don't open duplicates.
	for _, existing := range o.dialogs {
		if existing.ID() == d.ID() {
			return
		}
	}
	o.dialogs = append(o.dialogs, d)
}

// Close removes the dialog with the given ID.
func (o *Overlay) Close(id string) {
	for i, d := range o.dialogs {
		if d.ID() == id {
			o.dialogs = append(o.dialogs[:i], o.dialogs[i+1:]...)
			return
		}
	}
}

// CloseFront removes the top-most dialog.
func (o *Overlay) CloseFront() {
	if len(o.dialogs) == 0 {
		return
	}
	o.dialogs = o.dialogs[:len(o.dialogs)-1]
}

// Front returns the top-most dialog, or nil.
func (o *Overlay) Front() Dialog {
	if len(o.dialogs) == 0 {
		return nil
	}
	return o.dialogs[len(o.dialogs)-1]
}

// Update passes a key event to the front dialog and returns the resulting action.
func (o *Overlay) Update(msg tea.KeyPressMsg) Action {
	front := o.Front()
	if front == nil {
		return nil
	}

	// Global close binding.
	if key.Matches(msg, o.style.CloseKey) {
		o.CloseFront()
		return CloseAction{}
	}

	return front.Update(msg)
}

// View renders the front dialog centered within the given area.
func (o *Overlay) View(width, height int) string {
	front := o.Front()
	if front == nil {
		return ""
	}

	// Reserve space for border.
	innerW := min(width-4, 60)
	innerH := min(height-4, 20)
	if innerW < 10 || innerH < 3 {
		return ""
	}

	content := front.View(innerW, innerH)
	bordered := o.style.Border.Width(innerW).Render(content)

	// Center horizontally.
	borderedW := lipgloss.Width(bordered)
	pad := max((width-borderedW)/2, 0)
	lines := strings.Split(bordered, "\n")
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString(line)
	}

	return b.String()
}
