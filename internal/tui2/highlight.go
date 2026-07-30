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
		Text: ansi.StylePrimitive{
			Color: new(hex(theme.ToneMuted)),
		},
		Error: ansi.StylePrimitive{
			Color: new(hex(theme.ToneError)),
		},
		Comment: ansi.StylePrimitive{
			Color: new(hex(theme.ToneMuted)),
		},
		CommentPreproc: ansi.StylePrimitive{
			Color: new(hex(theme.ToneMuted)),
			Bold:  new(true),
		},
		Keyword: ansi.StylePrimitive{
			Color: new(hex(theme.ToneInfo)),
		},
		KeywordReserved: ansi.StylePrimitive{
			Color: new(hex(theme.ToneInfo)),
			Bold:  new(true),
		},
		KeywordNamespace: ansi.StylePrimitive{
			Color: new(hex(theme.ToneInfo)),
		},
		KeywordType: ansi.StylePrimitive{
			Color: new(hex(theme.AccentColor)),
		},
		Operator: ansi.StylePrimitive{
			Color: new(hex(theme.TonePrimary)),
		},
		Punctuation: ansi.StylePrimitive{
			Color: new(hex(theme.ToneMuted)),
		},
		Name: ansi.StylePrimitive{
			Color: new(hex(theme.ToneBody)),
		},
		NameBuiltin: ansi.StylePrimitive{
			Color: new(hex(theme.AccentColor)),
		},
		NameTag: ansi.StylePrimitive{
			Color: new(hex(theme.ToneInfo)),
		},
		NameAttribute: ansi.StylePrimitive{
			Color: new(hex(theme.ToneSuccess)),
		},
		NameClass: ansi.StylePrimitive{
			Color: new(hex(theme.AccentColor)),
			Bold:  new(true),
		},
		NameConstant: ansi.StylePrimitive{
			Color: new(hex(theme.ToneWarn)),
		},
		NameDecorator: ansi.StylePrimitive{
			Color: new(hex(theme.ToneInfo)),
		},
		NameException: ansi.StylePrimitive{
			Color: new(hex(theme.ToneError)),
		},
		NameFunction: ansi.StylePrimitive{
			Color: new(hex(theme.AccentColor)),
		},
		NameOther: ansi.StylePrimitive{
			Color: new(hex(theme.ToneBody)),
		},
		Literal: ansi.StylePrimitive{
			Color: new(hex(theme.ToneBody)),
		},
		LiteralNumber: ansi.StylePrimitive{
			Color: new(hex(theme.ToneWarn)),
		},
		LiteralDate: ansi.StylePrimitive{
			Color: new(hex(theme.ToneWarn)),
		},
		LiteralString: ansi.StylePrimitive{
			Color: new(hex(theme.ToneSuccess)),
		},
		LiteralStringEscape: ansi.StylePrimitive{
			Color: new(hex(theme.ToneInfo)),
		},
		GenericDeleted: ansi.StylePrimitive{
			Color: new(hex(theme.ToneError)),
		},
		GenericEmph: ansi.StylePrimitive{
			Color:  new(hex(theme.ToneBody)),
			Italic: new(true),
		},
		GenericInserted: ansi.StylePrimitive{
			Color: new(hex(theme.ToneSuccess)),
		},
		GenericStrong: ansi.StylePrimitive{
			Color: new(hex(theme.ToneBody)),
			Bold:  new(true),
		},
		GenericSubheading: ansi.StylePrimitive{
			Color: new(hex(theme.AccentColor)),
			Bold:  new(true),
		},
		// Background is deliberately omitted — no hard-coded code-block
		// background; code blocks inherit the terminal background.
	}
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
