package plugin

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/samcharles93/tau/pkg/plugin/api"
	"github.com/stretchr/testify/require"
)

func newTestHost(t *testing.T) *hostService {
	t.Helper()
	return &hostService{
		config: map[string]map[string]any{
			"mcp-plugin": {
				"servers": []any{map[string]any{"name": "fs"}},
				"enabled": true,
			},
		},
		kv: newKVStore(filepath.Join(t.TempDir(), "kv.json")),
	}
}

func TestHostServiceGetConfig(t *testing.T) {
	h := newTestHost(t)
	ctx := context.Background()

	// Whole block.
	resp, err := h.GetConfig(ctx, &api.GetConfigRequest{PluginName: "mcp-plugin"})
	require.NoError(t, err)
	require.True(t, resp.Found)
	require.Contains(t, resp.Value, "servers")

	// Single key.
	resp, err = h.GetConfig(ctx, &api.GetConfigRequest{PluginName: "mcp-plugin", Key: "enabled"})
	require.NoError(t, err)
	require.True(t, resp.Found)
	require.Equal(t, "true", resp.Value)

	// Unknown plugin and unknown key.
	resp, _ = h.GetConfig(ctx, &api.GetConfigRequest{PluginName: "nope"})
	require.False(t, resp.Found)
	resp, _ = h.GetConfig(ctx, &api.GetConfigRequest{PluginName: "mcp-plugin", Key: "missing"})
	require.False(t, resp.Found)
}

func TestHostServiceSetConfigOverridesAndPersists(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "kv.json")

	h := &hostService{
		config: map[string]map[string]any{"mcp-plugin": {"servers": []any{}}},
		kv:     newKVStore(path),
	}

	// SetConfig takes precedence over the static block on subsequent GetConfig.
	_, err := h.SetConfig(ctx, &api.SetConfigRequest{
		PluginName: "mcp-plugin",
		Key:        "servers",
		Value:      `[{"name":"x"}]`,
	})
	require.NoError(t, err)

	resp, err := h.GetConfig(ctx, &api.GetConfigRequest{PluginName: "mcp-plugin", Key: "servers"})
	require.NoError(t, err)
	require.True(t, resp.Found)
	require.Equal(t, `[{"name":"x"}]`, resp.Value)

	// A fresh store from the same path sees the persisted value.
	reopened := newKVStore(path)
	v, ok := reopened.get("mcp-plugin", "servers")
	require.True(t, ok)
	require.Equal(t, `[{"name":"x"}]`, v)
}

func TestManagerHasCapability(t *testing.T) {
	m := &Manager{capabilities: map[string][]string{
		"mcp":  {api.CapabilityTools},
		"full": {}, // advertises nothing -> treated as fully capable
	}}

	require.True(t, m.hasCapability("mcp", api.CapabilityTools))
	require.False(t, m.hasCapability("mcp", api.CapabilityEvents))
	require.True(t, m.hasCapability("full", api.CapabilityEvents))   // empty = all
	require.True(t, m.hasCapability("unknown", api.CapabilityTools)) // unrecorded = all
}

func TestHostServiceLogAndNotifyNilSafe(t *testing.T) {
	h := &hostService{} // no logger, no notifier, no kv
	ctx := context.Background()

	_, err := h.Notify(ctx, &api.NotifyRequest{Level: "info", Message: "hi"})
	require.NoError(t, err)
	_, err = h.Log(ctx, &api.LogRequest{Entry: &api.LogEntry{Level: "info", Message: "hi"}})
	require.NoError(t, err)
	resp, err := h.GetAvailableModels(ctx, &api.GetAvailableModelsRequest{})
	require.NoError(t, err)
	require.Empty(t, resp.Models)
}
