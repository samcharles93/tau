package taui

import "testing"

func TestCanonicalKeyNormalizesDisambiguated(t *testing.T) {
	cases := map[string]string{
		"\x1b[27u":    "\x1b", // Esc
		"\x1b[13u":    "\r",   // Enter
		"\x1b[9u":     "\t",   // Tab
		"\x1b[127u":   "\x7f", // Backspace
		"\x1b[99;5u":  "\x03", // Ctrl+C (Kitty CSI-u, codepoint form)
		"\x1b[3;5u":   "\x03", // Ctrl+C (Kitty CSI-u, control-char form)
		"\x1b[106;5u": "\x0a", // Ctrl+J — newline
		"\x1b[10;5u":  "\x0a", // Ctrl+J (control-char form)
		"\x1b[115;5u": "\x13", // Ctrl+S — steer
		"\x1b[97;5u":  "\x01", // Ctrl+A — home
		"\x1b[101;5u": "\x05", // Ctrl+E — end
		"\x1b[119;5u": "\x17", // Ctrl+W — delete word

		// Real ghostty sequences with NumLock active (modifier 133 = 1+Ctrl+NumLock):
		// the lock bit must be ignored so these still resolve to control bytes.
		"\x1b[99;133u":  "\x03", // Ctrl+C
		"\x1b[106;133u": "\x0a", // Ctrl+J
		"\x1b[115;133u": "\x13", // Ctrl+S
		"\x1b[97;133u":  "\x01", // Ctrl+A
		// Event-type sub-parameter (press/repeat/release) must be ignored too.
		"\x1b[99;5:1u": "\x03", // Ctrl+C, press event
	}
	for in, want := range cases {
		if got := canonicalKey(in); got != want {
			t.Errorf("canonicalKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCanonicalKeyStripsLockModifiers(t *testing.T) {
	// Shift+Enter with NumLock on (modifier 130 = 1+Shift+NumLock) must normalize
	// to the canonical Shift+Enter form so the line input's newline case matches.
	cases := map[string]string{
		"\x1b[13;130u": "\x1b[13;2u", // Shift+Enter + NumLock -> Shift+Enter
		"\x1b[13;2u":   "\x1b[13;2u", // already canonical -> unchanged

		// Cursor keys with NumLock on (modifier 129 = 1+NumLock) collapse to the
		// bare legacy form so the line input / completions handlers match.
		"\x1b[1;129A": "\x1b[A", // Up + NumLock
		"\x1b[1;129B": "\x1b[B", // Down + NumLock
		"\x1b[1;129C": "\x1b[C", // Right + NumLock
		"\x1b[1;129D": "\x1b[D", // Left + NumLock

		// A real modifier alongside NumLock keeps the modifier, drops the lock.
		"\x1b[1;133D": "\x1b[1;5D", // Ctrl+Left + NumLock -> Ctrl+Left (word jump)
		"\x1b[3;129~": "\x1b[3~",   // Delete + NumLock -> Delete
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
