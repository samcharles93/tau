package taui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

func TestDividerPlainFillsWidth(t *testing.T) {
	d := NewDivider("")
	lines := d.Render(10)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if got := VisibleWidth(lines[0]); got != 10 {
		t.Errorf("width = %d, want 10: %q", got, lines[0])
	}
}

func TestDividerCentersLabel(t *testing.T) {
	d := NewDivider("Results")
	lines := d.Render(20)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if !strings.Contains(lines[0], "Results") {
		t.Errorf("expected label in divider line: %q", lines[0])
	}
	if got := VisibleWidth(lines[0]); got != 20 {
		t.Errorf("width = %d, want 20: %q", got, lines[0])
	}
}

func TestDividerLabelWiderThanWidth(t *testing.T) {
	d := NewDivider("a much longer label than the available width")
	lines := d.Render(10)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if got := VisibleWidth(lines[0]); got > 10 {
		t.Errorf("width = %d, want <= 10: %q", got, lines[0])
	}
}

// TestDividerNilModeFuncUnaffected guards against a regression where adding
// SetModeFunc changed rendering for dividers that never call it — a Divider
// with no mode func (or one whose func returns nil) must render identically
// to a plain/static-label Divider, matching every other Divider in the app.
func TestDividerNilModeFuncUnaffected(t *testing.T) {
	termkit.ForceColor()
	defer termkit.DisableColor()

	plain := NewDivider("")
	withNilFunc := NewDivider("")
	withNilFunc.SetModeFunc(func() *InputMode { return nil })

	if got, want := plain.Render(20), withNilFunc.Render(20); got[0] != want[0] {
		t.Fatalf("nil-returning mode func changed rendering: %q vs %q", got[0], want[0])
	}
}

// TestDividerModeFuncOverridesLabelAndColor verifies an active mode replaces
// the static label with the mode's label and colors both the rule dashes
// and the label pill.
func TestDividerModeFuncOverridesLabelAndColor(t *testing.T) {
	termkit.ForceColor()
	defer termkit.DisableColor()

	d := NewDivider("")
	mode := &InputMode{Label: "Shell", Color: termkit.Xterm256(209)}
	d.SetModeFunc(func() *InputMode { return mode })

	lines := d.Render(30)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	line := lines[0]
	if !strings.Contains(line, "Shell") {
		t.Fatalf("expected mode label in divider line: %q", line)
	}
	if !strings.Contains(line, mode.Color.Fg()) {
		t.Fatalf("expected rule dashes colored with mode.Color: %q", line)
	}
	if !strings.Contains(line, mode.Color.Bg()) {
		t.Fatalf("expected label pill background colored with mode.Color: %q", line)
	}
	if got := VisibleWidth(line); got != 30 {
		t.Errorf("width = %d, want 30: %q", got, line)
	}
}

// TestDividerModeFuncLabelIsLeftAligned verifies a mode's label sits near
// the left edge (modeLabelLeftMargin dashes in), not centered — an
// input-mode indicator should catch the eye immediately, unlike a static
// Divider label such as "Results" (still centered — see
// TestDividerCentersLabel), which behaves like a section heading.
func TestDividerModeFuncLabelIsLeftAligned(t *testing.T) {
	termkit.ForceColor()
	defer termkit.DisableColor()

	d := NewDivider("")
	mode := &InputMode{Label: "Shell", Color: termkit.Xterm256(209)}
	d.SetModeFunc(func() *InputMode { return mode })

	line := d.Render(60)[0]
	plain := stripANSI(line)
	before, _, found := strings.Cut(plain, "Shell")
	if !found {
		t.Fatalf("expected label in rendered line: %q", plain)
	}
	// "─" is multi-byte in UTF-8, so count runes (columns), not bytes.
	col := utf8.RuneCountInString(before)
	// col counts the leading margin dashes plus the label's own leading
	// space (" Shell"), so it should equal modeLabelLeftMargin + 1.
	if col != modeLabelLeftMargin+1 {
		t.Fatalf("expected label to start at column %d (left-aligned), got %d: %q", modeLabelLeftMargin+1, col, plain)
	}
	// It must not be anywhere near center (60-wide line, center ~30).
	if col > 15 {
		t.Fatalf("label at column %d looks centered, not left-aligned: %q", col, plain)
	}
}

// TestDividerHideModeLabel verifies HideModeLabel renders the mode's color
// as a plain full-width rule without its name — used for the bottom
// divider around an input box, so only the top one names the mode.
func TestDividerHideModeLabel(t *testing.T) {
	termkit.ForceColor()
	defer termkit.DisableColor()

	d := NewDivider("")
	mode := &InputMode{Label: "Shell", Color: termkit.Xterm256(209)}
	d.SetModeFunc(func() *InputMode { return mode })
	d.HideModeLabel()

	line := d.Render(30)[0]
	if strings.Contains(line, "Shell") {
		t.Fatalf("expected no label text with HideModeLabel set: %q", line)
	}
	if !strings.Contains(line, mode.Color.Fg()) {
		t.Fatalf("expected the rule to still be colored with mode.Color: %q", line)
	}
	if got := VisibleWidth(line); got != 30 {
		t.Errorf("width = %d, want 30: %q", got, line)
	}
}

// TestDividerModeFuncResetsBeforeTrailingDashes guards against a real
// regression found via visual inspection: the label pill sets both a
// foreground AND a background color with no trailing Reset (FgBgOnly/Wrap
// semantics), so without an explicit Reset before the trailing dashes, the
// pill's background bled forward — the dashes after the label rendered as a
// solid colored block (leftover background) instead of colored dash glyphs
// on the default background.
func TestDividerModeFuncResetsBeforeTrailingDashes(t *testing.T) {
	termkit.ForceColor()
	defer termkit.DisableColor()

	d := NewDivider("")
	mode := &InputMode{Label: "Shell", Color: termkit.Xterm256(209)}
	d.SetModeFunc(func() *InputMode { return mode })

	line := d.Render(30)[0]
	pillEnd := strings.Index(line, mode.Color.Bg()) + len(mode.Color.Bg()) + len(" Shell ")
	afterPill := line[pillEnd:]
	if !strings.HasPrefix(afterPill, termkit.Reset) {
		t.Fatalf("expected a Reset immediately after the label pill, got: %q", afterPill)
	}
}

// TestDividerModeFuncLiveUpdates verifies the mode is re-resolved on every
// Render call (the pull-based live-update model), not cached from
// construction.
func TestDividerModeFuncLiveUpdates(t *testing.T) {
	current := ""
	d := NewDivider("")
	d.SetModeFunc(func() *InputMode {
		if current == "" {
			return nil
		}
		return &InputMode{Label: current, Color: termkit.ColorGreen}
	})

	if lines := d.Render(20); strings.Contains(lines[0], "Shell") {
		t.Fatalf("expected no label before mode is set: %q", lines[0])
	}

	current = "Shell"
	lines := d.Render(20)
	if !strings.Contains(lines[0], "Shell") {
		t.Fatalf("expected label after mode changes: %q", lines[0])
	}
}
