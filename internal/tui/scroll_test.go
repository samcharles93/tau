package tui

import (
	"strings"
	"testing"

	gt "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
)

// tallTranscript returns a transcript whose rendered height comfortably exceeds
// a short viewport, so the scroll container has a non-zero scroll range.
func tallTranscript() []tauchat.ChatMessage {
	para := strings.Repeat("This is a fairly long paragraph that wraps across several lines when constrained to the message width inside the scroll viewport. ", 4)
	return []tauchat.ChatMessage{
		{Role: tauchat.ChatRoleUser, Content: "hello"},
		{Role: tauchat.ChatRoleAssistant, Content: para + "\n\n" + para + "\n\n" + para + "\n\n" + para},
	}
}

// TestViewportReusedAcrossRenders guards the core regression: the scrollable
// viewport element must be reused across renders (not recreated), because the
// scroll offset lives on the element and go-tui rebuilds the tree every frame.
func TestViewportReusedAcrossRenders(t *testing.T) {
	panel := newTestPanel(&fakeRuntime{})
	panel.messages.Set(tallTranscript())

	vp1 := panel.renderMessagesViewport(nil)
	vp2 := panel.renderMessagesViewport(nil)

	if vp1 != vp2 {
		t.Fatal("renderMessagesViewport returned a new element on re-render; " +
			"scroll offset would be lost every frame")
	}
}

// TestViewportScrollOffsetSurvivesRerender is the behavioural regression test:
// after the user scrolls up, a plain re-render (e.g. the once-per-second notify
// timer) must not reset the scroll position to the top.
func TestViewportScrollOffsetSurvivesRerender(t *testing.T) {
	const w, h = 120, 16
	buf := gt.NewBuffer(w, h)

	panel := newTestPanel(&fakeRuntime{})
	panel.messages.Set(tallTranscript())

	// First render starts pinned at the bottom.
	vp := panel.renderMessagesViewport(nil)
	vp.Render(buf, w, h)
	_, maxY := vp.MaxScroll()
	if maxY == 0 {
		t.Fatalf("test transcript does not overflow viewport (maxY=0); cannot exercise scrolling")
	}
	if _, y := vp.ScrollOffset(); y != maxY {
		t.Fatalf("first render: want scrollY==maxY (%d), got %d", maxY, y)
	}

	// User scrolls up two lines.
	vp.ScrollBy(0, -2)
	_, want := vp.ScrollOffset()
	if want == 0 || want == maxY {
		t.Fatalf("setup: expected an intermediate scroll offset, got %d (maxY=%d)", want, maxY)
	}

	// A plain re-render with unchanged content must preserve the position.
	vp = panel.renderMessagesViewport(nil)
	vp.Render(buf, w, h)
	if _, got := vp.ScrollOffset(); got != want {
		t.Fatalf("scroll offset not preserved across re-render: want %d, got %d (the recreate-every-frame bug)", want, got)
	}
}

// TestViewportStickyBottomFollowsNewContent verifies that when the user is
// pinned at the bottom, new content keeps the view at the bottom; and when they
// have scrolled up, new content does not yank them down.
func TestViewportStickyBottomFollowsNewContent(t *testing.T) {
	const w, h = 120, 16
	buf := gt.NewBuffer(w, h)

	panel := newTestPanel(&fakeRuntime{})
	msgs := tallTranscript()
	panel.messages.Set(msgs)

	vp := panel.renderMessagesViewport(nil)
	vp.Render(buf, w, h)
	if !vp.IsAtBottom() {
		t.Fatal("first render should start at the bottom")
	}

	// New content arrives while pinned at the bottom -> should follow.
	panel.messages.Set(append(msgs, tauchat.ChatMessage{Role: tauchat.ChatRoleAssistant, Content: "one more line of reply"}))
	vp = panel.renderMessagesViewport(nil)
	vp.Render(buf, w, h)
	if !vp.IsAtBottom() {
		t.Fatal("sticky bottom: view should follow new content while pinned at the bottom")
	}

	// Scroll up, then more content arrives -> position must be preserved.
	vp.ScrollBy(0, -3)
	_, want := vp.ScrollOffset()
	panel.messages.Set(append(msgs, tauchat.ChatMessage{Role: tauchat.ChatRoleAssistant, Content: "and yet another reply line"}))
	vp = panel.renderMessagesViewport(nil)
	vp.Render(buf, w, h)
	if _, got := vp.ScrollOffset(); got != want {
		t.Fatalf("scrolled-up user should not be yanked to the bottom: want %d, got %d", want, got)
	}
}
