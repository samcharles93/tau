package taui

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// doRender acquires the render lock and draws one frame. Used by tests to
// force a render without managing the lock; production render timer calls
// renderLocked directly under the lock.
func (t *TUI) doRender() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.renderLocked()
}

// absPosRe matches absolute cursor positioning: CSI ... H (home / CUP).
// An inline renderer must never emit these — they anchor the frame to the top
// of the viewport instead of where it was first drawn.
var absPosRe = regexp.MustCompile("\x1b\\[[0-9;]*H")

// fakeTerm is a Terminal that records everything written to it and reports a
// fixed size, so renders are fully deterministic.
type fakeTerm struct {
	cols, rows int
	buf        strings.Builder
}

func newFakeTerm(cols, rows int) *fakeTerm { return &fakeTerm{cols: cols, rows: rows} }

func (f *fakeTerm) out() string { return f.buf.String() }
func (f *fakeTerm) reset()      { f.buf.Reset() }

func (f *fakeTerm) Start(onInput func(data string), onResize func()) {}
func (f *fakeTerm) SignalStop()                                      {}
func (f *fakeTerm) Stop()                                            {}
func (f *fakeTerm) Write(data string)                                { f.buf.WriteString(data) }
func (f *fakeTerm) Size() (int, int)                                 { return f.cols, f.rows }
func (f *fakeTerm) HideCursor()                                      {}
func (f *fakeTerm) ShowCursor()                                      {}
func (f *fakeTerm) MoveBy(int)                                       {}
func (f *fakeTerm) ClearLine()                                       {}
func (f *fakeTerm) ClearToEnd()                                      {}
func (f *fakeTerm) ClearScreen()                                     {}
func (f *fakeTerm) SetTitle(string)                                  {}

// assertNoScreenWipe fails if the output clears the screen or scrollback —
// destructive for an inline frame that shares the screen with shell history.
func assertNoScreenWipe(t *testing.T, label, out string) {
	t.Helper()
	for _, bad := range []string{"\x1b[2J", "\x1b[3J"} {
		if strings.Contains(out, bad) {
			t.Errorf("%s wiped the screen with %q: %q", label, bad, out)
		}
	}
}

// TestFirstRenderIsInline verifies the first paint draws at the current cursor
// position without homing or clearing the screen.
func TestFirstRenderIsInline(t *testing.T) {
	term := newFakeTerm(40, 12)
	engine := NewTUI(term)
	engine.AddChild(NewText("hello"))

	engine.doRender()
	out := term.out()

	if absPosRe.MatchString(out) {
		t.Errorf("first render used absolute positioning: %q", out)
	}
	assertNoScreenWipe(t, "first render", out)
	if !strings.Contains(out, "hello") {
		t.Errorf("first render did not contain the content: %q", out)
	}
}

// TestUpdateUsesRelativePositioning is the regression test for the cursor-jump
// bug: re-rendering after a content change must move the cursor only relative
// to its current row (CUU/CUD), never via absolute positioning, so the frame
// stays where it was first drawn.
func TestUpdateUsesRelativePositioning(t *testing.T) {
	term := newFakeTerm(40, 12)
	engine := NewTUI(term)
	line0 := NewText("line zero")
	engine.AddChild(line0)
	engine.AddChild(NewText("line one"))
	engine.AddChild(NewText("line two"))

	engine.doRender() // first paint, cursor parks at frame top (row 0)
	if engine.cursorRow != 0 {
		t.Fatalf("after first paint cursorRow = %d, want 0", engine.cursorRow)
	}
	term.reset()

	// Change the *top* line. The renderer is at the frame top already, so it
	// needs no vertical move to reach the changed line — it emits \r directly,
	// rewrites the line in place, and parks back at the top.
	line0.SetText("line ZERO")
	engine.doRender()
	out := term.out()

	if absPosRe.MatchString(out) {
		t.Fatalf("update used absolute positioning (the cursor-jump bug): %q", out)
	}
	assertNoScreenWipe(t, "update", out)
	if !strings.Contains(out, "line ZERO") {
		t.Errorf("update did not redraw the changed line: %q", out)
	}
}

// TestUnchangedRenderEmitsNothing verifies a render with no content change is a
// no-op (beyond the bookkeeping), so idle redraws don't churn the terminal.
func TestUnchangedRenderEmitsNothing(t *testing.T) {
	term := newFakeTerm(40, 12)
	engine := NewTUI(term)
	engine.AddChild(NewText("static"))

	engine.doRender()
	term.reset()
	engine.doRender()

	if out := term.out(); out != "" {
		t.Errorf("unchanged re-render emitted output: %q", out)
	}
}

// TestPrintAboveStreamsAndRedraws verifies the StreamAbove pattern: PrintAbove
// commits a line into scrollback above the frame (clearing the old frame from
// the cursor down) and then redraws the frame — all without absolute
// positioning, so the frame keeps its inline anchor.
func TestPrintAboveStreamsAndRedraws(t *testing.T) {
	term := newFakeTerm(40, 12)
	engine := NewTUI(term)
	engine.AddChild(NewText("frame line one"))
	engine.AddChild(NewText("frame line two"))

	engine.doRender() // paint the frame
	term.reset()

	engine.PrintAbovef("streamed: %s", "hello")
	out := term.out()

	if !strings.Contains(out, "streamed: hello") {
		t.Errorf("PrintAbove did not emit the streamed line: %q", out)
	}
	if !strings.Contains(out, "\x1b[0J") {
		t.Errorf("PrintAbove did not clear the old frame (\\x1b[0J): %q", out)
	}
	if absPosRe.MatchString(out) {
		t.Errorf("PrintAbove used absolute positioning: %q", out)
	}
	assertNoScreenWipe(t, "PrintAbove", out)
	// The frame must be redrawn after the streamed line.
	idx := strings.Index(out, "streamed: hello")
	if i := strings.Index(out, "frame line one"); i < idx {
		t.Errorf("frame was not redrawn below the streamed line: %q", out)
	}
	// And the engine still tracks the frame for the next diff.
	if len(engine.prevLines) != 2 {
		t.Errorf("after PrintAbove prevLines = %d, want 2 (frame retracked)", len(engine.prevLines))
	}
}

func TestUpdateThenPrintMutatesThenCommits(t *testing.T) {
	term := newFakeTerm(40, 12)
	engine := NewTUI(term)
	stage := &Container{}
	stage.AddChild(NewText("in-frame"))
	engine.AddChild(stage)
	engine.doRender()
	term.reset()

	// The closure mutates the tree (clears the stage) and returns a line to
	// commit. Crucially, calling this must not deadlock — it would if the
	// renderer let us PrintAbove while holding the lock.
	done := make(chan struct{})
	go func() {
		engine.UpdateThenPrint(func() []string {
			stage.Clear()
			return []string{"committed line"}
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("UpdateThenPrint deadlocked")
	}

	out := term.out()
	if !strings.Contains(out, "committed line") {
		t.Errorf("returned line was not printed to scrollback: %q", out)
	}
	if strings.Contains(out, "in-frame") {
		t.Errorf("stage was not cleared before repaint: %q", out)
	}
}

// TestResizeUsesClearToEndScreen verifies that on a width/height change, the
// renderer clears from the frame top to the end of the screen (\x1b[0J) before
// redrawing. This prevents the "staggered copies" artifact visible during a
// terminal-resize drag, where reflowed frame lines from a previous width
// remain on screen.
func TestResizeUsesClearToEndScreen(t *testing.T) {
	term := newFakeTerm(60, 12)
	engine := NewTUI(term)
	engine.AddChild(NewText("hello"))

	engine.doRender() // first paint at 60-col width
	term.reset()

	// Simulate a narrower resize: the old frame would reflow on a real
	// terminal, but we truncate and use \x1b[0J.
	term.cols = 30
	term.rows = 12
	engine.doRender()
	out := term.out()

	if !strings.Contains(out, "\x1b[0J") {
		t.Errorf("resize did not use \\x1b[0J to clear debris: %q", out)
	}
	if absPosRe.MatchString(out) {
		t.Errorf("resize used absolute positioning: %q", out)
	}
	// The re-rendered frame should still contain the content.
	if !strings.Contains(out, "hello") {
		t.Errorf("resize frame is missing the content: %q", out)
	}
}

// TestShrinkClearsOrphanLinesRelatively verifies that when the frame shrinks,
// the orphaned rows are cleared by a fullRewrite (which uses \x1b[0J from the
// top anchor) without absolute positioning or screen wipes.
func TestShrinkClearsOrphanLinesRelatively(t *testing.T) {
	term := newFakeTerm(40, 12)
	engine := NewTUI(term)
	a := NewText("a")
	b := NewText("b")
	c := NewText("c")
	engine.AddChild(a)
	engine.AddChild(b)
	engine.AddChild(c)

	engine.doRender() // three lines
	term.reset()

	// Drop to a single line.
	engine.RemoveChild(b)
	engine.RemoveChild(c)
	engine.doRender()
	out := term.out()

	if absPosRe.MatchString(out) {
		t.Fatalf("shrink used absolute positioning: %q", out)
	}
	assertNoScreenWipe(t, "shrink", out)
	// Shrink triggers a fullRewrite which uses \x1b[0J to clear from the frame
	// top to the end of the screen — this is not a screen wipe (2J/3J).
	if !strings.Contains(out, "\x1b[0J") {
		t.Errorf("shrink did not use \\x1b[0J to clear the old frame: %q", out)
	}
	if engine.cursorRow != 0 {
		t.Errorf("after shrink to one line, cursorRow = %d, want 0", engine.cursorRow)
	}
}

// TestFocusedDefaultsTrue verifies a fresh engine reports focused, so a
// terminal that never sends CSI ?1004 reports (no focus-tracking support)
// doesn't permanently suppress notifications gated on Focused().
func TestFocusedDefaultsTrue(t *testing.T) {
	engine := NewTUI(newFakeTerm(80, 24))
	if !engine.Focused() {
		t.Error("Focused() = false on a fresh engine, want true")
	}
}

// TestDispatchKeyTracksFocusReports verifies CSI ?1004 focus in/out reports
// update Focused() and are consumed rather than forwarded to the focused
// component as ordinary keystrokes.
func TestDispatchKeyTracksFocusReports(t *testing.T) {
	engine := NewTUI(newFakeTerm(80, 24))

	engine.dispatchKey("\x1b[O")
	if engine.Focused() {
		t.Error("after focus-out report, Focused() = true, want false")
	}

	engine.dispatchKey("\x1b[I")
	if !engine.Focused() {
		t.Error("after focus-in report, Focused() = false, want true")
	}
}
