package plugin

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/samcharles93/tau/pkg/plugin/api"
)

// TestUnload_DoesNotWaitForInFlightDispatch is the regression test for the
// original bug: Unload used to take a write lock that a slow/hanging
// DispatchEvent held a read lock across, so a stuck plugin blocked Unload
// (and every other manager call) for the full dispatch timeout. Now Unload
// publishes an empty snapshot with a single atomic swap and must return
// quickly even while a dispatch to the outgoing plugin is still in flight.
func TestUnload_DoesNotWaitForInFlightDispatch(t *testing.T) {
	m := newTestManager(t)
	m.cfg.EventDispatchTimeout = 2 * time.Second // long enough that a blocking Unload would fail the test

	dispatchStarted := make(chan struct{})
	releaseDispatch := make(chan struct{})
	hanging := &mockExtensionServiceClient{
		dispatchEventFunc: func(ctx context.Context, in *api.DispatchEventRequest, opts ...grpc.CallOption) (*api.DispatchEventResponse, error) {
			close(dispatchStarted)
			select {
			case <-releaseDispatch:
			case <-ctx.Done():
			}
			return nil, ctx.Err()
		},
	}
	m.setPluginClientForTest("plugin-a", &api.GRPCClient{Client: hanging})

	var dispatchDone sync.WaitGroup
	dispatchDone.Go(func() {
		m.DispatchEvent(context.Background(), "test_event", "session-1", nil)
	})

	<-dispatchStarted // the plugin RPC is now in flight and hanging

	unloadDone := make(chan struct{})
	go func() {
		m.Unload(context.Background())
		close(unloadDone)
	}()

	select {
	case <-unloadDone:
		// Unload returned without waiting for the hanging dispatch - correct.
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Unload blocked on an in-flight DispatchEvent RPC")
	}

	close(releaseDispatch)
	dispatchDone.Wait()
}

// TestExecutePluginTool_DoesNotWaitForConcurrentLoad is the regression test
// for Load holding a lock across slow plugin process starts. A read
// (ExecutePluginTool) against a plugin already in the registry must not
// wait behind a concurrent Load that is busy starting an unrelated, slow
// plugin.
func TestExecutePluginTool_DoesNotWaitForConcurrentLoad(t *testing.T) {
	m := newTestManager(t)

	fast := &mockExtensionServiceClient{
		executeToolFunc: func(ctx context.Context, in *api.ExecuteToolRequest, opts ...grpc.CallOption) (*api.ExecuteToolResponse, error) {
			return &api.ExecuteToolResponse{Content: "ok"}, nil
		},
	}
	m.setPluginClientForTest("plugin-existing", &api.GRPCClient{Client: fast})

	dir := t.TempDir()
	m.cfg.PluginsDir = dir
	writeFakeExecutable(t, dir, "slow-plugin")

	spawnStarted := make(chan struct{})
	releaseSpawn := make(chan struct{})
	m.spawnPlugin = func(ctx context.Context, pluginPath string) (pluginProcess, *api.GRPCClient, error) {
		close(spawnStarted)
		<-releaseSpawn
		return &fakeProcess{}, &api.GRPCClient{Client: &mockExtensionServiceClient{}}, nil
	}

	loadDone := make(chan struct{})
	go func() {
		_ = m.Load(context.Background())
		close(loadDone)
	}()

	<-spawnStarted // plugin start is now hanging inside Load

	resultCh := make(chan struct{})
	go func() {
		_, err := m.ExecutePluginTool(context.Background(), "plugin-existing", "some_tool", nil)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		close(resultCh)
	}()

	select {
	case <-resultCh:
		// ExecutePluginTool returned without waiting for the slow spawn inside Load - correct.
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ExecutePluginTool blocked on a concurrent, slow plugin spawn inside Load")
	}

	close(releaseSpawn)
	<-loadDone
}

// TestDispatchEvent_RunsPluginsInParallel verifies that DispatchEvent no
// longer serializes plugin RPCs: N plugins that each sleep for delay should
// complete in roughly delay, not N*delay.
func TestDispatchEvent_RunsPluginsInParallel(t *testing.T) {
	m := newTestManager(t)
	m.cfg.EventDispatchTimeout = 2 * time.Second

	const numPlugins = 5
	const delay = 100 * time.Millisecond

	for i := range numPlugins {
		client := &mockExtensionServiceClient{
			dispatchEventFunc: func(ctx context.Context, in *api.DispatchEventRequest, opts ...grpc.CallOption) (*api.DispatchEventResponse, error) {
				time.Sleep(delay)
				return &api.DispatchEventResponse{Response: &api.EventResponse{}}, nil
			},
		}
		m.setPluginClientForTest(pluginName(i), &api.GRPCClient{Client: client})
	}

	start := time.Now()
	m.DispatchEvent(context.Background(), "test_event", "session-1", nil)
	elapsed := time.Since(start)

	if elapsed >= numPlugins*delay {
		t.Errorf("DispatchEvent took %v, expected well under the sequential bound of %v (dispatch is not parallel)", elapsed, numPlugins*delay)
	}
}

func pluginName(i int) string {
	return "plugin-" + string(rune('a'+i))
}

// TestDispatchEvent_MergeOrderDeterministicDespiteCompletionOrder verifies
// that even though plugins run concurrently and may finish in any order,
// responses are merged in a fixed order (registry load order).
func TestDispatchEvent_MergeOrderDeterministicDespiteCompletionOrder(t *testing.T) {
	m := newTestManager(t)
	m.cfg.EventDispatchTimeout = 2 * time.Second

	// plugin-a finishes last, plugin-b finishes first - merge order must
	// still be a-then-b (registry order), not completion order.
	a := &mockExtensionServiceClient{
		dispatchEventFunc: func(ctx context.Context, in *api.DispatchEventRequest, opts ...grpc.CallOption) (*api.DispatchEventResponse, error) {
			time.Sleep(60 * time.Millisecond)
			return &api.DispatchEventResponse{Response: &api.EventResponse{InjectSystemPrompt: "from-a"}}, nil
		},
	}
	b := &mockExtensionServiceClient{
		dispatchEventFunc: func(ctx context.Context, in *api.DispatchEventRequest, opts ...grpc.CallOption) (*api.DispatchEventResponse, error) {
			return &api.DispatchEventResponse{Response: &api.EventResponse{InjectSystemPrompt: "from-b"}}, nil
		},
	}
	m.setPluginClientForTest("plugin-a", &api.GRPCClient{Client: a})
	m.setPluginClientForTest("plugin-b", &api.GRPCClient{Client: b})

	resp := m.DispatchEvent(context.Background(), "test_event", "session-1", nil)
	if resp == nil {
		t.Fatal("expected non-nil merged response")
	}
	if got, want := resp.GetInjectSystemPrompt(), "from-a\nfrom-b"; got != want {
		t.Errorf("merge order not deterministic: got %q, want %q", got, want)
	}
}

// TestLoad_StaleGenerationRetiresOrphanedPlugin verifies AC2/AC5's
// stale-generation-completion case: if a Load's compare-and-swap loses
// against a concurrent Unload that committed first, the Load must not
// silently leak the process/tools it just started - it retires them and
// reports an error instead of corrupting the winner's registry.
func TestLoad_StaleGenerationRetiresOrphanedPlugin(t *testing.T) {
	m := newTestManager(t)

	dir := t.TempDir()
	m.cfg.PluginsDir = dir
	writeFakeExecutable(t, dir, "slow-plugin")

	spawnStarted := make(chan struct{})
	proc := &fakeProcess{}
	m.spawnPlugin = func(ctx context.Context, pluginPath string) (pluginProcess, *api.GRPCClient, error) {
		close(spawnStarted)
		// Give the concurrent Unload a chance to win the race by committing
		// an empty snapshot before this Load publishes.
		time.Sleep(50 * time.Millisecond)
		return proc, &api.GRPCClient{Client: &mockExtensionServiceClient{}}, nil
	}

	loadErrCh := make(chan error, 1)
	go func() {
		loadErrCh <- m.Load(context.Background())
	}()

	<-spawnStarted
	m.Unload(context.Background()) // commits an empty snapshot first, while Load is still "spawning"

	loadErr := <-loadErrCh
	if loadErr == nil {
		t.Fatal("expected Load to report an error when it loses the race to a concurrent Unload")
	}

	if !proc.killed.Load() {
		t.Error("expected the orphaned plugin process to be killed (retired), but it was left running")
	}

	snap := m.snapshot.Load()
	if snap == nil || len(snap.entries) != 0 {
		t.Errorf("expected the registry to remain empty (Unload's snapshot wins), got %+v", snap)
	}
}

func writeFakeExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
