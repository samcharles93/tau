package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBridge struct {
	clients    int
	upgradeErr error
}

func (b *fakeBridge) UpgradeHTTP(w http.ResponseWriter, r *http.Request) error {
	return b.upgradeErr
}

func (b *fakeBridge) ClientCount() int { return b.clients }
func (b *fakeBridge) Close() error     { return nil }

func TestServerServesHealthAndSPA(t *testing.T) {
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<h1>Tau</h1>")
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := New(":0", &fakeBridge{clients: 0}, spa, nil)
	addr, err := srv.Start(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, addr)
	require.Contains(t, addr, "127.0.0.1")

	// Give the server a moment to start serving.
	time.Sleep(50 * time.Millisecond)

	client := &http.Client{Timeout: 2 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/health", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var health map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&health))
	assert.True(t, health["ok"].(bool))

	req2, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/not-a-route", nil)
	require.NoError(t, err)
	resp2, err := client.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()
	body, _ := io.ReadAll(resp2.Body)
	assert.Contains(t, string(body), "Tau")

	cancel()
	srv.Wait()
}
