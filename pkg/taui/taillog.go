package taui

import (
	"strings"
	"sync"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// TailLog is a bounded, live-updating view of streamed text: it keeps only
// the last MaxLines lines, dropping older ones as new output arrives. Use it
// for tool stdout/stderr while a tool is running — unlike printing every
// chunk straight to scrollback, the tail stays a fixed size and is meant to
// be discarded (RemoveChild it from its parent) once the producer finishes,
// rather than committed to permanent output.
//
// TailLog is safe for concurrent use: a producer goroutine calls Append while
// the render goroutine calls Render, mirroring ToolRow's locking.
type TailLog struct {
	mu       sync.Mutex
	lines    []string // bounded ring, oldest first
	partial  string   // buffered line with no trailing newline yet
	maxLines int
	styleFn  FgFn
}

// NewTailLog creates a tail log that keeps at most maxLines lines. styleFn,
// if non-nil, is applied to each rendered line (e.g. a dim/grey color).
func NewTailLog(maxLines int, styleFn FgFn) *TailLog {
	if maxLines <= 0 {
		maxLines = 1
	}
	return &TailLog{maxLines: maxLines, styleFn: styleFn}
}

// Append feeds a raw output chunk. Chunks need not be line-aligned — a chunk
// may split a line, contain several lines, or contain none — partial lines
// are buffered until a newline completes them.
func (t *TailLog) Append(chunk string) {
	if chunk == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	t.partial += chunk
	for {
		i := strings.IndexByte(t.partial, '\n')
		if i < 0 {
			break
		}
		t.pushLocked(strings.TrimSuffix(t.partial[:i], "\r"))
		t.partial = t.partial[i+1:]
	}
}

func (t *TailLog) pushLocked(line string) {
	t.lines = append(t.lines, line)
	if len(t.lines) > t.maxLines {
		t.lines = t.lines[len(t.lines)-t.maxLines:]
	}
}

// Reset clears all buffered lines.
func (t *TailLog) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lines = nil
	t.partial = ""
}

// Invalidate is a no-op; TailLog holds no render cache.
func (t *TailLog) Invalidate() {}

// Render returns up to maxLines lines, truncated to width and styled. An
// in-flight partial line (no trailing newline yet) is included too, so the
// tail doesn't appear frozen while a long line accumulates.
func (t *TailLog) Render(width int) []string {
	t.mu.Lock()
	lines := make([]string, len(t.lines), len(t.lines)+1)
	copy(lines, t.lines)
	if t.partial != "" {
		lines = append(lines, t.partial)
		if len(lines) > t.maxLines {
			lines = lines[len(lines)-t.maxLines:]
		}
	}
	styleFn := t.styleFn
	t.mu.Unlock()

	out := make([]string, len(lines))
	for i, line := range lines {
		if width > 0 {
			line = TruncateToWidth(line, width, "…")
		}
		if styleFn != nil && termkit.ColorEnabled() {
			line = styleFn(line)
		}
		out[i] = line
	}
	return out
}
