package termkit

import (
	"strings"
	"testing"
)

func TestColorEnabled_noColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	// Reset the cached detection so the env var takes effect.
	colorMu.Lock()
	colorChecked = false
	colorMu.Unlock()

	if ColorEnabled() {
		t.Error("ColorEnabled() should be false when NO_COLOR is set")
	}
}

func TestStyle_colorDisabled(t *testing.T) {
	DisableColor()
	got := Style("hello", Bold, ColorRed.Fg())
	if got != "hello" {
		t.Errorf("Style() with color disabled should return plain text, got %q", got)
	}
}

func TestStyle_colorEnabled(t *testing.T) {
	ForceColor()

	got := Style("hello", Bold)
	if !strings.Contains(got, Bold) || !strings.Contains(got, Reset) || !strings.Contains(got, "hello") {
		t.Errorf("Style() should wrap text in codes, got %q", got)
	}
}

func TestProgressBar(t *testing.T) {
	tests := []struct {
		fraction float64
		width    int
	}{
		{0.0, 10},
		{0.5, 10},
		{1.0, 10},
		{-0.1, 10}, // clamped to 0
		{1.5, 10},  // clamped to 1
	}
	for _, tt := range tests {
		got := ProgressBar(tt.fraction, tt.width)
		if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
			t.Errorf("ProgressBar(%f, %d) = %q, want bracketed", tt.fraction, tt.width, got)
		}
	}
}

func TestSpinnerFrame(t *testing.T) {
	f0 := SpinnerFrame(0)
	f1 := SpinnerFrame(1)
	fWrap := SpinnerFrame(len(SpinnerFrames))
	if f0 == f1 {
		t.Error("consecutive spinner frames should differ")
	}
	if fWrap != f0 {
		t.Error("spinner should wrap around")
	}
}

func TestHyperlink_colorDisabled(t *testing.T) {
	DisableColor()
	got := Hyperlink("click", "https://example.com")
	if got != "click" {
		t.Errorf("Hyperlink() with color disabled should return plain label, got %q", got)
	}
}

func TestFromRGB(t *testing.T) {
	c := FromRGB(128, 64, 32)
	if c[0] != 128 || c[1] != 64 || c[2] != 32 {
		t.Errorf("FromRGB: got %v, want [128, 64, 32]", c)
	}
}

func TestNewIndeterminateToolLifecycle(t *testing.T) {
	tl := NewIndeterminateToolLifecycle("fetch", "https://x.com", nil)
	if !tl.indeterminate {
		t.Error("indeterminate lifecycle should have indeterminate=true")
	}
}

func TestNewToolLifecycle_IsDeterminate(t *testing.T) {
	tl := NewToolLifecycle("go", "build", nil)
	if tl.indeterminate {
		t.Error("determinate lifecycle should have indeterminate=false")
	}
}

func TestColorFgBg(t *testing.T) {
	c := Color{255, 0, 0}
	if fg := c.Fg(); !strings.Contains(fg, "38;2;255;0;0") {
		t.Errorf("Fg() should contain 38;2;255;0;0, got %q", fg)
	}
	if bg := c.Bg(); !strings.Contains(bg, "48;2;255;0;0") {
		t.Errorf("Bg() should contain 48;2;255;0;0, got %q", bg)
	}
}
