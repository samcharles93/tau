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

	// SteeringFG is the navy blue foreground used for the steering indicator
	// in the status line when the user interrupts the agent mid-turn.
	SteeringFG = termkit.Color{100, 130, 220}

	// CommandFG is the accent colour used to echo a submitted slash command
	// into scrollback, so it reads visually distinct from a plain bold chat
	// prompt.
	CommandFG = termkit.Color{120, 170, 255}

	// BashFG is the accent colour for "!" bash mode — both the leading
	// trigger character and the "Shell" input-mode divider — xterm 256
	// color 209.
	BashFG = termkit.Xterm256(209)

	// BashExcludedFG is the muted variant used for "!!" bash commands, which
	// run the same way but are excluded from the LLM's context — the dimmer
	// tone signals "local only." Hand-tuned as a desaturated variant of
	// BashFG rather than another palette index, since there's no single
	// xterm-256 entry that reads as "dim 209."
	BashExcludedFG = termkit.Color{130, 90, 70}

	// SkillRunning is the dark lilac state shown while the Skill tool is executing,
	// so a skill load is visually distinct from a regular tool call.
	SkillRunning = ToolStatus{
		BG: termkit.Color{68, 50, 100},
		FG: termkit.Color{215, 195, 245},
	}

	// SkillSuccess is the dark lilac state shown when the Skill tool activates
	// a skill cleanly.
	SkillSuccess = ToolStatus{
		BG: termkit.Color{68, 50, 100},
		FG: termkit.Color{215, 195, 245},
	}

	// SkillFailed is the dark mauve state shown when the Skill tool errors
	// (e.g. unknown skill name).
	SkillFailed = ToolStatus{
		BG: termkit.Color{90, 40, 70},
		FG: termkit.Color{255, 190, 210},
	}

	// ToneInfo, ToneSuccess, ToneWarn, ToneError, and ToneMuted are the
	// foreground colors for plugin-rendered widgets' semantic Style.Tone,
	// resolved host-side so panels stay visually consistent with the rest of
	// the theme regardless of what a plugin author picks.
	ToneInfo    = termkit.Color{120, 170, 255}
	ToneSuccess = termkit.Color{140, 220, 140}
	ToneWarn    = termkit.Color{255, 200, 120}
	ToneError   = termkit.Color{255, 160, 160}
	ToneMuted   = termkit.Color{128, 134, 150}
)
