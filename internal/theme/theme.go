// Package theme provides the shared colour palette and pre-built styles for the
// Tau application. It is a leaf dependency with zero internal imports so any
// package may use it.
package theme

import (
	"charm.land/glamour/v2/ansi"
	"charm.land/lipgloss/v2"
)

// --- Brand colours ---

// Hex constants for the brand palette. Used by both lipgloss Color definitions
// and glamour markdown styles (which require *string hex values).
const (
	HexDarkNavy  = "#181B25"
	HexWhite     = "#FFFFFF"
	HexRed       = "#DA1710"
	HexNavyBlue  = "#1F1B4F"
	HexPurple    = "#9819D7"
	HexLightGray = "#E8E8ED"
	HexDimGray   = "#a5a5b1"
	HexGreen     = "#00A300"
)

var (
	ColorDarkNavy  = lipgloss.Color(HexDarkNavy)
	ColorWhite     = lipgloss.Color(HexWhite)
	ColorRed       = lipgloss.Color(HexRed)
	ColorNavyBlue  = lipgloss.Color(HexNavyBlue)
	ColorPurple    = lipgloss.Color(HexPurple)
	ColorLightGray = lipgloss.Color(HexLightGray)
	ColorDimGray   = lipgloss.Color(HexDimGray)
	ColorGreen     = lipgloss.Color(HexGreen)
)

// --- Pre-built semantic styles ---

var (
	// Error is used for error messages and destructive highlights.
	Error = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorRed)

	// Dim is secondary/muted text.
	Dim = lipgloss.NewStyle().
		Foreground(ColorDimGray)

	// Help is for keybinding hints.
	Help = lipgloss.NewStyle().
		Foreground(ColorDimGray)

	// Faint dims text via the terminal's faint attribute.
	Faint = lipgloss.NewStyle().
		Faint(true)

	// Bold is a convenience for bold text with no colour override.
	Bold = lipgloss.NewStyle().
		Bold(true)

	// StatusReady indicates an available/healthy resource.
	StatusReady = lipgloss.NewStyle().
			Foreground(ColorGreen)

	// StatusNotReady indicates an unavailable resource.
	StatusNotReady = lipgloss.NewStyle().
			Foreground(ColorRed).
			Faint(true)

	// Accent is the brand accent (purple) for borders and highlights.
	Accent = lipgloss.NewStyle().
		Foreground(ColorPurple)
)

// --- Markdown rendering style (glamour ansi.StyleConfig) ---

// strPtr is a helper to satisfy glamour's *string colour API.
func strPtr(s string) *string { return &s }

// boolPtr is a helper to satisfy glamour's *bool style API.
func boolPtr(b bool) *bool { return &b }

// uintPtr is a helper to satisfy glamour's *uint indent API.
func uintPtr(u uint) *uint { return &u }

// MarkdownStyle returns the brand-consistent glamour style configuration.
// All colours reference constants in this package — never local hex literals.
func MarkdownStyle() ansi.StyleConfig {
	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: strPtr(HexLightGray),
			},
			Margin: uintPtr(0),
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  boolPtr(true),
				Color: strPtr(HexNavyBlue),
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  boolPtr(true),
				Color: strPtr(HexPurple),
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  boolPtr(true),
				Color: strPtr(HexNavyBlue),
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Bold:  boolPtr(true),
				Color: strPtr(HexDimGray),
			},
		},
		Strong: ansi.StylePrimitive{
			Bold:  boolPtr(true),
			Color: strPtr(HexWhite),
		},
		Emph: ansi.StylePrimitive{
			Italic: boolPtr(true),
			Color:  strPtr(HexLightGray),
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Italic: boolPtr(true),
				Color:  strPtr(HexDimGray),
			},
			Indent: uintPtr(2),
		},
		Link: ansi.StylePrimitive{
			Color:     strPtr(HexPurple),
			Underline: boolPtr(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: strPtr(HexPurple),
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color:           strPtr(HexWhite),
				BackgroundColor: strPtr(HexDarkNavy),
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color: strPtr(HexLightGray),
				},
				Margin: uintPtr(1),
			},
			Chroma: &ansi.Chroma{
				Text: ansi.StylePrimitive{
					Color: strPtr(HexLightGray),
				},
				Keyword: ansi.StylePrimitive{
					Color: strPtr(HexPurple),
				},
				LiteralString: ansi.StylePrimitive{
					Color: strPtr(HexGreen),
				},
				Comment: ansi.StylePrimitive{
					Color: strPtr(HexDimGray),
				},
			},
		},
		List: ansi.StyleList{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color: strPtr(HexLightGray),
				},
			},
		},
		Item: ansi.StylePrimitive{
			Color: strPtr(HexLightGray),
		},
		HorizontalRule: ansi.StylePrimitive{
			Color: strPtr(HexDimGray),
		},
		Table: ansi.StyleTable{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color: strPtr(HexLightGray),
				},
			},
		},
	}
}
