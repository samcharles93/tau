package tui2

import (
	"charm.land/glamour/v2/ansi"

	"github.com/samcharles93/tau/internal/theme"
)

// tauGlamourConfig returns a glamour StyleConfig that uses Tau's semantic
// colour palette for all markdown elements, including fenced code blocks via
// chroma. No Background colours are set anywhere — code blocks inherit the
// terminal background, and primary content inherits the terminal foreground.
func tauGlamourConfig() ansi.StyleConfig {
	cfg := ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{},
		},
		Paragraph: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{},
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: new(hex(theme.AccentColor)),
				Bold:  new(true),
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: new(hex(theme.AccentColor)),
				Bold:  new(true),
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: new(hex(theme.AccentColor)),
				Bold:  new(true),
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: new(hex(theme.AccentColor)),
				Bold:  new(true),
			},
		},
		H4: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: new(hex(theme.AccentColor)),
				Bold:  new(true),
			},
		},
		H5: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: new(hex(theme.AccentColor)),
				Bold:  new(true),
			},
		},
		H6: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: new(hex(theme.AccentColor)),
				Bold:  new(true),
			},
		},
		Strong: ansi.StylePrimitive{
			Bold: new(true),
		},
		Emph: ansi.StylePrimitive{
			Italic: new(true),
		},
		Link: ansi.StylePrimitive{
			Color:     new(hex(theme.ToneInfo)),
			Underline: new(true),
		},
		LinkText: ansi.StylePrimitive{
			Color:     new(hex(theme.ToneInfo)),
			Underline: new(true),
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: new(hex(theme.AccentColor)),
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			Chroma: tauChromaInline(),
		},
		BlockQuote: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: new(hex(theme.ToneMuted)),
			},
		},
	}

	return cfg
}

// tauChromaInline returns the chroma configuration for fenced code blocks,
// mapping token types to Tau's restrained semantic palette.
// No Background colour is set on any token — code blocks inherit the
// terminal background without a hard-coded fill.
func tauChromaInline() *ansi.Chroma {
	return &ansi.Chroma{
		Text:                chromaPrim(theme.ToneMuted, false, false),
		Error:               chromaPrim(theme.ToneError, false, false),
		Comment:             chromaPrim(theme.ToneMuted, false, false),
		CommentPreproc:      chromaPrim(theme.ToneMuted, true, false),
		Keyword:             chromaPrim(theme.ToneInfo, false, false),
		KeywordReserved:     chromaPrim(theme.ToneInfo, true, false),
		KeywordNamespace:    chromaPrim(theme.ToneInfo, false, false),
		KeywordType:         chromaPrim(theme.AccentColor, false, false),
		Operator:            chromaPrim(theme.TonePrimary, false, false),
		Punctuation:         chromaPrim(theme.ToneMuted, false, false),
		Name:                chromaPrim(theme.ToneBody, false, false),
		NameBuiltin:         chromaPrim(theme.AccentColor, false, false),
		NameTag:             chromaPrim(theme.ToneInfo, false, false),
		NameAttribute:       chromaPrim(theme.ToneSuccess, false, false),
		NameClass:           chromaPrim(theme.AccentColor, true, false),
		NameConstant:        chromaPrim(theme.ToneWarn, false, false),
		NameDecorator:       chromaPrim(theme.ToneInfo, false, false),
		NameException:       chromaPrim(theme.ToneError, false, false),
		NameFunction:        chromaPrim(theme.AccentColor, false, false),
		NameOther:           chromaPrim(theme.ToneBody, false, false),
		Literal:             chromaPrim(theme.ToneBody, false, false),
		LiteralNumber:       chromaPrim(theme.ToneWarn, false, false),
		LiteralDate:         chromaPrim(theme.ToneWarn, false, false),
		LiteralString:       chromaPrim(theme.ToneSuccess, false, false),
		LiteralStringEscape: chromaPrim(theme.ToneInfo, false, false),
		GenericDeleted:      chromaPrim(theme.ToneError, false, false),
		GenericEmph:         chromaPrim(theme.ToneBody, false, true),
		GenericInserted:     chromaPrim(theme.ToneSuccess, false, false),
		GenericStrong:       chromaPrim(theme.ToneBody, true, false),
		GenericSubheading:   chromaPrim(theme.AccentColor, true, false),
		// Background is deliberately omitted — no hard-coded code-block
		// background; code blocks inherit the terminal background.
	}
}

// chromaPrim builds a chroma StylePrimitive from a theme tone plus optional
// bold/italic emphasis.
func chromaPrim(c [3]uint8, bold, italic bool) ansi.StylePrimitive {
	prim := ansi.StylePrimitive{Color: new(hex(c))}
	if bold {
		prim.Bold = new(true)
	}
	if italic {
		prim.Italic = new(true)
	}
	return prim
}

// hex returns a hex colour string (e.g. "#D19A66") from a termkit.Color.
func hex(c [3]uint8) string {
	// Use a fixed-size buffer to avoid allocations in the hot path.
	var buf [8]byte
	buf[0] = '#'
	buf[1] = hexDigit(c[0] >> 4)
	buf[2] = hexDigit(c[0] & 0xF)
	buf[3] = hexDigit(c[1] >> 4)
	buf[4] = hexDigit(c[1] & 0xF)
	buf[5] = hexDigit(c[2] >> 4)
	buf[6] = hexDigit(c[2] & 0xF)
	return string(buf[:7])
}

func hexDigit(v uint8) byte {
	if v < 10 {
		return '0' + v
	}
	return 'A' + (v - 10)
}
