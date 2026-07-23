package tools

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/agent/stdio"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/stretchr/testify/require"
)

// TestReadChildResult_DoesNotLeakRawWirePayloadAsToolOutput is the
// regression test for tau-gh9: readChildResult used to publish the raw
// agent.event wire envelope bytes as a ChatToolOutputEvent.Chunk, which is
// documented and consumed elsewhere as a human-readable live text chunk
// (see internal/tui2/events.go, internal/tui/inline_events.go). That leaked
// protocol internals (JSON type discriminators, nested envelopes) into
// anything that renders or persists ChatToolOutputEvent verbatim.
//
// The properly decoded event is already published separately as a
// ChildAgentMessageEvent, which the TUI's child-transcript renderer
// consumes - so no ChatToolOutputEvent should be published for agent.event
// messages at all.
func TestReadChildResult_DoesNotLeakRawWirePayloadAsToolOutput(t *testing.T) {
	const instanceID = "research#1"
	const callID = "call-1"

	var buf bytes.Buffer
	w := stdio.NewWriter(&buf)

	// A single agent.event carrying a decodable ChatResponseStartedEvent.
	err := w.WriteMessage(map[string]any{
		"type": "agent.event",
		"payload": map[string]any{
			"instance": instanceID,
			"event": map[string]any{
				"type": "ChatResponseStartedEvent",
				"payload": map[string]any{
					"session_id": "child-session",
					"request_id": "req-1",
					"started_at": time.Now(),
				},
			},
		},
	})
	require.NoError(t, err)

	// Terminate the read loop.
	err = w.WriteMessage(map[string]any{
		"type": "agent.result",
		"payload": map[string]any{
			"task_id":    instanceID,
			"status":     "completed",
			"final_text": "done",
		},
	})
	require.NoError(t, err)

	reader := stdio.NewReader(&buf)

	bus := eventbus.New()
	t.Cleanup(bus.Close)
	client := bus.Client("test")
	childPub := eventbus.Publish[tauchat.ChatEvent](client)
	sub := eventbus.Subscribe[tauchat.ChatEvent](client)
	defer sub.Close()

	// Wait for subscription to be ready, matching the pattern used by
	// TestCoordinatorUIBridge_Log in internal/agent/ui_bridge_test.go.
	time.Sleep(10 * time.Millisecond)

	// Drain events on a background goroutine for the lifetime of the test:
	// Publish blocks until a subscriber receives, so readChildResult below
	// would deadlock without a concurrent reader. Stopped via close(stop)
	// rather than relying on Events() closing - per Subscriber.Close's
	// docs, "receives on Events block forever" after Close, so a
	// range-until-closed consumer would itself deadlock.
	stop := make(chan struct{})
	collected := make(chan []tauchat.ChatEvent, 1)
	go func() {
		var events []tauchat.ChatEvent
		for {
			select {
			case ev := <-sub.Events():
				events = append(events, ev)
			case <-stop:
				collected <- events
				return
			}
		}
	}()

	_, result, _, err := readChildResult(context.Background(), reader, instanceID, callID, "parent-session", childPub)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "completed", result.Status)

	// readChildResult has returned, so nothing more will be published;
	// give the collector a brief moment to receive anything already in
	// flight before stopping it.
	time.Sleep(20 * time.Millisecond)
	close(stop)
	gotEvents := <-collected

	for _, ev := range gotEvents {
		if out, ok := ev.(tauchat.ChatToolOutputEvent); ok {
			t.Fatalf("readChildResult published a ChatToolOutputEvent for an agent.event message (raw wire payload leak): %+v", out)
		}
	}

	var sawChildMessage bool
	for _, ev := range gotEvents {
		if msg, ok := ev.(tauchat.ChildAgentMessageEvent); ok {
			sawChildMessage = true
			require.Equal(t, instanceID, msg.InstanceID)
			require.Equal(t, callID, msg.CallID)
			_, ok := msg.Event.(tauchat.ChatResponseStartedEvent)
			require.True(t, ok, "expected decoded ChatResponseStartedEvent, got %T", msg.Event)
		}
	}
	require.True(t, sawChildMessage, "expected a ChildAgentMessageEvent to be published with the decoded event")
}
