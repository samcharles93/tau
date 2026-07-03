package plugin

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
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

// fakeViewRenderer records RenderView/CloseView calls for assertions.
type fakeViewRenderer struct {
	mu       sync.Mutex
	rendered []string // "plugin:viewID" per RenderView call
	closed   []string // "plugin:viewID" per CloseView call
	failIDs  map[string]bool
}

func (f *fakeViewRenderer) RenderView(_ context.Context, pluginName string, view *api.View) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := pluginName + ":" + view.Id
	f.rendered = append(f.rendered, key)
	if f.failIDs[key] {
		return errors.New("fakeViewRenderer: forced failure")
	}
	return nil
}

func (f *fakeViewRenderer) CloseView(_ context.Context, pluginName, viewID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, pluginName+":"+viewID)
	return nil
}

func TestHostServiceRenderView(t *testing.T) {
	fvr := &fakeViewRenderer{}
	h := &hostService{viewRenderer: fvr, views: map[string]map[string]struct{}{}}
	ctx := context.Background()

	_, err := h.RenderView(ctx, &api.RenderViewRequest{
		PluginName: "hello",
		View:       &api.View{Id: "panel-1"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"hello:panel-1"}, fvr.rendered)

	// Re-rendering the same id updates in place - delegate is called again
	// but the id is not double-counted in the ownership registry.
	_, err = h.RenderView(ctx, &api.RenderViewRequest{
		PluginName: "hello",
		View:       &api.View{Id: "panel-1"},
	})
	require.NoError(t, err)
	require.Len(t, fvr.rendered, 2)
	require.Len(t, h.views["hello"], 1)
}

func TestHostServiceRenderViewRequiresPluginNameAndID(t *testing.T) {
	h := &hostService{viewRenderer: &fakeViewRenderer{}, views: map[string]map[string]struct{}{}}
	ctx := context.Background()

	_, err := h.RenderView(ctx, &api.RenderViewRequest{View: &api.View{Id: "x"}})
	require.Error(t, err)

	_, err = h.RenderView(ctx, &api.RenderViewRequest{PluginName: "hello", View: &api.View{}})
	require.Error(t, err)
}

func TestHostServiceRenderViewMaxViewsExceeded(t *testing.T) {
	fvr := &fakeViewRenderer{}
	h := &hostService{viewRenderer: fvr, views: map[string]map[string]struct{}{}, maxViewsPerPlugin: 2}
	ctx := context.Background()

	for i := range 2 {
		_, err := h.RenderView(ctx, &api.RenderViewRequest{
			PluginName: "hello",
			View:       &api.View{Id: fmt.Sprintf("panel-%d", i)},
		})
		require.NoError(t, err)
	}

	_, err := h.RenderView(ctx, &api.RenderViewRequest{
		PluginName: "hello",
		View:       &api.View{Id: "panel-c"},
	})
	require.Error(t, err)
	require.Len(t, fvr.rendered, 2) // the rejected 3rd view never reached the renderer
}

// TestHostServiceRenderViewFailedRenderRollsBackReservation verifies that a
// renderer failure on a *new* id releases the quota slot it reserved, so a
// panel that was never actually shown doesn't permanently count against the
// plugin's view limit.
func TestHostServiceRenderViewFailedRenderRollsBackReservation(t *testing.T) {
	fvr := &fakeViewRenderer{failIDs: map[string]bool{"hello:panel-1": true}}
	h := &hostService{viewRenderer: fvr, views: map[string]map[string]struct{}{}, maxViewsPerPlugin: 1}
	ctx := context.Background()

	_, err := h.RenderView(ctx, &api.RenderViewRequest{PluginName: "hello", View: &api.View{Id: "panel-1"}})
	require.Error(t, err)
	require.Empty(t, h.views["hello"], "a failed render must not leave the id registered")

	// The slot must be available again - a second, successful render for a
	// *different* id should not be rejected as over-quota.
	_, err = h.RenderView(ctx, &api.RenderViewRequest{PluginName: "hello", View: &api.View{Id: "panel-2"}})
	require.NoError(t, err)
	require.Len(t, h.views["hello"], 1)
}

// TestHostServiceRenderViewConcurrentNewViewsRespectQuota guards against a
// regression where checking the quota and reserving the slot were split
// across two separate critical sections: concurrent RenderView calls for
// distinct new ids could all observe room under the limit and jointly
// exceed it. The reservation must happen atomically with the check.
func TestHostServiceRenderViewConcurrentNewViewsRespectQuota(t *testing.T) {
	fvr := &fakeViewRenderer{}
	h := &hostService{viewRenderer: fvr, views: map[string]map[string]struct{}{}, maxViewsPerPlugin: 5}
	ctx := context.Background()

	const attempts = 20
	var wg sync.WaitGroup
	var successes atomic.Int32
	for i := range attempts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := h.RenderView(ctx, &api.RenderViewRequest{
				PluginName: "hello",
				View:       &api.View{Id: fmt.Sprintf("panel-%d", i)},
			})
			if err == nil {
				successes.Add(1)
			}
		}(i)
	}
	wg.Wait()

	require.EqualValues(t, 5, successes.Load(), "exactly maxViewsPerPlugin renders should succeed")
	require.Len(t, h.views["hello"], 5)
}

func TestHostServiceCloseView(t *testing.T) {
	fvr := &fakeViewRenderer{}
	h := &hostService{viewRenderer: fvr, views: map[string]map[string]struct{}{}}
	ctx := context.Background()

	_, err := h.RenderView(ctx, &api.RenderViewRequest{PluginName: "hello", View: &api.View{Id: "panel-1"}})
	require.NoError(t, err)

	_, err = h.CloseView(ctx, &api.CloseViewRequest{PluginName: "hello", ViewId: "panel-1"})
	require.NoError(t, err)
	require.Equal(t, []string{"hello:panel-1"}, fvr.closed)
	require.Empty(t, h.views["hello"])
}

func TestHostServiceCloseViewWrongOwnerRejected(t *testing.T) {
	fvr := &fakeViewRenderer{}
	h := &hostService{viewRenderer: fvr, views: map[string]map[string]struct{}{}}
	ctx := context.Background()

	_, err := h.RenderView(ctx, &api.RenderViewRequest{PluginName: "hello", View: &api.View{Id: "panel-1"}})
	require.NoError(t, err)

	// A different plugin (even one guessing the exact same view id) cannot
	// close hello's view - CloseView silently no-ops rather than erroring,
	// so a spoofing attempt can't be used to probe for view existence.
	_, err = h.CloseView(ctx, &api.CloseViewRequest{PluginName: "evil", ViewId: "panel-1"})
	require.NoError(t, err)
	require.Empty(t, fvr.closed)
	require.Len(t, h.views["hello"], 1) // hello's view is untouched
}

func TestHostServiceCloseAllViewsForPlugin(t *testing.T) {
	fvr := &fakeViewRenderer{}
	h := &hostService{viewRenderer: fvr, views: map[string]map[string]struct{}{}}
	ctx := context.Background()

	for _, id := range []string{"a", "b", "c"} {
		_, err := h.RenderView(ctx, &api.RenderViewRequest{PluginName: "hello", View: &api.View{Id: id}})
		require.NoError(t, err)
	}
	_, err := h.RenderView(ctx, &api.RenderViewRequest{PluginName: "other", View: &api.View{Id: "z"}})
	require.NoError(t, err)

	h.closeAllViewsForPlugin(ctx, "hello")

	require.ElementsMatch(t, []string{"hello:a", "hello:b", "hello:c"}, fvr.closed)
	require.Empty(t, h.views["hello"])
	require.Len(t, h.views["other"], 1) // other plugins are untouched
}
