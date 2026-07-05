package taui

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config holds user-facing knobs for the TUI engine. It follows tau's
// configuration pattern — a plain struct with defaults for the zero value.
type Config struct {
	// DefaultToolStyle is the ToolStyle applied to ToolRow components created
	// via NewToolRow. ToolStyleCombined (the zero value) composes on coloured
	// boxes; ToolStyleBadge uses bg-chip badges.
	DefaultToolStyle ToolStyle

	// CursorColor is the background colour for the block cursor in LineInput.
	// Zero means use the default (mid-grey, rgb 128/134/150).
	CursorColor [3]uint8
}

// TUI is the main engine — it owns the component tree, the terminal, and the
// differential render loop. Ported from Pi's tui.ts TUI class.
type TUI struct {
	Container
	Terminal Terminal

	// Config holds user-facing options. Safe to read from any goroutine after
	// construction; do not mutate without holding mu.
	Config Config

	// mu guards all render state below. Renders fire from a timer goroutine,
	// input arrives on the stdin goroutine, and application code (PrintAbove,
	// RequestRender, Stop) runs on its own goroutines — mu serializes them so
	// the frame bookkeeping and terminal writes never interleave.
	mu sync.Mutex

	// Render state.
	//
	// The renderer draws the frame inline — wherever the cursor happens to be,
	// never on the alternate screen. After every render the cursor is parked at
	// the frame's top line (cursorRow=0). This is the resize-safe anchor: from
	// a known position, \x1b[0J clears the old frame (and any wrapping debris)
	// to the end of the screen before redrawing. All movement is relative
	// (CUU/CUD); absolute positioning (\x1b[H) is forbidden — it would jump
	// the frame to the top of the viewport.
	//
	// Frame lines are truncated to width so one logical line = one physical
	// row. This is mandatory for the two invariants above: a narrow terminal
	// would otherwise reflow lines, breaking the cursorRow→physical-row
	// correspondence and making relative movement unreliable.
	prevLines     []string
	prevWidth     int
	prevHeight    int
	cursorRow     int
	renderPending bool
	renderTimer   *time.Timer
	lastRender    time.Time

	// Focus
	focused Component

	// termFocused mirrors the terminal window's own focus state, from CSI
	// ?1004 reports (see ProcessTerminal.Start/Stop). Defaults to true: a
	// terminal without focus-reporting support never sends a report, so
	// callers gating on Focused() (e.g. desktop notifications) simply always
	// fire on those terminals rather than silently never firing.
	termFocused atomic.Bool

	// Input segmentation (paste handling + batched-key splitting).
	input *stdinBuffer

	started bool
	stopped bool
	done    chan struct{}
}

const minRenderInterval = 16 * time.Millisecond

// NewTUI creates a TUI engine with the given terminal and default config.
func NewTUI(term Terminal) *TUI {
	t := &TUI{
		Terminal: term,
		done:     make(chan struct{}),
	}
	t.termFocused.Store(true)
	return t
}

// NewTUIWithConfig creates a TUI engine with custom configuration.
func NewTUIWithConfig(term Terminal, cfg Config) *TUI {
	t := &TUI{
		Terminal: term,
		Config:   cfg,
		done:     make(chan struct{}),
	}
	t.termFocused.Store(true)
	return t
}

// Focused reports whether the terminal window currently has focus.
func (t *TUI) Focused() bool {
	return t.termFocused.Load()
}

// SetFocus sets the focused component.
func (t *TUI) SetFocus(c Component) {
	if f, ok := t.focused.(Focusable); ok {
		f.SetFocused(false)
	}
	t.focused = c
	if f, ok := c.(Focusable); ok {
		f.SetFocused(true)
	}
}

// Start begins the render loop and input handling. It is not safe to call
// multiple times — subsequent calls after the first are no-ops.
func (t *TUI) Start() {
	t.mu.Lock()
	if t.started {
		t.mu.Unlock()
		return
	}
	t.started = true
	t.mu.Unlock()

	t.input = newStdinBuffer(t.dispatchKey, t.dispatchPaste)
	t.Terminal.Start(
		func(data string) { t.input.process(data) },
		func() { t.RequestRender() },
	)
	t.Terminal.HideCursor()
	t.RequestRender()

	// Block until stopped.
	<-t.done
}

// Stop halts the render loop and restores terminal state.
func (t *TUI) Stop() {
	// Signal the terminal's read loop to exit before we do anything else.
	// This is safe to call multiple times (idempotent via sync.Once).
	// For the /exit path (and other programmatic stops) this runs in a
	// spawned goroutine, but it's the first thing it does — so the window
	// between dispatchKey returning and the read loop checking stopCh is
	// as small as possible. For the Ctrl+C path, dispatchKey calls
	// SignalStop synchronously before spawning us, so the channel is
	// already closed.
	t.Terminal.SignalStop()

	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return
	}
	t.stopped = true

	if t.renderTimer != nil {
		t.renderTimer.Stop()
	}

	// Move the cursor below the frame so the shell prompt lands after our
	// content. The cursor is parked at the frame top, so step down to the last
	// line first, then onto a fresh line — all relative, never absolute.
	if n := len(t.prevLines); n > 0 {
		if n > 1 {
			t.Terminal.Write(fmt.Sprintf("\x1b[%dB", n-1))
		}
		t.Terminal.Write("\r\n")
	}
	// Release the lock before Terminal.Stop() so the resize signal goroutine
	// (which calls RequestRender → t.mu.Lock) can observe t.stopped and exit,
	// unblocking t.wg.Wait() inside Terminal.Stop().
	t.mu.Unlock()

	t.Terminal.ShowCursor()
	t.Terminal.Stop()
	close(t.done)
}

// RequestRender schedules a re-render. If force is true, the diff cache is
// cleared so the entire screen is redrawn.
func (t *TUI) RequestRender() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped || t.renderPending {
		return
	}
	t.renderPending = true

	elapsed := time.Since(t.lastRender)
	delay := max(0, minRenderInterval-elapsed)
	if t.renderTimer != nil {
		t.renderTimer.Stop()
	}
	t.renderTimer = time.AfterFunc(delay, func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if t.stopped {
			return
		}
		t.renderPending = false
		t.lastRender = time.Now()
		t.renderLocked()
	})
}

// dispatchKey handles a single discrete key sequence emitted by the input
// buffer.
func (t *TUI) dispatchKey(seq string) {
	// Normalize Kitty-protocol disambiguated keys back to their legacy bytes so
	// components (and existing `case "\x1b"` handlers) stay terminal-agnostic.
	seq = canonicalKey(seq)

	// Focus-in/out reports (CSI ?1004, enabled in ProcessTerminal.Start) aren't
	// keystrokes — update termFocused and stop rather than forwarding to the
	// focused component.
	switch seq {
	case "\x1b[I":
		t.termFocused.Store(true)
		return
	case "\x1b[O":
		t.termFocused.Store(false)
		return
	}

	if h, ok := t.focused.(InputHandler); ok {
		if h.HandleInput(seq) {
			t.RequestRender()
		}
	}
}

// dispatchPaste delivers a bracketed-paste payload to the focused component if
// it accepts pastes. Components that don't implement PasteHandler simply ignore
// pastes rather than receiving the raw bracket markers as keystrokes.
func (t *TUI) dispatchPaste(content string) {
	if h, ok := t.focused.(PasteHandler); ok {
		if h.HandlePaste(content) {
			t.RequestRender()
		}
	}
}

// renderLocked draws one frame. The caller must hold t.mu.
func (t *TUI) renderLocked() {
	width, height := t.Terminal.Size()
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}

	// Render the full component tree.
	newLines := t.Render(width)

	// If the root is a Box with Expand, fill remaining height.
	if len(t.Children) == 1 {
		if box, ok := t.Children[0].(*Box); ok {
			newLines = box.Fill(newLines, width, height)
		}
	}

	// Prevent the live frame from exceeding terminal height. During streaming
	// the frame can grow very tall (hundreds of response lines), which would
	// push the input and status past the viewport and into the scrollback
	// buffer during fullRewrite. Truncating from the top keeps the visible
	// portion anchored to the bottom (input + status) while the oldest
	// response lines scroll off.
	if len(newLines) > height {
		newLines = newLines[len(newLines)-height:]
	}

	// Truncate every line to width so one logical line = one physical row (the
	// renderer's core invariant), then append line-end resets.
	for i, line := range newLines {
		newLines[i] = truncateANSIToWidth(line, width) + "\x1b[0m"
	}

	// First render, resize, or the frame shrank (component removed lines):
	// rewrite the whole frame from a known anchor, \x1b[0J clearing debris.
	// A shrink happens when the input backspaces over a newline and the
	// wrapped-line count drops — without a full rewrite the orphaned rows
	// would linger as ghosted text.
	if len(t.prevLines) == 0 ||
		t.prevWidth != width || t.prevHeight != height ||
		len(newLines) < len(t.prevLines) {
		t.fullRewrite(newLines, width, height)
		return
	}

	// Differential render — only redraw the changed band of lines.
	t.diffRender(newLines, width, height)
}

// fullRewrite redraws every line of the frame from a known anchor — the
// cursor is parked at the frame's top line on entry (and parked back there on
// exit). On resize, \x1b[0J from the top clears the entire old frame including
// any reflow/wrapping debris; on first paint it is harmless. Because the
// operating area is erased to the end of the screen, shrinking causes no
// leftover rows.
func (t *TUI) fullRewrite(newLines []string, width, height int) {
	var buf strings.Builder
	buf.WriteString("\x1b[?2026h") // Begin synchronized output
	buf.WriteString("\r")
	buf.WriteString("\x1b[0J") // Clear from frame top to end of screen

	for i, line := range newLines {
		if i > 0 {
			buf.WriteString("\r\n")
		}
		buf.WriteString(line)
	}

	// Park cursor back at the frame top.
	if last := len(newLines) - 1; last > 0 {
		fmt.Fprintf(&buf, "\x1b[%dA", last)
	}
	buf.WriteString("\x1b[?2026l") // End synchronized output
	t.Terminal.Write(buf.String())

	t.prevLines = newLines
	t.prevWidth = width
	t.prevHeight = height
	t.cursorRow = 0
}

func (t *TUI) diffRender(newLines []string, width, height int) {
	// Find the changed band.
	firstChange := -1
	lastChange := -1
	maxLen := max(len(newLines), len(t.prevLines))
	for i := range maxLen {
		oldLine := ""
		newLine := ""
		if i < len(t.prevLines) {
			oldLine = t.prevLines[i]
		}
		if i < len(newLines) {
			newLine = newLines[i]
		}
		if oldLine != newLine {
			if firstChange < 0 {
				firstChange = i
			}
			lastChange = i
		}
	}

	appended := len(newLines) > len(t.prevLines)
	if appended {
		if firstChange < 0 {
			firstChange = len(t.prevLines)
		}
		lastChange = len(newLines) - 1
	}

	if firstChange < 0 {
		return // No changes.
	}

	// appendStart: the change is purely new lines tacked onto the end. Step
	// onto the last existing line, then emit a newline to open fresh rows.
	appendStart := appended && firstChange == len(t.prevLines) && firstChange > 0

	var buf strings.Builder
	buf.WriteString("\x1b[?2026h") // Begin synchronized output

	// Cursor is at the frame top (cursorRow=0). Step down to the first line we
	// need to touch — all relative, so the frame stays anchored.
	moveTarget := firstChange
	if appendStart {
		moveTarget = firstChange - 1
	}
	if moveTarget > 0 {
		fmt.Fprintf(&buf, "\x1b[%dB", moveTarget)
	}
	if appendStart {
		buf.WriteString("\r\n")
	} else {
		buf.WriteString("\r")
	}

	// Draw the changed lines, tracking where the cursor ends up.
	renderEnd := min(lastChange, len(newLines)-1)
	for i := firstChange; i <= renderEnd; i++ {
		if i > firstChange {
			buf.WriteString("\r\n")
		}
		buf.WriteString("\x1b[2K") // Clear line
		buf.WriteString(newLines[i])
	}
	finalPhysicalRow := renderEnd // cursor sits here after drawing

	// If the previous frame was taller, clear the orphaned rows below, then
	// step back up to the top.
	if len(t.prevLines) > len(newLines) {
		if renderEnd < len(newLines)-1 {
			down := len(newLines) - 1 - renderEnd
			fmt.Fprintf(&buf, "\x1b[%dB", down)
			finalPhysicalRow = len(newLines) - 1
		}
		extra := len(t.prevLines) - len(newLines)
		for range extra {
			buf.WriteString("\r\n\x1b[2K")
		}
		finalPhysicalRow += extra
	}

	// Park cursor back at the frame top.
	if finalPhysicalRow > 0 {
		fmt.Fprintf(&buf, "\x1b[%dA", finalPhysicalRow)
	}
	buf.WriteString("\x1b[?2026l") // End synchronized output
	t.Terminal.Write(buf.String())

	t.prevLines = newLines
	t.prevWidth = width
	t.prevHeight = height
	t.cursorRow = 0
}

// ─────────────────────────────────────────────────────────────────────────────
// StreamAbove — write persistent output above the pinned inline frame
// ─────────────────────────────────────────────────────────────────────────────

// PrintAbove writes a line into the scrollback *above* the inline frame, then
// redraws the frame beneath it. This is the "stream above a fixed frame"
// pattern (go-tui's StreamAbove): persistent output — assistant tokens,
// completed tool summaries — accumulates in history while the live frame stays
// pinned at the bottom.
//
// It lifts the frame (stepping up relative to the cursor, then clearing from
// there to the end of the screen), commits the line into history, and lets the
// normal render path repaint the frame on the next line. All movement is
// relative, so the frame keeps its inline anchor. The text may contain ANSI
// styling and embedded newlines.
func (t *TUI) PrintAbovef(format string, args ...any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped {
		return
	}

	// Normalize newlines to CRLF: in raw mode a bare \n moves down without
	// returning to column 0, so multi-line history would stair-step.
	text := fmt.Sprintf(format, args...)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\n", "\r\n")

	var buf strings.Builder
	buf.WriteString("\x1b[?2026h") // Begin synchronized output
	// Cursor is at the frame top. Clear the frame from there down, then commit
	// the text into the scrollback above.
	buf.WriteString("\r")
	buf.WriteString("\x1b[0J") // Clear from frame top to end of screen
	buf.WriteString(text)
	buf.WriteString("\r\n")        // Commit the line into scrollback
	buf.WriteString("\x1b[?2026l") // End synchronized output
	t.Terminal.Write(buf.String())

	// The frame no longer exists on screen; force a fresh redraw at the new
	// cursor position, just below the line we committed.
	t.prevLines = nil
	t.cursorRow = 0
	t.renderLocked()
}

// PrintAboveStyled is retained for compatibility; PrintAbove already preserves
// ANSI styling in the format string.
func (t *TUI) PrintAboveStyledf(format string, args ...any) {
	t.PrintAbovef(format, args...)
}

// PrintAbove delegates to PrintAbovef (backward-compat alias).
//
//nolint:goprintffuncname
func (t *TUI) PrintAbove(format string, args ...any) { t.PrintAbovef(format, args...) }

// PrintAboveStyled delegates to PrintAboveStyledf (backward-compat alias).
//
//nolint:goprintffuncname
func (t *TUI) PrintAboveStyled(format string, args ...any) { t.PrintAboveStyledf(format, args...) }

// Update runs fn while holding the render lock, then repaints once. Use it to
// mutate the component tree (add/remove children) or any component that is not
// internally synchronized, so structural changes never race the renderer. fn
// must not call other TUI methods that take the lock (PrintAbove,
// RequestRender, Update, Stop) — that would deadlock.
func (t *TUI) Update(fn func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped {
		return
	}
	fn()
	t.renderLocked()
}

// UpdateThenPrint runs fn under the render lock to mutate the component tree and
// collect scrollback output, repaints the frame, then prints the lines fn
// returns ABOVE the frame after the lock is released.
//
// It exists to make a deadlock unrepresentable: calling PrintAbove (or any
// lock-taking method) inside an Update closure deadlocks because both take the
// render lock. Returning the lines instead lets the renderer own the ordering —
// mutate + repaint under the lock, then commit to scrollback once it's free.
// Each returned line is printed verbatim, so callers bake in their own styling
// and trailing newlines.
func (t *TUI) UpdateThenPrint(fn func() []string) {
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return
	}
	above := fn()
	t.renderLocked()
	t.mu.Unlock()

	for _, line := range above {
		t.PrintAbovef("%s", line)
	}
}
