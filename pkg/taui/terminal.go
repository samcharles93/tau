package taui

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// Terminal abstracts the terminal device. Ported from Pi's terminal.ts.
type Terminal interface {
	// Start begins listening for input and resize events.
	Start(onInput func(data string), onResize func())

	// Stop restores terminal state.
	Stop()

	// SignalStop closes the stop channel to request the stdin and resize
	// goroutines exit, without waiting for them. It is safe to call multiple
	// times and from any goroutine. Call Stop afterwards to wait for the
	// goroutines and restore terminal state.
	SignalStop()

	// Write outputs data to the terminal.
	Write(data string)

	// Size returns the current terminal dimensions.
	Size() (cols, rows int)

	// Cursor control.
	HideCursor()
	ShowCursor()
	MoveBy(lines int)
	ClearLine()
	ClearToEnd()
	ClearScreen()
	SetTitle(title string)
}

// ProcessTerminal is the real terminal using os.Stdin / os.Stdout.
type ProcessTerminal struct {
	wasRaw   bool
	inputFn  func(data string)
	resizeFn func()
	stopCh   chan struct{}

	// stopOnce guards close(stopCh) so SignalStop and Stop are both safe to call
	// multiple times and from any goroutine.
	stopOnce sync.Once

	// Restore state
	originalTermios *TermiosState

	wg sync.WaitGroup // tracks stdin + resize goroutines for clean shutdown
}

// NewProcessTerminal creates a terminal backed by the real stdin/stdout.
func NewProcessTerminal() *ProcessTerminal {
	return &ProcessTerminal{}
}

// Start puts the terminal into raw mode and begins listening.
func (t *ProcessTerminal) Start(onInput func(data string), onResize func()) {
	t.inputFn = onInput
	t.resizeFn = onResize
	t.stopCh = make(chan struct{})

	// Save and set raw mode.
	if state, err := MakeRaw(os.Stdin.Fd()); err == nil {
		t.originalTermios = state
		t.wasRaw = true
	}

	// Enable bracketed paste.
	_, _ = os.Stdout.WriteString("\x1b[?2004h")

	// Enable focus reporting (CSI I on focus-in, CSI O on focus-out), so the
	// engine's Focused() reflects whether the terminal window is actually
	// active - used to gate desktop notifications. Terminals without support
	// just never send the reports, which leaves Focused() at its default of
	// true. Popped in Stop.
	_, _ = os.Stdout.WriteString("\x1b[?1004h")

	// Push the Kitty keyboard protocol (flag 1 = disambiguate escape codes) so
	// modified keys - Shift+Enter, Ctrl+Enter, etc. - arrive as unambiguous
	// CSI-u sequences instead of colliding with legacy encodings. Terminals
	// without support silently ignore this. Popped in Stop.
	_, _ = os.Stdout.WriteString(kittyKeyboardPush)

	// Hide cursor.
	_, _ = os.Stdout.WriteString(termkit.HideCursor)

	// Resize signal handling (SIGWINCH on Unix, no-op on Windows).
	sigCh := make(chan os.Signal, 1)
	if sigWINCH != nil {
		signal.Notify(sigCh, sigWINCH)
	}
	t.wg.Go(func() {
		for {
			select {
			case <-t.stopCh:
				signal.Stop(sigCh)
				close(sigCh)
				return
			case <-sigCh:
				if t.resizeFn != nil {
					t.resizeFn()
				}
			}
		}
	})

	// Read stdin in a goroutine.
	t.wg.Go(t.readStdin)
}

func (t *ProcessTerminal) readStdin() {
	buf := make([]byte, 4096)
	for {
		select {
		case <-t.stopCh:
			return
		default:
		}
		// Poll with a short deadline so Stop() unblocks this read within one
		// interval instead of leaking the goroutine until the next keypress.
		// Terminals that don't support deadlines ignore it and block as before.
		_ = os.Stdin.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, err := os.Stdin.Read(buf)
		if n > 0 && t.inputFn != nil {
			t.inputFn(string(buf[:n]))
		}
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				continue
			}
			return
		}
	}
}

// Kitty keyboard protocol push/pop. Flag 1 = disambiguate escape codes: enough
// to report modified keys (Shift+Enter, …) as CSI-u without sending key-release
// events or re-encoding plain text, so the simple input handler keeps working.
const (
	kittyKeyboardPush = "\x1b[>1u"
	kittyKeyboardPop  = "\x1b[<u"
)

// SignalStop closes the stop channel to request the stdin and resize
// goroutines exit, without waiting for them. It is idempotent (safe to call
// multiple times and from any goroutine). Call Stop afterwards to wait for
// the goroutines and restore terminal state.
func (t *ProcessTerminal) SignalStop() {
	t.stopOnce.Do(func() {
		close(t.stopCh)
	})
}

// Stop restores terminal state. It blocks until the stdin and resize
// goroutines have exited, guaranteeing no further reads or signal handlers
// fire after terminal settings are restored.
func (t *ProcessTerminal) Stop() {
	t.stopOnce.Do(func() {
		close(t.stopCh)
	})

	// Wait for goroutines to exit before restoring terminal state.
	t.wg.Wait()

	// Clear any read deadline left set by the input poll loop.
	_ = os.Stdin.SetReadDeadline(time.Time{})

	// Pop the Kitty keyboard protocol pushed in Start.
	_, _ = os.Stdout.WriteString(kittyKeyboardPop)

	// Disable focus reporting.
	_, _ = os.Stdout.WriteString("\x1b[?1004l")

	// Disable bracketed paste.
	_, _ = os.Stdout.WriteString("\x1b[?2004l")

	// Show cursor.
	_, _ = os.Stdout.WriteString(termkit.ShowCursor)

	// Restore terminal mode.
	if t.originalTermios != nil {
		_ = Restore(os.Stdin.Fd(), t.originalTermios)
	}
}

// Write outputs data to stdout.
func (t *ProcessTerminal) Write(data string) {
	_, _ = os.Stdout.WriteString(data)
}

// Size returns terminal dimensions, falling back to environment.
func (t *ProcessTerminal) Size() (cols, rows int) {
	if size, err := GetWinsize(os.Stdout.Fd()); err == nil {
		return int(size.Col), int(size.Row)
	}
	// Fallback via escape sequence query (simplified - just env vars).
	cols = 80
	rows = 24
	if c := os.Getenv("COLUMNS"); c != "" {
		_, _ = fmt.Sscanf(c, "%d", &cols)
	}
	if r := os.Getenv("LINES"); r != "" {
		_, _ = fmt.Sscanf(r, "%d", &rows)
	}
	return cols, rows
}

// Cursor control methods.

func (t *ProcessTerminal) HideCursor()  { _, _ = os.Stdout.WriteString(termkit.HideCursor) }
func (t *ProcessTerminal) ShowCursor()  { _, _ = os.Stdout.WriteString(termkit.ShowCursor) }
func (t *ProcessTerminal) ClearLine()   { _, _ = os.Stdout.WriteString(termkit.ClearLine) }
func (t *ProcessTerminal) ClearToEnd()  { _, _ = os.Stdout.WriteString(termkit.ClearToEnd) }
func (t *ProcessTerminal) ClearScreen() { _, _ = os.Stdout.WriteString("\x1b[2J\x1b[H") }

func (t *ProcessTerminal) MoveBy(lines int) {
	if lines > 0 {
		_, _ = fmt.Fprintf(os.Stdout, "\x1b[%dB", lines)
	} else if lines < 0 {
		_, _ = fmt.Fprintf(os.Stdout, "\x1b[%dA", -lines)
	}
}

func (t *ProcessTerminal) SetTitle(title string) {
	_, _ = fmt.Fprintf(os.Stdout, "\x1b]0;%s\x07", title)
}
