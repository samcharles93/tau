package taui

import (
	"strings"
	"testing"
)

func TestStatusRowStates(t *testing.T) {
	cases := []struct {
		state     StatusRowState
		wantGlyph string
	}{
		{StatusRowRunning, ""}, // spinner frame varies; checked separately
		{StatusRowSuccess, "✓"},
		{StatusRowFailed, "✗"},
		{StatusRowNeutral, "•"},
	}
	for _, tc := range cases {
		r := NewStatusRow("label", "detail", tc.state)
		line := r.Render(80)[0]
		if !strings.Contains(line, "label") || !strings.Contains(line, "detail") {
			t.Errorf("state %d: line %q missing label/detail", tc.state, line)
		}
		if tc.wantGlyph != "" && !strings.Contains(line, tc.wantGlyph) {
			t.Errorf("state %d: line %q missing glyph %q", tc.state, line, tc.wantGlyph)
		}
	}
}

func TestStatusRowNoDetailOmitsSeparator(t *testing.T) {
	r := NewStatusRow("label", "", StatusRowNeutral)
	line := r.Render(80)[0]
	if strings.Contains(line, "-") {
		t.Errorf("line with no detail should not contain the separator: %q", line)
	}
}

func TestStatusRowTickOnlyAdvancesWhileRunning(t *testing.T) {
	r := NewStatusRow("label", "", StatusRowSuccess)
	before := r.frame
	r.Tick()
	if r.frame != before {
		t.Errorf("Tick should be a no-op once resolved, frame changed from %d to %d", before, r.frame)
	}

	running := NewStatusRow("label", "", StatusRowRunning)
	running.Tick()
	if running.frame == 0 {
		t.Error("Tick should advance the frame while running")
	}
}
