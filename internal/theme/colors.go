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

// ToolBoxStyle combines background, foreground, and border colors for a
// tool or skill's visual container in the TUI.
type ToolBoxStyle struct {
	BG     termkit.Color
	FG     termkit.Color
	Border termkit.Color
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

	// ToolPending is a muted grey-blue state for tools that have been
	// announced but haven't started executing yet.
	ToolPending = ToolStatus{
		BG: termkit.Color{64, 64, 72},
		FG: termkit.Color{160, 165, 175},
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

	// SkillSuccess is the dark teal-green state shown when the Skill tool
	// activates a skill cleanly, distinct from the lilac running state.
	SkillSuccess = ToolStatus{
		BG: termkit.Color{20, 60, 40},
		FG: termkit.Color{160, 235, 185},
	}

	// SkillFailed is the dark mauve state shown when the Skill tool errors
	// (e.g. unknown skill name).
	SkillFailed = ToolStatus{
		BG: termkit.Color{90, 40, 70},
		FG: termkit.Color{255, 190, 210},
	}

	// Tool box border colors — lighter companion colors for tool/skill
	// container borders, chosen so borders read clearly against the
	// background without overwhelming the foreground text.
	ToolRunningBorder  = termkit.Color{230, 170, 130}
	ToolSuccessBorder  = termkit.Color{100, 200, 100}
	ToolFailedBorder   = termkit.Color{240, 120, 120}
	ToolPendingBorder  = termkit.Color{130, 135, 145}
	SkillRunningBorder = termkit.Color{180, 160, 210}
	SkillSuccessBorder = termkit.Color{120, 200, 160}
	SkillFailedBorder  = termkit.Color{240, 140, 180}

	// ShimmerBase and ShimmerHighlight are the two stops of the "current"
	// gradient swept across the live working indicator (see
	// internal/tui2/animation.go). ShimmerBase is tau's brand violet — the
	// resting colour of the text — and ShimmerHighlight is the electric cyan
	// band of light that travels through it, tying the motion to the same cyan
	// accent used for the input caret. The pair reads as "computation flowing
	// through the τ mark" rather than a decorative rainbow.
	ShimmerBase      = termkit.Color{123, 47, 190}
	ShimmerHighlight = termkit.Color{127, 219, 255}

	// TonePrimary, ToneBody, ToneInfo, ToneSuccess, ToneWarn, ToneError, and
	// ToneMuted are foreground colors for app text and plugin-rendered
	// widgets' semantic Style.Tone, resolved host-side so panels stay visually
	// consistent with the rest of the theme regardless of what a plugin author
	// picks.
	TonePrimary = termkit.ColorWhite
	ToneBody    = termkit.Color{204, 204, 204}
	ToneInfo    = termkit.Color{120, 170, 255}
	ToneSuccess = termkit.Color{140, 220, 140}
	ToneWarn    = termkit.Color{255, 200, 120}
	ToneError   = termkit.Color{255, 160, 160}
	ToneMuted   = termkit.Color{128, 134, 150}
)

// ToolBox returns a ToolBoxStyle combining the background and foreground
// colors of the supplied ToolStatus with a lighter companion border color.
func ToolBox(ts ToolStatus, borderColor termkit.Color) ToolBoxStyle {
	return ToolBoxStyle{
		BG:     ts.BG,
		FG:     ts.FG,
		Border: borderColor,
	}
}
