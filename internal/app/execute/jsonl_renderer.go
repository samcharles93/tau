package execute

import (
	"context"
	"log"
	"os"

	"github.com/samcharles93/tau/internal/agent/stdio"
	"github.com/samcharles93/tau/internal/bridge"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/metrics"
)

// JSONLRenderer emits bridge.Envelope-shaped JSON lines on stdout via a
// stdio.Writer for line-framed output. Every event is emitted; no events
// are dropped or redirected to stderr.
type JSONLRenderer struct {
	w *stdio.Writer
}

// NewJSONLRenderer creates a JSONLRenderer writing framed JSONL to os.Stdout.
func NewJSONLRenderer() *JSONLRenderer {
	return &JSONLRenderer{w: stdio.NewWriter(os.Stdout)}
}

// OnDelta emits the full ChatResponseDeltaEvent envelope so no fields
// (request ID, timestamps, etc.) are lost through reconstruction.
func (r *JSONLRenderer) OnDelta(_ context.Context, evt tauchat.ChatResponseDeltaEvent) {
	r.emit(evt)
}

// OnToolStart emits a ChatToolExecutionStartedEvent envelope.
func (r *JSONLRenderer) OnToolStart(_ context.Context, evt tauchat.ChatToolExecutionStartedEvent) {
	r.emit(evt)
}

// OnToolComplete emits a ChatToolExecutionCompletedEvent envelope.
func (r *JSONLRenderer) OnToolComplete(_ context.Context, evt tauchat.ChatToolExecutionCompletedEvent) {
	r.emit(evt)
}

// OnComplete emits a ChatResponseCompletedEvent envelope.
func (r *JSONLRenderer) OnComplete(_ context.Context, evt tauchat.ChatResponseCompletedEvent, _ *metrics.UsageTracker, _ string) {
	r.emit(evt)
}

// OnError emits a ChatRuntimeErrorEvent envelope. The runner constructs and
// returns the error itself; this renderer only emits the JSONL frame.
func (r *JSONLRenderer) OnError(_ context.Context, evt tauchat.ChatRuntimeErrorEvent) {
	r.emit(evt)
}

// OnCancel emits a ChatResponseCancelledEvent envelope.
func (r *JSONLRenderer) OnCancel(_ context.Context, evt tauchat.ChatResponseCancelledEvent) {
	r.emit(evt)
}

// OnTimeout emits a synthetic error envelope for timeouts.
func (r *JSONLRenderer) OnTimeout(ctx context.Context) {
	r.emit(tauchat.ChatRuntimeErrorEvent{
		Message: ctx.Err().Error(),
		Fatal:   true,
	})
}

// emit marshals an event using bridge.MarshalEvent and writes it as a
// framed JSONL line. Marshal and write errors are logged to stderr so a
// broken output pipe or unencodable event does not silently lose frames.
func (r *JSONLRenderer) emit(evt tauchat.ChatEvent) {
	data, err := bridge.MarshalEvent(evt)
	if err != nil {
		log.Printf("jsonl: marshal error: %v", err)
		return
	}
	if err := r.w.WriteRaw(data); err != nil {
		log.Printf("jsonl: write error: %v", err)
	}
}
