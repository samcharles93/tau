package termkit

import (
	"fmt"
	"io"
	"os"
	"time"
)

// ToolLifecycle renders the three-state inline tool-call lifecycle:
// an animated RUNNING line that is overwritten in place, resolving to either
// SUCCESS or FAILURE. It writes to an io.Writer (e.g. a go-tui StreamWriter
// or os.Stdout).
//
// Two modes are available:
//
//	// Determinate - spinner + progress bar (for tools with a known step count):
//	tl := NewToolLifecycle("go build", "./...", w)
//
//	// Indeterminate - spinner only (for tools with unknown duration):
//	tl := NewIndeterminateToolLifecycle("fetch", "https://api.example.com", w)
//
//	tl.Start()
//	for !done {
//	    tl.Tick()
//	    time.Sleep(40 * time.Millisecond)
//	}
//	tl.Resolve(true, "done in 1.2s")
type ToolLifecycle struct {
	toolName      string
	args          string
	w             io.Writer
	indeterminate bool

	start   time.Time
	tick    int
	running bool
}

// NewToolLifecycle creates a determinate ToolLifecycle that shows a progress
// bar alongside the spinner. Use this for tools with a known or estimable duration.
func NewToolLifecycle(toolName, args string, w io.Writer) *ToolLifecycle {
	return &ToolLifecycle{
		toolName: toolName,
		args:     args,
		w:        w,
	}
}

// NewIndeterminateToolLifecycle creates a ToolLifecycle that shows only a
// spinner with elapsed time - no progress bar. Use this for tools whose
// duration is unknown (network calls, user prompts, etc.).
func NewIndeterminateToolLifecycle(toolName, args string, w io.Writer) *ToolLifecycle {
	return &ToolLifecycle{
		toolName:      toolName,
		args:          args,
		w:             w,
		indeterminate: true,
	}
}

// writer returns the io.Writer, defaulting to os.Stdout if nil.
func (tl *ToolLifecycle) writer() io.Writer {
	if tl.w == nil {
		return os.Stdout
	}
	return tl.w
}

// Start begins the running animation and shows the cursor hidden.
func (tl *ToolLifecycle) Start() {
	tl.start = time.Now()
	tl.running = true
	Emit(tl.writer(), HideCursor)
}

// Tick advances the animation one frame. Call from a timer goroutine.
// Tick is safe to call after Resolve (it is a no-op).
func (tl *ToolLifecycle) Tick() {
	if !tl.running {
		return
	}
	frame := SpinnerFrame(tl.tick)
	tl.tick++

	elapsed := time.Since(tl.start).Seconds()

	if !ColorEnabled() {
		return
	}

	label := Style(" RUNNING ", ColorAmber.Bg(), ColorYellow.Fg(), Bold)

	if tl.indeterminate {
		// Spinner-only: no progress bar, just spinner + timer.
		fmt.Fprintf(
			tl.writer(), "%s%s %s %s%s %s(%s) %s%.1fs%s",
			ClearLine, frame, label,
			Bold, tl.toolName,
			ColorGrey.Fg(), tl.args, ColorGrey.Fg(), elapsed,
			Reset,
		)
	} else {
		bar := Style(ProgressBar(float64(tl.tick%30)/30.0, 20), ColorYellow.Fg())
		fmt.Fprintf(
			tl.writer(), "%s%s %s %s %s%s %s(%s) %s%.1fs%s",
			ClearLine, frame, label, bar,
			Bold, tl.toolName,
			ColorGrey.Fg(), tl.args, ColorGrey.Fg(), elapsed,
			Reset,
		)
	}
}

// Resolve finishes the animation: clears the running line and prints
// either a SUCCESS or FAILURE badge with timing and details.
func (tl *ToolLifecycle) Resolve(ok bool, detail string) {
	tl.running = false
	Emit(tl.writer(), ClearLine)
	Emit(tl.writer(), ShowCursor)

	elapsed := time.Since(tl.start).Seconds()
	meta := Style("("+tl.args+")", ColorGrey.Fg())

	if ok {
		badge := Style(" SUCCESS ", ColorGreen.Bg(), ColorObsidian.Fg(), Bold)
		timing := Style(fmt.Sprintf("- %s", detail), ColorGrey.Fg())
		if detail == "" {
			timing = Style(fmt.Sprintf("- done in %.1fs", elapsed), ColorGrey.Fg())
		}
		fmt.Fprintf(tl.writer(), "%s %s %s %s\n",
			badge, Style(tl.toolName, Bold), meta, timing)
	} else {
		badge := Style(" FAILED  ", ColorRed.Bg(), ColorWhite.Fg(), Bold)
		logLink := Style(Hyperlink("view log", "https://example.com/logs/"+tl.toolName), Underline)
		tail := Style(fmt.Sprintf("- %s ·", detail), ColorGrey.Fg())
		if detail == "" {
			tail = Style("- exit 1 ·", ColorGrey.Fg())
		}
		fmt.Fprintf(tl.writer(), "%s %s %s %s %s\n",
			badge, Style(tl.toolName, Bold), meta, tail, logLink)
	}
}
