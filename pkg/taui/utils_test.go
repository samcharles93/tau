package taui

import "testing"

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
