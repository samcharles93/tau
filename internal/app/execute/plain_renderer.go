package execute

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/metrics"
)

// PlainRenderer formats events as human-readable text on stdout/stderr,
// matching the byte-identical output of the current -p mode.
type PlainRenderer struct {
	stdout io.Writer
	stderr io.Writer
}

// NewPlainRenderer creates a PlainRenderer writing to os.Stdout and os.Stderr.
func NewPlainRenderer() *PlainRenderer {
	return &PlainRenderer{stdout: os.Stdout, stderr: os.Stderr}
}

// OnDelta writes the delta to stdout.
func (r *PlainRenderer) OnDelta(_ context.Context, _ string, delta string) {
	_, _ = fmt.Fprint(r.stdout, delta)
}

// OnToolStart writes a tool-start line to stderr.
func (r *PlainRenderer) OnToolStart(_ context.Context, evt tauchat.ChatToolExecutionStartedEvent) {
	_, _ = fmt.Fprintf(r.stderr, "\n⚙️  %s", evt.ToolName)
	if evt.Summary != "" {
		_, _ = fmt.Fprintf(r.stderr, ": %s", evt.Summary)
	}
}

// OnToolComplete writes a tool-complete line to stderr.
func (r *PlainRenderer) OnToolComplete(_ context.Context, evt tauchat.ChatToolExecutionCompletedEvent) {
	status := "✓"
	if evt.IsError {
		status = "✗"
	}
	_, _ = fmt.Fprintf(r.stderr, "\n  %s %s (%s)", status, evt.ToolName, evt.Duration.Round(time.Millisecond))
}

// OnComplete writes a trailing newline, any error/warning, and the metrics
// summary matching current -p behaviour.
func (r *PlainRenderer) OnComplete(_ context.Context, evt tauchat.ChatResponseCompletedEvent, tracker *metrics.UsageTracker, sessionID string) {
	_, _ = fmt.Fprintln(r.stdout)
	if evt.State.LastError != "" {
		_, _ = fmt.Fprintf(r.stderr, "\nerror: %s\n", evt.State.LastError)
	}
	if evt.FinishReason == "length" {
		_, _ = fmt.Fprintln(r.stderr, "\nwarning: response was truncated by max_tokens; rerun with --max-tokens N for a longer answer")
	}
	printSummary(r.stderr, tracker, sessionID)
}

// OnError writes nothing (the caller propagates the error).
func (r *PlainRenderer) OnError(_ context.Context, _ tauchat.ChatRuntimeErrorEvent) error {
	return nil
}

// OnCancel writes the "cancelled" message to stderr.
func (r *PlainRenderer) OnCancel(_ context.Context, _ tauchat.ChatResponseCancelledEvent) {
	_, _ = fmt.Fprintln(r.stderr, "\ncancelled")
}

// OnTimeout writes the "timed out" message to stderr.
func (r *PlainRenderer) OnTimeout(_ context.Context) {
	_, _ = fmt.Fprintln(r.stderr, "\ntimed out")
}

// printSummary prints a compact metrics summary to the given writer.
func printSummary(w io.Writer, tracker *metrics.UsageTracker, sessionID string) {
	if tracker == nil {
		return
	}
	totals := tracker.Snapshot(sessionID)
	if totals == nil {
		return
	}
	var parts []string
	if totals.PromptTokens > 0 || totals.CompletionTokens > 0 {
		parts = append(parts, fmt.Sprintf("tokens: %s / %s",
			tokens(totals.PromptTokens), tokens(totals.CompletionTokens)))
	}
	if totals.Cost > 0 {
		parts = append(parts, fmt.Sprintf("cost: %s", cost(totals.Cost)))
	}
	if totals.TurnDurationMs > 0 {
		parts = append(parts, duration(totals.TurnDurationMs))
	}
	if len(parts) > 0 {
		_, _ = fmt.Fprintf(w, "\n%s\n", strings.Join(parts, " | "))
	}
}

func tokens(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
}

func cost(usd float64) string {
	if usd < 1 {
		return fmt.Sprintf("$%.4f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

func duration(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", ms)
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		return d.Round(time.Second).String()
	}
}
