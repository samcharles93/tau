package tui

import "charm.land/lipgloss/v2"

// Westpac NOW theme palette.
var (
	colorDarkNavy   = lipgloss.Color("#181B25")
	colorWhite      = lipgloss.Color("#FFFFFF")
	colorWestpacRed = lipgloss.Color("#DA1710")
	colorNavyBlue   = lipgloss.Color("#1F1B4F")
	colorPurple     = lipgloss.Color("#9819D7")
	colorLightGray  = lipgloss.Color("#E8E8ED")
	colorDimGray    = lipgloss.Color("#a5a5b1")
)

// theme holds pre-computed styles derived from the Westpac NOW colour palette.
type theme struct {
	// Header bar at the top of the screen.
	Header lipgloss.Style

	// User message label.
	UserLabel lipgloss.Style

	// Assistant message label.
	AssistantLabel lipgloss.Style

	// Streaming text (plain, no markdown rendering yet).
	StreamingText lipgloss.Style

	// Error messages.
	Error lipgloss.Style

	// Status bar at the bottom.
	StatusBar lipgloss.Style

	// Help text (slash commands hint).
	Help lipgloss.Style

	// Spinner styling.
	Spinner lipgloss.Style

	// Dim text for secondary information.
	Dim lipgloss.Style

	// Completion popup: normal item.
	CompletionNormal lipgloss.Style

	// Completion popup: selected/highlighted item.
	CompletionSelected lipgloss.Style
}

func newTheme() theme {
	return theme{
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWhite).
			Background(colorNavyBlue).
			Padding(0, 1),

		UserLabel: lipgloss.NewStyle().
			Bold(true).
			Foreground(colorNavyBlue),

		AssistantLabel: lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPurple),

		StreamingText: lipgloss.NewStyle().
			Foreground(colorLightGray),

		Error: lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWestpacRed),

		StatusBar: lipgloss.NewStyle().
			Foreground(colorDimGray).
			Background(colorLightGray).
			Padding(0, 1),

		Help: lipgloss.NewStyle().
			Foreground(colorDimGray),

		Spinner: lipgloss.NewStyle().
			Foreground(colorWestpacRed),

		Dim: lipgloss.NewStyle().
			Foreground(colorDimGray),

		CompletionNormal: lipgloss.NewStyle().
			Foreground(colorWhite).
			Background(colorDarkNavy).
			Padding(0, 1),

		CompletionSelected: lipgloss.NewStyle().
			Foreground(colorWhite).
			Background(colorNavyBlue).
			Bold(true).
			Padding(0, 1),
	}
}
