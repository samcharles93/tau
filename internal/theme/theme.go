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
	HexGray800   = "#1F2937"
	HexGreen     = "#00A300"

	// Splash palette — warm Coral (Tau brand) and Lilac (π family).
	HexCoral     = "#f8d1cf"
	HexCoralDeep = "#e8a8a0"
	HexCoralDim  = "#d4b8b4"
	HexLilac     = "#f0ecff"
	HexLilacMid  = "#c9c0e8"
	HexLilacSoft = "#e8e4f8"
	HexLilacDim  = "#9890c0"
)

var (
	ColorDarkNavy  = mustColor(HexDarkNavy)
	ColorWhite     = mustColor(HexWhite)
	ColorRed       = mustColor(HexRed)
	ColorNavyBlue  = mustColor(HexNavyBlue)
	ColorPurple    = mustColor(HexPurple)
	ColorLightGray = mustColor(HexLightGray)
	ColorDimGray   = mustColor(HexDimGray)
	ColorGray800   = mustColor(HexGray800)
	ColorGreen     = mustColor(HexGreen)

	ColorCoral     = mustColor(HexCoral)
	ColorCoralDeep = mustColor(HexCoralDeep)
	ColorCoralDim  = mustColor(HexCoralDim)
	ColorLilac     = mustColor(HexLilac)
	ColorLilacMid  = mustColor(HexLilacMid)
	ColorLilacSoft = mustColor(HexLilacSoft)
	ColorLilacDim  = mustColor(HexLilacDim)
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

// SelectedStyle is used for focused/selected list items, cycle elements, etc.
func SelectedStyle() gotui.Style {
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
