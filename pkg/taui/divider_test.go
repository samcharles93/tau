package taui

import (
	"strings"
	"testing"
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
