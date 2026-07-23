package execute

import (
	"context"
	"errors"
	"fmt"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/metrics"
)

// Runner drains a chat.ChatEvent channel and dispatches to a Renderer.
// It owns the event-loop logic; the renderer implementations own all I/O.
type Runner struct{}

// NewRunner returns a new Runner.
func NewRunner() *Runner {
	return &Runner{}
}

// Run drains events until a terminal event or context cancellation. It
// filters events by sessionID and dispatches non-terminal events to the
// renderer. Returns nil on normal completion, or an error on runtime
// errors and timeouts.
func (r *Runner) Run(
	ctx context.Context,
	events <-chan tauchat.ChatEvent,
	renderer Renderer,
	sessionID string,
	tracker *metrics.UsageTracker,
) error {
	for {
		select {
		case <-ctx.Done():
			renderer.OnTimeout(ctx)
			//nolint:wrapcheck // ctx.Err() is the canonical error
			return ctx.Err()
		case event, ok := <-events:
			if !ok {
				return nil
			}
			switch e := event.(type) {
			case tauchat.ChatResponseDeltaEvent:
				if e.SessionID == sessionID {
					renderer.OnDelta(ctx, e)
				}
			case tauchat.ChatToolExecutionStartedEvent:
				if e.SessionID == sessionID {
					renderer.OnToolStart(ctx, e)
				}
			case tauchat.ChatToolExecutionCompletedEvent:
				if e.SessionID == sessionID {
					renderer.OnToolComplete(ctx, e)
				}
			case tauchat.ChatResponseCompletedEvent:
				if e.State.SessionID == sessionID {
					renderer.OnComplete(ctx, e, tracker, sessionID)
					return nil
				}
			case tauchat.ChatRuntimeErrorEvent:
				if e.SessionID == sessionID {
					renderer.OnError(ctx, e)
					return Err(e)
				}
			case tauchat.ChatResponseCancelledEvent:
				if e.State.SessionID == sessionID {
					renderer.OnCancel(ctx, e)
					return nil
				}
			}
		}
	}
}

// Err renders a ChatRuntimeErrorEvent as an error.
func Err(evt tauchat.ChatRuntimeErrorEvent) error {
	return fmt.Errorf("%s", evt.Message)
}

// Ensure Err satisfies the error interface.
var _ error = Err(tauchat.ChatRuntimeErrorEvent{Message: "test"})

// Ensure error type is compatible with errors.Is.
var _ = errors.Unwrap(Err(tauchat.ChatRuntimeErrorEvent{Message: "test"}))
