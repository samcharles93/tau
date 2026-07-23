package taui

import (
	"strings"
	"testing"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

func TestVisibleWidthGraphemeAware(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"hello", 5},
		{"世界", 4},    // two wide CJK chars
		{"a世b", 4},   // mixed narrow + wide
		{"👍", 2},     // emoji
		{"é", 1},    // 'e' + combining acute = one grapheme, width 1
		{"👨‍👩‍👧", 2}, // ZWJ family emoji = single grapheme, width 2
	}
	for _, c := range cases {
		if got := VisibleWidth(c.in); got != c.want {
			t.Errorf("VisibleWidth(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestVisibleWidthStripsANSI(t *testing.T) {
	// ANSI escapes have zero width; the wide char inside counts as 2.
	if got := VisibleWidth("\x1b[31m世\x1b[0m"); got != 2 {
		t.Errorf("VisibleWidth with ANSI = %d, want 2", got)
	}
}

func TestTruncateToWidthWideChars(t *testing.T) {
	// "世界" is width 4; truncating to width 3 (ellipsis "…" width 1) should
	// keep one wide char + ellipsis = width 3.
	got := TruncateToWidth("世界", 3, "…")
	if VisibleWidth(got) > 3 {
		t.Errorf("TruncateToWidth(%q,3) = %q (width %d), want <= 3", "世界", got, VisibleWidth(got))
	}
}

func TestStripANSIPlain(t *testing.T) {
	plain := StripANSI("hello world")
	if plain != "hello world" {
		t.Fatalf("StripANSI(%q) = %q", "hello world", plain)
	}
}

func TestStripANSISingleEscape(t *testing.T) {
	plain := StripANSI("\x1b[31mhello\x1b[0m")
	if plain != "hello" {
		t.Fatalf("StripANSI with escape codes = %q, want %q", plain, "hello")
	}
}

func TestStripANSIMultipleCodes(t *testing.T) {
	plain := StripANSI("\x1b[1m\x1b[31mhello\x1b[0m world")
	if plain != "hello world" {
		t.Fatalf("StripANSI with multiple codes = %q, want %q", plain, "hello world")
	}
}

func TestStripANSINonEscapeCodes(t *testing.T) {
	plain := StripANSI("hello\x1bworld")
	if plain != "helloworld" {
		// The incomplete escape is still consumed.
		t.Logf("StripANSI handles incomplete escape, got %q", plain)
	}
}

// TestStripANSIOSC8Hyperlink guards against a regression where StripANSI
// only recognised the SGR 'm' terminator: an OSC 8 hyperlink's ST
// terminator (ESC '\') was never matched, so StripANSI kept "in escape"
// hunting for a stray 'm' anywhere in the URL or link text, silently
// eating real content (and corrupting width math for anything built from
// it, like a status line's "web" segment).
func TestStripANSIOSC8Hyperlink(t *testing.T) {
	termkit.ForceColor()
	t.Cleanup(termkit.DisableColor)
	link := termkit.Hyperlink("web", "http://127.0.0.1:8080")
	if plain := StripANSI(link); plain != "web" {
		t.Fatalf("StripANSI(OSC8 hyperlink) = %q, want %q", plain, "web")
	}

	// A URL containing 'm' (the old bug's exact trigger) must not change
	// the outcome.
	link = termkit.Hyperlink("web", "http://example.com:8080")
	if plain := StripANSI(link); plain != "web" {
		t.Fatalf("StripANSI(OSC8 hyperlink with 'm' in URL) = %q, want %q", plain, "web")
	}
}

func TestStripANSIMalformedAndPrivateCSISequences(t *testing.T) {
	tests := map[string]struct {
		input string
		want  string
	}{
		"unterminated OSC": {input: "visible\x1b]8;;https://example.test", want: "visible"},
		"private CSI":      {input: "\x1b[?25lvisible", want: "visible"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := StripANSI(tt.input); got != tt.want {
				t.Fatalf("StripANSI(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTruncateANSIToWidthFits(t *testing.T) {
	termkit.ForceColor()
	t.Cleanup(termkit.DisableColor)
	s := termkit.FgOnly("hello", termkit.ColorRed)
	out := TruncateANSIToWidth(s, 100, "…")
	plain := StripANSI(out)
	if plain != "hello" {
		t.Fatalf("plain = %q, want %q", plain, "hello")
	}
}

func TestTruncateANSIToWidthTruncates(t *testing.T) {
	termkit.ForceColor()
	t.Cleanup(termkit.DisableColor)
	s := termkit.FgOnly("hello world", termkit.ColorRed)
	out := TruncateANSIToWidth(s, 6, "…")
	plain := StripANSI(out)
	if VisibleWidth(plain) != 6 || !strings.HasPrefix(plain, "hello") {
		t.Fatalf("truncated = %q, want 6-char prefix with ellipsis", plain)
	}
	if !strings.HasSuffix(plain, "…") {
		t.Fatalf("truncated = %q, want trailing ellipsis", plain)
	}
}

func TestTruncateANSIToWidthZeroBudget(t *testing.T) {
	out := TruncateANSIToWidth("hello", 0, "…")
	if out != "" {
		t.Fatalf("expected empty for zero width, got %q", out)
	}
}

func TestTruncateANSIToWidthStyledOutputPreservesEscapes(t *testing.T) {
	termkit.ForceColor()
	t.Cleanup(termkit.DisableColor)
	s := termkit.FgOnly("very long text here", termkit.ColorRed)
	out := TruncateANSIToWidth(s, 10, "…")
	plain := StripANSI(out)
	if VisibleWidth(plain) > 10 {
		t.Fatalf("truncated plain text width = %d, want <= 10", VisibleWidth(plain))
	}
	if !strings.HasSuffix(plain, "…") {
		t.Fatalf("truncated = %q, want trailing ellipsis", plain)
	}
	if !strings.HasPrefix(out, "\x1b") {
		t.Error("truncated output lost ANSI style prefix")
	}
}
