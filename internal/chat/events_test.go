package chat

import (
	"testing"
	"time"
)

func TestChatToolOutputEvent(t *testing.T) {
	ev := ChatToolOutputEvent{
		SessionID:  "s1",
		RequestID:  "r1",
		CallID:     "c1",
		Chunk:      "hello world",
		ReceivedAt: time.Now(),
	}

	// Verify it implements ChatEvent
	var _ ChatEvent = ev

	if ev.Chunk != "hello world" {
		t.Errorf("expected chunk 'hello world', got %q", ev.Chunk)
	}
}
