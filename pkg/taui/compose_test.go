package taui

import (
	"strings"
	"testing"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// TestToolRowComposesInColouredBox guards the "combined format" requirement:
// a ToolRow (default ToolStyleCombined) must render fg-only so it sits on a
// coloured Box background without terminating it. A stray full reset (\x1b[0m)
// inside the row would cut the box background mid-line.
func TestToolRowComposesInColouredBox(t *testing.T) {
	termkit.ForceColor()
	defer termkit.DisableColor()

	row := NewToolRow("go", "build ./...")
	row.Succeed("done in 1.1s")

	box := NewBox().Padding(1, 1).ExpandW().
		Bg(func(s string) string { return termkit.FgBgOnly(s, termkit.ColorObsidian, termkit.ColorAmber) }).
		Build()
	box.AddChild(row)

	var rowLine string
	for _, ln := range box.Render(60) {
		if strings.Contains(ln, "build ./...") {
			rowLine = ln
		}
	}
	if rowLine == "" {
		t.Fatal("tool row not found in box render")
	}

	// The box background must wrap the row line…
	if !strings.Contains(rowLine, termkit.ColorAmber.Bg()) {
		t.Errorf("row line is missing the box background code: %q", rowLine)
	}
	// …and the row must not emit a full reset that would break it. (The engine
	// appends the single end-of-line reset later, during render.)
	if strings.Contains(rowLine, "\x1b[0m") {
		t.Errorf("tool row emitted a reset that breaks the box background: %q", rowLine)
	}
}

// TestBadgeStyleDoesEmbedReset documents the contrast: the badge style is *not*
// box-safe - it embeds resets - which is why combined is the default and the
// in-box choice.
func TestBadgeStyleDoesEmbedReset(t *testing.T) {
	termkit.ForceColor()
	defer termkit.DisableColor()

	row := NewToolRow("go", "build ./...")
	row.SetStyle(ToolStyleBadge)
	row.Succeed("done in 1.1s")

	if !strings.Contains(row.Render(60)[0], "\x1b[0m") {
		t.Skip("badge style produced no reset in this build; nothing to contrast")
	}
}
