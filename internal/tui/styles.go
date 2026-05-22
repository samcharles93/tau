package tui

import (
	"bitbucket.srv.westpac.com.au/m055731/aim/internal/theme"
	"charm.land/lipgloss/v2"
)

// tuiTheme holds pre-computed styles derived from the brand colour palette.
type tuiTheme struct {
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

func newTheme() tuiTheme {
	return tuiTheme{
		StreamingText: lipgloss.NewStyle().
			Foreground(theme.ColorLightGray),

		Error: theme.Error,

		StatusBar: lipgloss.NewStyle().
			Foreground(theme.ColorDimGray),

		Help: theme.Help,

		Spinner: lipgloss.NewStyle().
			Foreground(theme.ColorWestpacRed),

		Dim: theme.Dim,

		CompletionNormal: lipgloss.NewStyle().
			Foreground(theme.ColorWhite).
			Background(theme.ColorDarkNavy).
			Padding(0, 1),

		CompletionSelected: lipgloss.NewStyle().
			Foreground(theme.ColorWhite).
			Background(theme.ColorNavyBlue).
			Bold(true).
			Padding(0, 1),
	}
}
