// Package theme provides Tau's semantic color palette and styling constants.
//
// All color values live here so the rest of the codebase imports them rather
// than repeating hex literals or RGB triples.
package theme

import "github.com/samcharles93/tau/pkg/taui/termkit"

// ToolStatus describes the background and foreground colors for one of the
// three tool-lifecycle states shown in the inline TUI.
type ToolStatus struct {
	BG termkit.Color
	FG termkit.Color
}

var (
	// ToolRunning is the warm peach state shown while a tool is executing.
	ToolRunning = ToolStatus{
		BG: termkit.Color{252, 214, 187},
		FG: termkit.Color{124, 11, 11},
	}

	// ToolSuccess is the dark green state shown when a tool completes cleanly.
	// The background matches the requested terminal color #0e200e.
	ToolSuccess = ToolStatus{
		BG: termkit.Color{14, 32, 14},
		FG: termkit.Color{140, 220, 140},
	}

	// ToolFailed is the dark red state shown when a tool errors out.
	// The background matches the requested terminal color #300b0b.
	ToolFailed = ToolStatus{
		BG: termkit.Color{48, 11, 11},
		FG: termkit.Color{255, 160, 160},
	}
)
