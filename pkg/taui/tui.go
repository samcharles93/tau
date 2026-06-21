// Package tui provides a minimal component-based terminal UI framework,
// ported from Pi's TUI (packages/tui/src/). It supports differential
// rendering, focus management, overlays, and a growing set of components.
package taui

// Component is the interface all UI components must implement.
type Component interface {
	// Render returns lines for the given viewport width.
	Render(width int) []string

	// Invalidate clears any cached rendering state.
	Invalidate()
}

// InputHandler is an optional interface for components that handle keyboard input.
// When the focused component implements InputHandler, the TUI forwards a single
// discrete key sequence to it. Return true if the input was consumed.
type InputHandler interface {
	HandleInput(data string) bool
}

// PasteHandler is an optional interface for components that want bracketed-paste
// payloads delivered atomically, separate from individual keypresses. The
// content has its \x1b[200~ / \x1b[201~ markers stripped. Return true if the
// paste was consumed.
type PasteHandler interface {
	HandlePaste(content string) bool
}

// Focusable is implemented by components that can receive a hardware cursor.
type Focusable interface {
	Component
	// Focused returns whether the component currently has focus.
	Focused() bool
	// SetFocused sets the focus state.
	SetFocused(focused bool)
}

// Container holds child components and delegates rendering.
type Container struct {
	Children []Component
}

// AddChild appends a component.
func (c *Container) AddChild(child Component) {
	c.Children = append(c.Children, child)
}

// RemoveChild removes a component by reference.
func (c *Container) RemoveChild(child Component) {
	for i, ch := range c.Children {
		if ch == child {
			c.Children = append(c.Children[:i], c.Children[i+1:]...)
			return
		}
	}
}

// Clear removes all children.
func (c *Container) Clear() {
	c.Children = nil
}

// Invalidate clears caches in all children.
func (c *Container) Invalidate() {
	for _, child := range c.Children {
		child.Invalidate()
	}
}

// Render renders all children sequentially.
func (c *Container) Render(width int) []string {
	var lines []string
	for _, child := range c.Children {
		lines = append(lines, child.Render(width)...)
	}
	return lines
}
