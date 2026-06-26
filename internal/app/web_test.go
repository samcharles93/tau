package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubRuntime struct {
	sent []tauchat.ChatCommand
}

func (r *stubRuntime) Send(cmd tauchat.ChatCommand) error {
	r.sent = append(r.sent, cmd)
	return nil
}

func (r *stubRuntime) Close() {}

func TestStartWebUIServesAndAcceptsCommands(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()

	rt := &stubRuntime{}
	opts := ChatOptions{
		Provider: tauconfig.ProviderConfig{Name: "test-provider"},
		WebPort:  0,
		NoWeb:    false,
	}

	res, err := startWebUI(rt, bus, opts, "sess-1", "gpt-test", []tauchat.CommandRef{
		{Name: "/model", Description: "Pick a model"},
	}, nil)
	require.NoError(t, err)
	require.NotNil(t, res)
	defer func() {
		res.Shutdown()
		res.Wait()
	}()

	require.Contains(t, res.URL, "http://127.0.0.1")

	client := &http.Client{Timeout: 2 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Health endpoint.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, res.URL+"/health", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var health map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&health))
	assert.True(t, health["ok"].(bool))

	// SPA fallback.
	req2, err := http.NewRequestWithContext(ctx, http.MethodGet, res.URL+"/chat", nil)
	require.NoError(t, err)
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()
	body, _ := io.ReadAll(resp2.Body)
	assert.Contains(t, string(body), "<title>Tau</title>")

	// WebSocket init + command round-trip.
	wsURL := "ws" + res.URL[4:] + "/ws"
	ws, dialResp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	if dialResp != nil {
		_ = dialResp.Body.Close()
	}
	defer ws.Close()

	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	mt, data, err := ws.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, websocket.TextMessage, mt)

	var initMsg map[string]any
	require.NoError(t, json.Unmarshal(data, &initMsg))
	assert.Equal(t, "init", initMsg["type"])
	assert.Equal(t, "sess-1", initMsg["session_id"])

	ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"SubmitChatPromptCommand","payload":{"session_id":"sess-1","request_id":"r1","prompt":"hello","submitted_at":"2026-06-27T00:00:00Z"}}`))
	require.Eventually(t, func() bool { return len(rt.sent) > 0 }, time.Second, 10*time.Millisecond)
	assert.IsType(t, tauchat.SubmitChatPromptCommand{}, rt.sent[0])
}

func TestStartWebUIDisabled(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()
	rt := &stubRuntime{}
	res, err := startWebUI(rt, bus, ChatOptions{NoWeb: true}, "s1", "m1", nil, nil)
	assert.NoError(t, err)
	assert.Nil(t, res)
}
