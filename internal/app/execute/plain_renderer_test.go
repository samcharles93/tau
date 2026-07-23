package execute

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

func TestPlainRenderer_OnDelta_WritesToStdout(t *testing.T) {
	var out, err bytes.Buffer
	r := &PlainRenderer{stdout: &out, stderr: &err}

	r.OnDelta(context.Background(), "s1", "hello")
	if out.String() != "hello" {
		t.Fatalf("expected 'hello', got %q", out.String())
	}
	if err.Len() > 0 {
		t.Fatalf("expected no stderr, got %q", err.String())
	}
}

func TestPlainRenderer_OnComplete_WritesNewlineAndSummary(t *testing.T) {
	var out, errBuf bytes.Buffer
	r := &PlainRenderer{stdout: &out, stderr: &errBuf}

	evt := tauchat.ChatResponseCompletedEvent{
		State: tauchat.ChatSessionState{SessionID: "s1"},
	}
	r.OnComplete(context.Background(), evt, nil, "s1")
	if !strings.HasPrefix(out.String(), "\n") {
		t.Fatalf("expected newline, got %q", out.String())
	}
}

func TestPlainRenderer_OnComplete_WritesErrorToStderr(t *testing.T) {
	var out, errBuf bytes.Buffer
	r := &PlainRenderer{stdout: &out, stderr: &errBuf}

	evt := tauchat.ChatResponseCompletedEvent{
		State: tauchat.ChatSessionState{
			SessionID: "s1",
			LastError: "something went wrong",
		},
	}
	r.OnComplete(context.Background(), evt, nil, "s1")
	if !strings.Contains(errBuf.String(), "error: something went wrong") {
		t.Fatalf("expected error in stderr, got %q", errBuf.String())
	}
}

func TestPlainRenderer_OnComplete_WarnsOnLengthFinish(t *testing.T) {
	var out, errBuf bytes.Buffer
	r := &PlainRenderer{stdout: &out, stderr: &errBuf}

	evt := tauchat.ChatResponseCompletedEvent{
		State:        tauchat.ChatSessionState{SessionID: "s1"},
		FinishReason: "length",
	}
	r.OnComplete(context.Background(), evt, nil, "s1")
	if !strings.Contains(errBuf.String(), "truncated by max_tokens") {
		t.Fatalf("expected truncation warning in stderr, got %q", errBuf.String())
	}
}

func TestPlainRenderer_OnCancel_WritesToStderr(t *testing.T) {
	var out, errBuf bytes.Buffer
	r := &PlainRenderer{stdout: &out, stderr: &errBuf}

	r.OnCancel(context.Background(), tauchat.ChatResponseCancelledEvent{
		State: tauchat.ChatSessionState{SessionID: "s1"},
	})
	if !strings.Contains(errBuf.String(), "cancelled") {
		t.Fatalf("expected 'cancelled' in stderr, got %q", errBuf.String())
	}
	if out.Len() > 0 {
		t.Fatalf("expected no stdout, got %q", out.String())
	}
}

func TestPlainRenderer_OnTimeout_WritesToStderr(t *testing.T) {
	var out, errBuf bytes.Buffer
	r := &PlainRenderer{stdout: &out, stderr: &errBuf}

	r.OnTimeout(context.Background())
	if !strings.Contains(errBuf.String(), "timed out") {
		t.Fatalf("expected 'timed out' in stderr, got %q", errBuf.String())
	}
	if out.Len() > 0 {
		t.Fatalf("expected no stdout, got %q", out.String())
	}
}

func TestPlainRenderer_ToolEvents_GoToStderr(t *testing.T) {
	var out, errBuf bytes.Buffer
	r := &PlainRenderer{stdout: &out, stderr: &errBuf}

	start := tauchat.ChatToolExecutionStartedEvent{
		SessionID: "s1",
		ToolName:  "read",
		Summary:   "reading file",
	}
	r.OnToolStart(context.Background(), start)
	if !strings.Contains(errBuf.String(), "read") {
		t.Fatalf("expected tool name in stderr, got %q", errBuf.String())
	}
	if out.Len() > 0 {
		t.Fatalf("expected no stdout, got %q", out.String())
	}

	// Reset buffers
	out.Reset()
	errBuf.Reset()

	complete := tauchat.ChatToolExecutionCompletedEvent{
		SessionID: "s1",
		ToolName:  "read",
		Status:    "ok",
		Duration:  time.Second,
	}
	r.OnToolComplete(context.Background(), complete)
	if !strings.Contains(errBuf.String(), "read") {
		t.Fatalf("expected tool name in stderr, got %q", errBuf.String())
	}
	if out.Len() > 0 {
		t.Fatalf("expected no stdout, got %q", out.String())
	}
}

func TestPrintSummary_Empty(t *testing.T) {
	var buf bytes.Buffer
	printSummary(&buf, nil, "s1")
	if buf.Len() > 0 {
		t.Fatalf("expected no output for nil tracker, got %q", buf.String())
	}
}

func TestTokens_Formatting(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{500, "500"},
		{1000, "1.0k"},
		{1500, "1.5k"},
		{1_000_000, "1.0M"},
	}
	for _, tt := range tests {
		got := tokens(tt.n)
		if got != tt.want {
			t.Errorf("tokens(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestCost_Formatting(t *testing.T) {
	tests := []struct {
		usd  float64
		want string
	}{
		{0.001, "$0.0010"},
		{0.1234, "$0.1234"},
		{1.0, "$1.00"},
		{1.5, "$1.50"},
	}
	for _, tt := range tests {
		got := cost(tt.usd)
		if got != tt.want {
			t.Errorf("cost(%f) = %q, want %q", tt.usd, got, tt.want)
		}
	}
}

func TestDuration_Formatting(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{100, "100ms"},
		{1000, "1.0s"},
		{60000, "1m0s"},
	}
	for _, tt := range tests {
		got := duration(tt.ms)
		if got != tt.want {
			t.Errorf("duration(%d) = %q, want %q", tt.ms, got, tt.want)
		}
	}
}

func TestPlainRenderer_OnError_ReturnsNil(t *testing.T) {
	r := NewPlainRenderer()
	err := r.OnError(context.Background(), tauchat.ChatRuntimeErrorEvent{})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

// Ensure NewPlainRenderer uses os.Stdout/os.Stderr by checking it doesn't panic.
func TestNewPlainRenderer_DoesNotPanic(t *testing.T) {
	r := NewPlainRenderer()
	if r == nil {
		t.Fatal("expected non-nil renderer")
	}
	if r.stdout == nil || r.stderr == nil {
		t.Fatal("expected non-nil writers")
	}
}
