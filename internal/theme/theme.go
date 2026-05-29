// Package theme provides the shared colour palette and pre-built styles for the
// Tau application. It is a leaf dependency with zero internal imports so any
// package may use it.
package theme

import gotui "github.com/grindlemire/go-tui"

// --- Brand colours ---

// Hex constants for the brand palette. Keep raw colour values in this package
// only; consuming packages should use the Color/Style helpers below.
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
	ColorDarkNavy  = mustColor(HexDarkNavy)
	ColorWhite     = mustColor(HexWhite)
	ColorRed       = mustColor(HexRed)
	ColorNavyBlue  = mustColor(HexNavyBlue)
	ColorPurple    = mustColor(HexPurple)
	ColorLightGray = mustColor(HexLightGray)
	ColorDimGray   = mustColor(HexDimGray)
	ColorGreen     = mustColor(HexGreen)
)

func mustColor(hex string) gotui.Color {
	color, err := gotui.HexColor(hex)
	if err != nil {
		panic("invalid theme hex colour: " + hex)
	}
	return color
}

// BrandStyle is the brand accent for headers, active controls, and highlights.
func BrandStyle() gotui.Style {
	return gotui.NewStyle().Foreground(ColorPurple).Bold()
}

// BodyStyle is default prose content.
func BodyStyle() gotui.Style {
	return gotui.NewStyle().Foreground(ColorLightGray)
}

// DimStyle is secondary/muted text.
func DimStyle() gotui.Style {
	return gotui.NewStyle().Foreground(ColorDimGray)
}

// ErrorStyle is used for error messages and destructive highlights.
func ErrorStyle() gotui.Style {
	return gotui.NewStyle().Foreground(ColorRed).Bold()
}

// ReadyStyle indicates an available/healthy resource.
func ReadyStyle() gotui.Style {
	return gotui.NewStyle().Foreground(ColorGreen)
}

// BorderStyle is used for inactive borders and structural separators.
func BorderStyle() gotui.Style {
	return gotui.NewStyle().Foreground(ColorDimGray)
}
