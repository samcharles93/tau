// Package components provides reusable go-tui GSX components and a debug
// registry for isolated component preview via /debug components.
package components

import gt "github.com/grindlemire/go-tui"

// SelectOption is one entry in a Select component.
type SelectOption struct {
	Value string
	Label string
}

// ListItem is one entry in a List component.
type ListItem struct {
	ID          string
	Label       string
	Description string
	Disabled    bool
}

// DebugFactory creates a component and its debug controls for the
// component preview system (/debug components <name>).
type DebugFactory func(app *gt.App) (gt.Component, []DebugControl)

// DebugControl describes one interactive state knob shown in the
// debug view's control panel.
type DebugControl struct {
	Label   string
	Type    string // "toggle", "cycle", "text"
	Options []string
	State   *gt.State[string]
}

// TreeItem is one entry in a Tree component.
type TreeItem struct {
	ID          string
	Label       string
	Description string
	Depth       int
	Expanded    bool
	HasChildren bool
	Disabled    bool
}
