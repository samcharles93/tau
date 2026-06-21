package taui

import (
	"strings"
	"testing"
)

// The default (combined) style shows a ✓/✗ glyph + name/args/detail; the badge
// style shows SUCCESS/FAILED words. Both the glyphs and the name/args/detail
// appear in the colour and no-colour branches, so these assertions are stable
// regardless of whether termkit thinks colour is enabled in the test env.

func TestToolRowStartsRunning(t *testing.T) {
	r := NewToolRow("go", "build ./...")
	if !r.Running() {
		t.Fatalf("new ToolRow should be running, got state %d", r.State())
	}
	line := r.Render(80)[0]
	for _, want := range []string{"go", "build ./..."} {
		if !strings.Contains(line, want) {
			t.Errorf("running line %q missing %q", line, want)
		}
	}
}

func TestToolRowSucceed(t *testing.T) {
	r := NewToolRow("go", "build ./...")
	r.Succeed("done in 1.2s")

	if r.State() != ToolSuccess {
		t.Fatalf("state = %d, want ToolSuccess", r.State())
	}
	line := r.Render(80)[0]
	for _, want := range []string{"✓", "go", "done in 1.2s"} {
		if !strings.Contains(line, want) {
			t.Errorf("success line %q missing %q", line, want)
		}
	}
	if strings.Contains(line, "✗") {
		t.Errorf("success line should not contain the failure glyph: %q", line)
	}
}

func TestToolRowFail(t *testing.T) {
	r := NewToolRow("golangci-lint", "run ./...")
	r.Fail("exit 1")

	if r.State() != ToolFailed {
		t.Fatalf("state = %d, want ToolFailed", r.State())
	}
	line := r.Render(80)[0]
	for _, want := range []string{"✗", "golangci-lint", "exit 1"} {
		if !strings.Contains(line, want) {
			t.Errorf("failed line %q missing %q", line, want)
		}
	}
}

func TestToolRowBadgeStyle(t *testing.T) {
	r := NewToolRow("go", "build ./...")
	r.SetStyle(ToolStyleBadge)
	r.Succeed("done in 1.2s")
	line := r.Render(80)[0]
	for _, want := range []string{"SUCCESS", "go", "done in 1.2s"} {
		if !strings.Contains(line, want) {
			t.Errorf("badge success line %q missing %q", line, want)
		}
	}
}

func TestToolRowTickIsNoOpAfterResolve(t *testing.T) {
	r := NewToolRow("fetch", "url")
	r.Tick()
	r.Tick()
	r.Succeed("")
	r.Tick() // must not panic or revert state
	if r.State() != ToolSuccess {
		t.Errorf("Tick after resolve changed state to %d", r.State())
	}
}

func TestToolRowRendersSingleLine(t *testing.T) {
	r := NewToolRow("mkdir", "src/components")
	if got := len(r.Render(80)); got != 1 {
		t.Errorf("ToolRow rendered %d lines, want 1", got)
	}
}
