package execute

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/stdio"
	"github.com/samcharles93/tau/internal/bridge"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/metrics"
)

// spyRenderer records every method call for assertion.
type spyRenderer struct {
	deltas        []string
	toolStarts    []tauchat.ChatToolExecutionStartedEvent
	toolCompletes []tauchat.ChatToolExecutionCompletedEvent
	completes     []tauchat.ChatResponseCompletedEvent
	errors        []tauchat.ChatRuntimeErrorEvent
	cancels       []tauchat.ChatResponseCancelledEvent
	timeouts      int
}

func (s *spyRenderer) OnDelta(_ context.Context, evt tauchat.ChatResponseDeltaEvent) {
	s.deltas = append(s.deltas, evt.Delta)
}

func (s *spyRenderer) OnToolStart(_ context.Context, evt tauchat.ChatToolExecutionStartedEvent) {
	s.toolStarts = append(s.toolStarts, evt)
}

func (s *spyRenderer) OnToolComplete(_ context.Context, evt tauchat.ChatToolExecutionCompletedEvent) {
	s.toolCompletes = append(s.toolCompletes, evt)
}

func (s *spyRenderer) OnComplete(_ context.Context, evt tauchat.ChatResponseCompletedEvent, _ *metrics.UsageTracker, _ string) {
	s.completes = append(s.completes, evt)
}

func (s *spyRenderer) OnError(_ context.Context, evt tauchat.ChatRuntimeErrorEvent) {
	s.errors = append(s.errors, evt)
}

func (s *spyRenderer) OnCancel(_ context.Context, evt tauchat.ChatResponseCancelledEvent) {
	s.cancels = append(s.cancels, evt)
}

func (s *spyRenderer) OnTimeout(_ context.Context) {
	s.timeouts++
}

func TestRunner_DeltaEvents(t *testing.T) {
	runner := NewRunner()
	spy := &spyRenderer{}
	ch := make(chan tauchat.ChatEvent, 8)
	sid := "s1"

	ch <- tauchat.ChatResponseDeltaEvent{SessionID: sid, Delta: "hello "}
	ch <- tauchat.ChatResponseDeltaEvent{SessionID: sid, Delta: "world"}
	ch <- tauchat.ChatResponseCompletedEvent{
		State: tauchat.ChatSessionState{SessionID: sid},
	}
	close(ch)

	err := runner.Run(context.Background(), ch, spy, sid, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spy.deltas) != 2 {
		t.Fatalf("expected 2 deltas, got %d", len(spy.deltas))
	}
	if spy.deltas[0] != "hello " || spy.deltas[1] != "world" {
		t.Fatalf("unexpected deltas: %v", spy.deltas)
	}
	if len(spy.completes) != 1 {
		t.Fatalf("expected 1 complete, got %d", len(spy.completes))
	}
}

func TestRunner_FiltersBySessionID(t *testing.T) {
	runner := NewRunner()
	spy := &spyRenderer{}
	ch := make(chan tauchat.ChatEvent, 4)
	sid := "s1"

	ch <- tauchat.ChatResponseDeltaEvent{SessionID: "other", Delta: "ignored"}
	ch <- tauchat.ChatResponseCompletedEvent{
		State: tauchat.ChatSessionState{SessionID: sid},
	}
	close(ch)

	err := runner.Run(context.Background(), ch, spy, sid, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spy.deltas) != 0 {
		t.Fatalf("expected 0 deltas, got %d", len(spy.deltas))
	}
	if len(spy.completes) != 1 {
		t.Fatalf("expected 1 complete, got %d", len(spy.completes))
	}
}

func TestRunner_CancelledEvent(t *testing.T) {
	runner := NewRunner()
	spy := &spyRenderer{}
	ch := make(chan tauchat.ChatEvent, 2)
	sid := "s1"

	ch <- tauchat.ChatResponseCancelledEvent{
		State: tauchat.ChatSessionState{SessionID: sid},
	}
	close(ch)

	err := runner.Run(context.Background(), ch, spy, sid, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spy.cancels) != 1 {
		t.Fatalf("expected 1 cancel, got %d", len(spy.cancels))
	}
}

func TestRunner_ErrorEvent(t *testing.T) {
	runner := NewRunner()
	spy := &spyRenderer{}
	ch := make(chan tauchat.ChatEvent, 2)
	sid := "s1"

	ch <- tauchat.ChatRuntimeErrorEvent{SessionID: sid, Message: "boom"}
	close(ch)

	err := runner.Run(context.Background(), ch, spy, sid, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "boom" {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spy.errors) != 1 {
		t.Fatalf("expected 1 error recorded, got %d", len(spy.errors))
	}
}

func TestRunner_Timeout(t *testing.T) {
	runner := NewRunner()
	spy := &spyRenderer{}
	ch := make(chan tauchat.ChatEvent)

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	err := runner.Run(ctx, ch, spy, "s1", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
	if spy.timeouts != 1 {
		t.Fatalf("expected 1 timeout, got %d", spy.timeouts)
	}
}

func TestRunner_ToolEvents(t *testing.T) {
	runner := NewRunner()
	spy := &spyRenderer{}
	ch := make(chan tauchat.ChatEvent, 4)
	sid := "s1"

	ch <- tauchat.ChatToolExecutionStartedEvent{SessionID: sid, ToolName: "read", Summary: "reading file"}
	ch <- tauchat.ChatToolExecutionCompletedEvent{SessionID: sid, ToolName: "read", Status: "ok"}
	ch <- tauchat.ChatResponseCompletedEvent{
		State: tauchat.ChatSessionState{SessionID: sid},
	}
	close(ch)

	err := runner.Run(context.Background(), ch, spy, sid, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spy.toolStarts) != 1 {
		t.Fatalf("expected 1 tool start, got %d", len(spy.toolStarts))
	}
	if spy.toolStarts[0].ToolName != "read" {
		t.Fatalf("expected tool 'read', got %q", spy.toolStarts[0].ToolName)
	}
	if len(spy.toolCompletes) != 1 {
		t.Fatalf("expected 1 tool complete, got %d", len(spy.toolCompletes))
	}
}

func TestRunner_ChannelClosed(t *testing.T) {
	runner := NewRunner()
	spy := &spyRenderer{}
	ch := make(chan tauchat.ChatEvent)
	close(ch)

	err := runner.Run(context.Background(), ch, spy, "s1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunner_WithJSONLRenderer_ProducesFrames(t *testing.T) {
	var buf bytes.Buffer
	runner := NewRunner()
	j := &JSONLRenderer{w: stdio.NewWriter(&buf)}
	ch := make(chan tauchat.ChatEvent, 4)
	sid := "s1"

	ch <- tauchat.ChatResponseDeltaEvent{SessionID: sid, Delta: "hello"}
	ch <- tauchat.ChatResponseCompletedEvent{
		State: tauchat.ChatSessionState{SessionID: sid},
	}
	close(ch)

	err := runner.Run(context.Background(), ch, j, sid, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d: %q", len(lines), buf.String())
	}
	for i, line := range lines {
		var env bridge.Envelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			t.Fatalf("line %d: invalid JSON: %v", i, err)
		}
	}
	if !strings.Contains(lines[0], "ChatResponseDeltaEvent") {
		t.Errorf("expected delta event, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "ChatResponseCompletedEvent") {
		t.Errorf("expected completed event, got %q", lines[1])
	}
}

func TestRunner_CompletedEvent_FiltersByStateSessionID(t *testing.T) {
	runner := NewRunner()
	spy := &spyRenderer{}
	ch := make(chan tauchat.ChatEvent, 4)
	sid := "s1"

	// Delta from our session reaches spy.
	ch <- tauchat.ChatResponseDeltaEvent{SessionID: sid, Delta: "ours"}
	// Completed from a different session is ignored.
	ch <- tauchat.ChatResponseCompletedEvent{
		State: tauchat.ChatSessionState{SessionID: "other"},
	}
	// Then our session completes.
	ch <- tauchat.ChatResponseCompletedEvent{
		State: tauchat.ChatSessionState{SessionID: sid},
	}
	close(ch)

	err := runner.Run(context.Background(), ch, spy, sid, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spy.deltas) != 1 {
		t.Fatalf("expected 1 delta, got %d", len(spy.deltas))
	}
	if len(spy.completes) != 1 {
		t.Fatalf("expected 1 complete, got %d", len(spy.completes))
	}
}
