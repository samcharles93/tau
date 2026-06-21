package taui

import "testing"

func TestCanonicalKeyNormalizesDisambiguated(t *testing.T) {
	cases := map[string]string{
		"\x1b[27u":   "\x1b", // Esc
		"\x1b[13u":   "\r",   // Enter
		"\x1b[9u":    "\t",   // Tab
		"\x1b[127u":  "\x7f", // Backspace
		"\x1b[99;5u": "\x03", // Ctrl+C (Kitty CSI-u, codepoint form)
		"\x1b[3;5u":  "\x03", // Ctrl+C (Kitty CSI-u, control-char form)
	}
	for in, want := range cases {
		if got := canonicalKey(in); got != want {
			t.Errorf("canonicalKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCanonicalKeyLeavesModifiedKeys(t *testing.T) {
	// Modified keys keep their CSI-u form (components match them directly).
	for _, seq := range []string{"\x1b[13;2u", "\x1b[1;5D", "\x1b[A", "a", "\x03"} {
		if got := canonicalKey(seq); got != seq {
			t.Errorf("canonicalKey(%q) = %q, want unchanged", seq, got)
		}
	}
}

func TestEscNormalizationFeedsEscHandlers(t *testing.T) {
	// A disambiguated Esc (\x1b[27u) must reach a component as plain \x1b so
	// existing `case "\x1b"` handlers (e.g. completions dismiss) still fire.
	input := NewLineInput("")
	c := NewCompletions(input, func(ctx CompletionContext) *CompletionSet {
		return &CompletionSet{
			ReplaceStart: 0, ReplaceEnd: 0,
			Groups: []MatchGroup{{Matches: []Match{{Word: "x"}}}},
		}
	})
	c.Render(20) // open the dropdown
	if !c.Visible() {
		t.Fatal("expected dropdown visible")
	}
	// Simulate dispatch normalization, then hand to the component.
	if !c.HandleInput(canonicalKey("\x1b[27u")) {
		t.Fatal("normalized Esc was not consumed by completions")
	}
	if c.Visible() {
		t.Fatal("completions should be dismissed by normalized Esc")
	}
}
