// Package execute extracts the event-reduction loop from RunStdIn into a
// standalone Runner that drains chat.ChatEvent and calls a Renderer
// interface. Plain-text and JSONL renderers consume the same runner,
// keeping output formatting out of the event loop.
package execute

import (
	"context"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/metrics"
)

// Renderer receives terminal and tool events from the Runner. The runner
// calls these methods but never touches fmt, os, or any I/O — the renderer
// implementations own all output formatting.
type Renderer interface {
	// OnDelta is called for each ChatResponseDeltaEvent.
	OnDelta(ctx context.Context, sessionID string, delta string)

	// OnToolStart is called for ChatToolExecutionStartedEvent.
	OnToolStart(ctx context.Context, evt tauchat.ChatToolExecutionStartedEvent)

	// OnToolComplete is called for ChatToolExecutionCompletedEvent.
	OnToolComplete(ctx context.Context, evt tauchat.ChatToolExecutionCompletedEvent)

	// OnComplete is called for ChatResponseCompletedEvent. The runner
	// returns nil after this call.
	OnComplete(ctx context.Context, evt tauchat.ChatResponseCompletedEvent, tracker *metrics.UsageTracker, sessionID string)

	// OnError is called for ChatRuntimeErrorEvent. The runner returns the
	// error built from the event after this call.
	OnError(ctx context.Context, evt tauchat.ChatRuntimeErrorEvent) error

	// OnCancel is called for ChatResponseCancelledEvent. The runner
	// returns nil after this call.
	OnCancel(ctx context.Context, evt tauchat.ChatResponseCancelledEvent)

	// OnTimeout is called when ctx is cancelled (e.g. timeout). The runner
	// returns ctx.Err() after this call.
	OnTimeout(ctx context.Context)
}
