package bridge

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRuntime struct {
	mu   sync.Mutex
	sent []tauchat.ChatCommand
}

func (r *fakeRuntime) Send(cmd tauchat.ChatCommand) error {
	r.mu.Lock()
	r.sent = append(r.sent, cmd)
	r.mu.Unlock()
	return nil
}

func (r *fakeRuntime) Close() {}

func TestBridgeForwardsCommandFromClient(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()

	rt := &fakeRuntime{}
	b, err := NewBridge(rt, bus, InitInfo{}, nil)
	require.NoError(t, err)
	defer b.Close()

	// Serve the bridge's WebSocket handler on a test HTTP server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = b.UpgradeHTTP(w, r)
	}))
	defer srv.Close()

	clientConn, resp, err := websocket.DefaultDialer.Dial("ws"+srv.URL[4:]+"/ws", nil)
	require.NoError(t, err)
	if resp != nil {
		_ = resp.Body.Close()
	}
	defer clientConn.Close()

	payload := []byte(`{"type":"SubmitChatPromptCommand","payload":{"session_id":"s1","request_id":"r1","prompt":"hello","submitted_at":"2026-06-27T00:00:00Z"}}`)
	err = clientConn.WriteMessage(websocket.TextMessage, payload)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		rt.mu.Lock()
		defer rt.mu.Unlock()
		return len(rt.sent) > 0
	}, time.Second, 10*time.Millisecond)
	require.IsType(t, tauchat.SubmitChatPromptCommand{}, rt.sent[0])
}

func TestBridgeBroadcastsEventToClient(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()

	rt := &fakeRuntime{}
	b, err := NewBridge(rt, bus, InitInfo{}, nil)
	require.NoError(t, err)
	defer b.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = b.UpgradeHTTP(w, r)
	}))
	defer srv.Close()

	clientConn, resp, err := websocket.DefaultDialer.Dial("ws"+srv.URL[4:]+"/ws", nil)
	require.NoError(t, err)
	if resp != nil {
		_ = resp.Body.Close()
	}
	defer clientConn.Close()

	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))

	// Read the init envelope first. UpgradeHTTP registers the client
	// (addClient, making it eligible for broadcastEvent's fan-out) strictly
	// before writing this message, so receiving it is proof the server has
	// finished registration - not just that the WebSocket handshake
	// completed. Publishing before this point races the bus delivering the
	// event against the server reaching addClient(); on a fast local
	// machine the publish reliably loses that race, but it flaked on a
	// slower/more contended CI runner, dropping the event for this client.
	mt, _, err := clientConn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, websocket.TextMessage, mt)

	// Now publish the notification - the client is guaranteed registered.
	pub := eventbus.Publish[tauchat.ChatEvent](bus.Client("test"))
	pub.Publish(tauchat.ChatNotificationEvent{Message: "hi", Level: tauchat.ChatNotificationInfo, OccurredAt: time.Now().UTC()})
	pub.Close()

	// Next message should be the broadcast event.
	mt, data, err := clientConn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, websocket.TextMessage, mt)
	assert.Contains(t, string(data), `"type":"ChatNotificationEvent"`)
	assert.Contains(t, string(data), `"message":"hi"`)
}
