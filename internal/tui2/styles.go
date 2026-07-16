package tui2

import (
	"fmt"
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// themeHex converts a termkit.Color RGB triple to a lipgloss-compatible color.
func themeHex(c termkit.Color) color.Color {
	return lipgloss.Color(fmt.Sprintf("#%02X%02X%02X", c[0], c[1], c[2]))
}

var (
	// Semantic colors sourced from internal/theme.
	//
	// Chat message bodies (user/assistant/streaming) deliberately set no
	// Foreground at all: that lets the text inherit the terminal's own
	// default foreground instead of forcing a brand color across a large
	// block of content, matching Tau's rule of never overriding the user's
	// terminal theme. Only the small "⏎ " user-message glyph gets an
	// explicit accent, via userGlyphStyle below.
	inputColor = themeHex(theme.ShimmerHighlight)

	userGlyphStyle = lipgloss.NewStyle().Foreground(themeHex(theme.AccentColor)).Bold(true)
	userStyle      = lipgloss.NewStyle()
	assistantStyle = lipgloss.NewStyle()
	// reasoningLabelStyle colors the "│ " bar prefixed to every line of a
	// reasoning block - the one deliberate, sparing use of Warm Ochre in
	// reasoning output, giving the whole block its own visual lane next to
	// the (unstyled) user/assistant text around it. reasoningStyle is the
	// body: an explicit muted grey (theme.ToneMuted) is used instead of
	// relying on ANSI Faint alone, since several terminals render faint as
	// visually identical to normal weight - pairing it with Faint still
	// helps on terminals that do support it, and Italic adds a second cue
	// so the block stays clearly secondary to the un-styled, full-brightness
	// final answer even in terminals with little contrast headroom.
	reasoningLabelStyle = lipgloss.NewStyle().Foreground(themeHex(theme.AccentColor))
	reasoningStyle      = lipgloss.NewStyle().Foreground(themeHex(theme.ToneMuted)).Faint(true).Italic(true)
	streamStyle         = lipgloss.NewStyle()
	inputStyle          = lipgloss.NewStyle().Foreground(inputColor)

	userContinuationStyle      = lipgloss.NewStyle().PaddingLeft(6)
	assistantContinuationStyle = lipgloss.NewStyle().PaddingLeft(6)
	continuationStyle          = lipgloss.NewStyle().Faint(true).PaddingLeft(6)

	// inputCursorStyle is the block-cursor background - matches the default
	// mid-grey pkg/taui/lineinput.go uses (\x1b[48;2;128;134;150m).
	// #808696 = R=128 G=134 B=150 = theme.ToneMuted.
	inputCursorStyle = lipgloss.NewStyle().Background(themeHex(theme.ToneMuted))

	// Prompt / completion styles.
	promptTitleStyle     = lipgloss.NewStyle().Bold(true).Foreground(themeHex(theme.ShimmerHighlight))
	promptTextStyle      = lipgloss.NewStyle().Foreground(themeHex(theme.TonePrimary))
	promptHintStyle      = lipgloss.NewStyle().Faint(true).Italic(true)
	promptHighlightStyle = lipgloss.NewStyle().Bold(true).Underline(true)
	compTitleStyle       = lipgloss.NewStyle().Faint(true).Bold(true)
	compItemStyle        = lipgloss.NewStyle().Foreground(themeHex(theme.ToneBody))
	compSelectedStyle    = lipgloss.NewStyle().Foreground(themeHex(theme.AccentColor)).Bold(true)
	compHighlightStyle   = lipgloss.NewStyle().Foreground(themeHex(theme.TonePrimary)).Bold(true).Underline(true)
	compDescStyle        = lipgloss.NewStyle().Faint(true)
	compMoreStyle        = lipgloss.NewStyle().Faint(true).Italic(true)
	panelStyle           = lipgloss.NewStyle().Foreground(themeHex(theme.AccentColor))

	// streamCursorStyle marks the live streaming cursor - Warm Ochre, the
	// same marker color already used for every other small interactive
	// glyph in the UI (userGlyphStyle, reasoningLabelStyle), at a mid-tone
	// luminance legible against both light and dark terminal backgrounds,
	// unlike a saturated color that could wash out or vanish on one extreme.
	streamCursorStyle = lipgloss.NewStyle().Foreground(themeHex(theme.AccentColor))

	// Muted trailing metadata - the per-tool elapsed suffix. Faint (terminal-
	// native dim) rather than a fixed color, so the tool name stays the focus
	// and the timing reads as ambient against whatever foreground the user's
	// terminal actually has.
	toolMetaStyle = lipgloss.NewStyle().Faint(true)

	// Tool status styles - per-state foreground colors for tool call rows.
	toolRunningStyle = lipgloss.NewStyle().Foreground(themeHex(theme.ToolRunning.FG))
	toolSuccessStyle = lipgloss.NewStyle().Foreground(themeHex(theme.SuccessColor))
	toolErrorStyle   = lipgloss.NewStyle().Foreground(themeHex(theme.ErrorColor))
	// Skill tool gets lilac variants - same as theme.SkillRunning/SkillSuccess/SkillFailed.
	skillRunningStyle = lipgloss.NewStyle().Foreground(themeHex(theme.SkillRunning.FG))
	skillSuccessStyle = lipgloss.NewStyle().Foreground(themeHex(theme.SkillSuccess.FG))
	skillFailedStyle  = lipgloss.NewStyle().Foreground(themeHex(theme.SkillFailed.FG))

	// Styled input prompt and divider styles.
	inputPromptStyle      = lipgloss.NewStyle().Foreground(themeHex(theme.AccentColor)).Bold(true)
	inputSteerPromptStyle = lipgloss.NewStyle().Foreground(themeHex(theme.ToneWarn)).Bold(true)
	inputPlaceholderStyle = lipgloss.NewStyle().Faint(true).Italic(true)
	inputBoxStyle         = lipgloss.NewStyle().Foreground(themeHex(theme.SecondaryColor))
	separatorStyle        = lipgloss.NewStyle().Foreground(themeHex(theme.SecondaryColor))

	// Phase 1: tool box styles - bordered boxes for each lifecycle state,
	// foreground/border only (no Background fill): a tool box can span many
	// lines of live tail output or expanded result content, and painting a
	// saturated background across that much text is exactly the "large
	// block of text" coloring the palette rules out. Same rationale as
	// contextMenuStyle below.
	toolBoxRunningStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(themeHex(theme.ToolRunning.FG)).
				Foreground(themeHex(theme.ToolRunning.FG)).
				Padding(0, 1)
	toolBoxSuccessStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(themeHex(theme.SuccessColor)).
				Foreground(themeHex(theme.SuccessColor)).
				Padding(0, 1)
	toolBoxErrorStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(themeHex(theme.ErrorColor)).
				Foreground(themeHex(theme.ErrorColor)).
				Padding(0, 1)
	// Skill tool gets lilac variants.
	toolBoxSkillRunningStyle = lipgloss.NewStyle().
					Border(lipgloss.RoundedBorder()).
					BorderForeground(themeHex(theme.SkillRunning.FG)).
					Foreground(themeHex(theme.SkillRunning.FG)).
					Padding(0, 1)
	toolBoxSkillSuccessStyle = lipgloss.NewStyle().
					Border(lipgloss.RoundedBorder()).
					BorderForeground(themeHex(theme.SkillSuccess.FG)).
					Foreground(themeHex(theme.SkillSuccess.FG)).
					Padding(0, 1)
	toolBoxSkillFailedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(themeHex(theme.SkillFailed.FG)).
				Foreground(themeHex(theme.SkillFailed.FG)).
				Padding(0, 1)

	// Context menu - a floating overlay composited on top of arbitrary
	// already-rendered content (see compositeContextMenu). No Background():
	// a terminal's "rounded" border is only rounded in the corner glyphs -
	// the cell grid underneath is always a hard rectangle, so an explicit
	// fill just makes that rectangle's edge obvious as a harsh square
	// around the border. Foreground-only (like the prompt/completions
	// family) reads as a cleaner floating outline instead.
	contextMenuStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(themeHex(theme.SecondaryColor)).
				Foreground(themeHex(theme.ToneBody)).
				Padding(0, 1)
	// Selected-item highlight follows the completions dropdown's convention:
	// foreground color + bold, no background swap.
	contextMenuSelectedStyle = lipgloss.NewStyle().Foreground(themeHex(theme.AccentColor)).Bold(true)

	// toolBoxExpandedStyle is the style for an expanded tool box (subtle border).
	toolBoxExpandedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder(), false, false, false, true).
				BorderForeground(themeHex(theme.SecondaryColor)).
				Padding(0, 1)

	// toolGroupBoxStyle wraps a live multi-tool-call batch (renderToolGroup)
	// - neutral (not status-colored, since it holds a mix of statuses).
	toolGroupBoxStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(themeHex(theme.SecondaryColor))
	toolGroupHeaderStyle   = lipgloss.NewStyle().Faint(true).Bold(true)
	toolFocusMarkerStyle   = lipgloss.NewStyle().Foreground(themeHex(theme.AccentColor)).Bold(true)
	toolGroupSummaryStyle  = lipgloss.NewStyle().Faint(true)
	toolGroupOverflowStyle = lipgloss.NewStyle().Faint(true).Italic(true)
)
