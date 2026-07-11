package theme

import (
	"testing"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// TestBrandPaletteHexValues locks the six semantic brand colors to their
// specified hex values, so a future edit can't silently drift the palette.
func TestBrandPaletteHexValues(t *testing.T) {
	cases := []struct {
		name string
		got  termkit.Color
		want termkit.Color
	}{
		{"AccentColor (Warm Ochre)", AccentColor, termkit.Color{0xD1, 0x9A, 0x66}},
		{"SuccessColor (Sage Green)", SuccessColor, termkit.Color{0x7C, 0x9C, 0x72}},
		{"ErrorColor (Salmon Pink)", ErrorColor, termkit.Color{0xE0, 0x6C, 0x75}},
		{"PrimaryColor (Soft Off-White)", PrimaryColor, termkit.Color{0xAB, 0xB2, 0xBF}},
		{"SecondaryColor (Slate Blue-Grey)", SecondaryColor, termkit.Color{0x28, 0x33, 0x47}},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// TestBrandPaletteContrastAgainstCommonBackgrounds guards against the
// palette accidentally shipping a foreground color too dark or too light to
// read comfortably against a typical dark terminal background, keeping the
// "avoid reducing contrast below comfortable readability" rule honest.
// SecondaryColor is excluded: it is meant for borders/chrome, not readable
// body text (see its doc comment), so it deliberately sits closer to the
// background end of the range.
func TestBrandPaletteContrastAgainstCommonBackgrounds(t *testing.T) {
	darkBG := termkit.Color{0x1E, 0x22, 0x2A} // a representative dark terminal background
	const minReadableContrast = 3.0           // WCAG "large text" floor

	fgColors := map[string]termkit.Color{
		"AccentColor":  AccentColor,
		"SuccessColor": SuccessColor,
		"ErrorColor":   ErrorColor,
		"PrimaryColor": PrimaryColor,
	}
	for name, c := range fgColors {
		if ratio := termkit.ContrastRatio(c, darkBG); ratio < minReadableContrast {
			t.Errorf("%s has contrast ratio %.2f against a dark background, want >= %.1f", name, ratio, minReadableContrast)
		}
	}
}
