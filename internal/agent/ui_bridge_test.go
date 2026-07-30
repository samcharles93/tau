package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
)

func TestCoordinatorUIBridge_Log(t *testing.T) {
	bus := eventbus.New()
	t.Cleanup(bus.Close)
	client := bus.Client("test")
	chatPub := eventbus.Publish[chat.ChatEvent](client)

	c := &Coordinator{
		chatPub: chatPub,
	}
	bridge := &coordinatorUIBridge{coordinator: c}

	sub := eventbus.Subscribe[chat.ChatEvent](client)
	defer sub.Close()

	// Wait for subscription to be ready
	time.Sleep(10 * time.Millisecond)

	bridge.Log("test chunk")

	select {
	case event := <-sub.Events():
		logEvent, ok := event.(chat.ChatToolOutputEvent)
		require.True(t, ok, "expected ChatToolOutputEvent, got %T", event)
		require.Equal(t, "test chunk", logEvent.Chunk)
		require.NotZero(t, logEvent.ReceivedAt)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for ChatToolOutputEvent")
	}
}
