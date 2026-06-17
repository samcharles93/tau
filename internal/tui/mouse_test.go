package tui

import (
	"testing"

	gt "github.com/grindlemire/go-tui"
)

// TestHandleMouseWheelScrolls verifies the panel-level wheel handler scrolls the
// message viewport (the fix for wheel-over-content doing nothing because
// hit-testing resolves to a non-scrollable text leaf).
func TestHandleMouseWheelScrolls(t *testing.T) {
	const w, h = 120, 16
	buf := gt.NewBuffer(w, h)

	panel := newTestPanel(&fakeRuntime{})
	panel.messages.Set(tallTranscript())

	vp := panel.renderMessagesViewport(nil)
	vp.Render(buf, w, h)
	_, maxY := vp.MaxScroll()
	if maxY < wheelScrollLines {
		t.Fatalf("transcript does not overflow enough to test wheel scroll (maxY=%d)", maxY)
	}
	if _, y := vp.ScrollOffset(); y != maxY {
		t.Fatalf("first render should pin to bottom: want %d, got %d", maxY, y)
	}

	// Wheel up scrolls up and is consumed.
	if !panel.HandleMouse(gt.MouseEvent{Button: gt.MouseWheelUp}) {
		t.Fatal("wheel up should be consumed")
	}
	if _, y := vp.ScrollOffset(); y != maxY-wheelScrollLines {
		t.Fatalf("wheel up: want scrollY %d, got %d", maxY-wheelScrollLines, y)
	}

	// Wheel down scrolls back toward the bottom and is consumed.
	if !panel.HandleMouse(gt.MouseEvent{Button: gt.MouseWheelDown}) {
		t.Fatal("wheel down should be consumed")
	}
	if _, y := vp.ScrollOffset(); y != maxY {
		t.Fatalf("wheel down: want scrollY %d, got %d", maxY, y)
	}

	// Non-wheel events are left unconsumed so terminal selection still works.
	if panel.HandleMouse(gt.MouseEvent{Button: gt.MouseLeft}) {
		t.Fatal("left button should not be consumed by the wheel handler")
	}
}

// TestHandleMouseIgnoredWhenOverlayOpen verifies the wheel is left alone (and
// the chat is not scrolled) while a full-screen overlay owns the screen.
func TestHandleMouseIgnoredWhenOverlayOpen(t *testing.T) {
	const w, h = 120, 16
	buf := gt.NewBuffer(w, h)

	panel := newTestPanel(&fakeRuntime{})
	panel.messages.Set(tallTranscript())
	vp := panel.renderMessagesViewport(nil)
	vp.Render(buf, w, h)
	vp.ScrollBy(0, -2)
	_, before := vp.ScrollOffset()

	panel.showHelp.Set(true)
	if panel.HandleMouse(gt.MouseEvent{Button: gt.MouseWheelUp}) {
		t.Fatal("wheel should not be consumed while an overlay is open")
	}
	if _, after := vp.ScrollOffset(); after != before {
		t.Fatalf("overlay open: scroll must not change (before=%d after=%d)", before, after)
	}
}
