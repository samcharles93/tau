package execute

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/samcharles93/tau/internal/agent/stdio"
	"github.com/samcharles93/tau/internal/bridge"
	tauchat "github.com/samcharles93/tau/internal/chat"
)

// testWriter implements io.Writer and captures written bytes.
type testWriter struct {
	bytes.Buffer
}

func (w *testWriter) Flush() error { return nil }

func TestJSONLRenderer_OnDelta_EmitsValidEnvelope(t *testing.T) {
	var buf testWriter
	r := &JSONLRenderer{w: stdio.NewWriter(&buf)}

	r.OnDelta(context.Background(), "s1", "hello")
	r.OnDelta(context.Background(), "s1", " world")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}
	for i, line := range lines {
		var env bridge.Envelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			t.Fatalf("line %d: invalid JSON: %v", i, err)
		}
		if env.Type != "ChatResponseDeltaEvent" {
			t.Errorf("line %d: expected type ChatResponseDeltaEvent, got %q", i, env.Type)
		}
	}
}

func TestJSONLRenderer_OnToolStart_EmitsValidEnvelope(t *testing.T) {
	var buf testWriter
	r := &JSONLRenderer{w: stdio.NewWriter(&buf)}

	r.OnToolStart(context.Background(), tauchat.ChatToolExecutionStartedEvent{
		SessionID: "s1",
		ToolName:  "read",
		Summary:   "reading file",
	})

	line := strings.TrimSpace(buf.String())
	var env bridge.Envelope
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if env.Type != "ChatToolExecutionStartedEvent" {
		t.Errorf("expected type ChatToolExecutionStartedEvent, got %q", env.Type)
	}
}

func TestJSONLRenderer_OnToolComplete_EmitsValidEnvelope(t *testing.T) {
	var buf testWriter
	r := &JSONLRenderer{w: stdio.NewWriter(&buf)}

	r.OnToolComplete(context.Background(), tauchat.ChatToolExecutionCompletedEvent{
		SessionID: "s1",
		ToolName:  "read",
		Status:    "ok",
	})

	line := strings.TrimSpace(buf.String())
	var env bridge.Envelope
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if env.Type != "ChatToolExecutionCompletedEvent" {
		t.Errorf("expected type ChatToolExecutionCompletedEvent, got %q", env.Type)
	}
}

func TestJSONLRenderer_OnComplete_EmitsValidEnvelope(t *testing.T) {
	var buf testWriter
	r := &JSONLRenderer{w: stdio.NewWriter(&buf)}

	r.OnComplete(context.Background(), tauchat.ChatResponseCompletedEvent{
		State: tauchat.ChatSessionState{SessionID: "s1"},
	}, nil, "s1")

	line := strings.TrimSpace(buf.String())
	var env bridge.Envelope
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if env.Type != "ChatResponseCompletedEvent" {
		t.Errorf("expected type ChatResponseCompletedEvent, got %q", env.Type)
	}
}

func TestJSONLRenderer_OnError_EmitsValidEnvelope(t *testing.T) {
	var buf testWriter
	r := &JSONLRenderer{w: stdio.NewWriter(&buf)}

	err := r.OnError(context.Background(), tauchat.ChatRuntimeErrorEvent{
		SessionID: "s1",
		Message:   "boom",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	line := strings.TrimSpace(buf.String())
	var env bridge.Envelope
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if env.Type != "ChatRuntimeErrorEvent" {
		t.Errorf("expected type ChatRuntimeErrorEvent, got %q", env.Type)
	}
}

func TestJSONLRenderer_OnCancel_EmitsValidEnvelope(t *testing.T) {
	var buf testWriter
	r := &JSONLRenderer{w: stdio.NewWriter(&buf)}

	r.OnCancel(context.Background(), tauchat.ChatResponseCancelledEvent{
		State: tauchat.ChatSessionState{SessionID: "s1"},
	})

	line := strings.TrimSpace(buf.String())
	var env bridge.Envelope
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if env.Type != "ChatResponseCancelledEvent" {
		t.Errorf("expected type ChatResponseCancelledEvent, got %q", env.Type)
	}
}

func TestJSONLRenderer_OnTimeout_EmitsValidEnvelope(t *testing.T) {
	var buf testWriter
	r := &JSONLRenderer{w: stdio.NewWriter(&buf)}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.OnTimeout(ctx)

	line := strings.TrimSpace(buf.String())
	var env bridge.Envelope
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if env.Type != "ChatRuntimeErrorEvent" {
		t.Errorf("expected type ChatRuntimeErrorEvent, got %q", env.Type)
	}
}

func TestNewJSONLRenderer_DoesNotPanic(t *testing.T) {
	r := NewJSONLRenderer()
	if r == nil {
		t.Fatal("expected non-nil renderer")
	}
	if r.w == nil {
		t.Fatal("expected non-nil writer")
	}
}
