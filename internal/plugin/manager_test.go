package plugin

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/pkg/plugin/api"
)

// mockExtensionServiceClient implements api.ExtensionServiceClient for tests.
type mockExtensionServiceClient struct {
	api.ExtensionServiceClient // embed for the methods we don't override

	dispatchEventFunc func(ctx context.Context, in *api.DispatchEventRequest, opts ...grpc.CallOption) (*api.DispatchEventResponse, error)
	executeToolFunc   func(ctx context.Context, in *api.ExecuteToolRequest, opts ...grpc.CallOption) (*api.ExecuteToolResponse, error)

	// dispatchCallCount tracks how many times DispatchEvent was called.
	dispatchCallCount atomic.Int64
}

func (m *mockExtensionServiceClient) DispatchEvent(ctx context.Context, in *api.DispatchEventRequest, opts ...grpc.CallOption) (*api.DispatchEventResponse, error) {
	m.dispatchCallCount.Add(1)
	if m.dispatchEventFunc != nil {
		return m.dispatchEventFunc(ctx, in, opts...)
	}
	return &api.DispatchEventResponse{}, nil
}

func (m *mockExtensionServiceClient) ExecuteTool(ctx context.Context, in *api.ExecuteToolRequest, opts ...grpc.CallOption) (*api.ExecuteToolResponse, error) {
	if m.executeToolFunc != nil {
		return m.executeToolFunc(ctx, in, opts...)
	}
	return &api.ExecuteToolResponse{}, nil
}

// GetCapabilities, GetMetadata, and GetTools have safe zero-value defaults
// so tests that exercise Load() (which calls all three) don't panic on the
// embedded nil api.ExtensionServiceClient.
func (m *mockExtensionServiceClient) GetCapabilities(ctx context.Context, in *api.GetCapabilitiesRequest, opts ...grpc.CallOption) (*api.GetCapabilitiesResponse, error) {
	return &api.GetCapabilitiesResponse{}, nil
}

func (m *mockExtensionServiceClient) GetMetadata(ctx context.Context, in *api.GetMetadataRequest, opts ...grpc.CallOption) (*api.GetMetadataResponse, error) {
	return &api.GetMetadataResponse{}, nil
}

func (m *mockExtensionServiceClient) GetTools(ctx context.Context, in *api.GetToolsRequest, opts ...grpc.CallOption) (*api.GetToolsResponse, error) {
	return &api.GetToolsResponse{}, nil
}

// newTestManager creates a Manager with short timeouts for fast tests.
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(Config{
		Logger:               slog.New(slog.DiscardHandler),
		EventDispatchTimeout: 50 * time.Millisecond,
		ToolExecutionTimeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// fakeProcess is a no-op pluginProcess for tests that don't spawn real
// plugin binaries.
type fakeProcess struct {
	killed atomic.Bool
}

func (p *fakeProcess) Kill() { p.killed.Store(true) }

// setPluginClientForTest injects a mock gRPC client for a plugin, bypassing
// the full go-plugin startup path.
func (m *Manager) setPluginClientForTest(name string, client *api.GRPCClient) {
	m.setPluginEntryForTest(&pluginEntry{name: name, grpc: client, process: &fakeProcess{}})
}

// setPluginEntryForTest publishes a single plugin entry into the registry,
// preserving whatever is already there. Test setup is single-threaded, so
// this does a plain (non-CAS) read-modify-publish.
func (m *Manager) setPluginEntryForTest(pe *pluginEntry) {
	old := m.snapshot.Load()
	next := &registrySnapshot{entries: make(map[string]*pluginEntry)}
	if old != nil {
		maps.Copy(next.entries, old.entries)
		next.order = append(next.order, old.order...)
	}
	if _, exists := next.entries[pe.name]; !exists {
		next.order = append(next.order, pe.name)
	}
	next.entries[pe.name] = pe
	m.snapshot.Store(next)
}

func TestDispatchEvent_AllSuccess(t *testing.T) {
	m := newTestManager(t)

	fastA := &mockExtensionServiceClient{
		dispatchEventFunc: func(ctx context.Context, in *api.DispatchEventRequest, opts ...grpc.CallOption) (*api.DispatchEventResponse, error) {
			return &api.DispatchEventResponse{
				Response: &api.EventResponse{InjectSystemPrompt: "from-a"},
			}, nil
		},
	}
	m.setPluginClientForTest("plugin-a", &api.GRPCClient{Client: fastA})

	fastB := &mockExtensionServiceClient{
		dispatchEventFunc: func(ctx context.Context, in *api.DispatchEventRequest, opts ...grpc.CallOption) (*api.DispatchEventResponse, error) {
			return &api.DispatchEventResponse{
				Response: &api.EventResponse{InjectSystemPrompt: "from-b"},
			}, nil
		},
	}
	m.setPluginClientForTest("plugin-b", &api.GRPCClient{Client: fastB})

	resp := m.DispatchEvent(context.Background(), "test_event", "session-1", nil)
	if resp == nil {
		t.Fatal("expected non-nil merged response")
		return
	}
	if resp.GetInjectSystemPrompt() != "from-a\nfrom-b" {
		t.Errorf("expected merged system prompt %q, got %q", "from-a\nfrom-b", resp.GetInjectSystemPrompt())
	}
}

func TestDispatchEvent_OneTimeout_OneSuccess(t *testing.T) {
	m := newTestManager(t)

	// Plugin A hangs until its context is cancelled.
	hanging := &mockExtensionServiceClient{
		dispatchEventFunc: func(ctx context.Context, in *api.DispatchEventRequest, opts ...grpc.CallOption) (*api.DispatchEventResponse, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	m.setPluginClientForTest("plugin-a", &api.GRPCClient{Client: hanging})

	fast := &mockExtensionServiceClient{
		dispatchEventFunc: func(ctx context.Context, in *api.DispatchEventRequest, opts ...grpc.CallOption) (*api.DispatchEventResponse, error) {
			return &api.DispatchEventResponse{
				Response: &api.EventResponse{InjectSystemPrompt: "from-fast"},
			}, nil
		},
	}
	m.setPluginClientForTest("plugin-b", &api.GRPCClient{Client: fast})

	resp := m.DispatchEvent(context.Background(), "test_event", "session-1", nil)
	if resp == nil {
		t.Fatal("expected non-nil response from fast plugin")
		return
	}
	if resp.GetInjectSystemPrompt() != "from-fast" {
		t.Errorf("expected InjectSystemPrompt %q, got %q", "from-fast", resp.GetInjectSystemPrompt())
	}
}

func TestDispatchEvent_AllTimeout(t *testing.T) {
	m := newTestManager(t)

	hang := func(ctx context.Context, in *api.DispatchEventRequest, opts ...grpc.CallOption) (*api.DispatchEventResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	m.setPluginClientForTest("plugin-a", &api.GRPCClient{Client: &mockExtensionServiceClient{dispatchEventFunc: hang}})
	m.setPluginClientForTest("plugin-b", &api.GRPCClient{Client: &mockExtensionServiceClient{dispatchEventFunc: hang}})

	resp := m.DispatchEvent(context.Background(), "test_event", "session-1", nil)
	if resp != nil {
		t.Errorf("expected nil response when all plugins timeout, got %+v", resp)
	}
}

func TestDispatchEvent_ParentContextCancelled(t *testing.T) {
	m := newTestManager(t)

	blockForever := func(ctx context.Context, in *api.DispatchEventRequest, opts ...grpc.CallOption) (*api.DispatchEventResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	a := &mockExtensionServiceClient{dispatchEventFunc: blockForever}
	m.setPluginClientForTest("plugin-a", &api.GRPCClient{Client: a})

	b := &mockExtensionServiceClient{dispatchEventFunc: blockForever}
	m.setPluginClientForTest("plugin-b", &api.GRPCClient{Client: b})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before dispatch

	resp := m.DispatchEvent(ctx, "test_event", "session-1", nil)
	// Both plugins should return immediately with context.Canceled.
	// We don't assert on the response value - the key is that this doesn't hang.
	_ = resp
}

func TestDispatchEvent_PluginNotFound(t *testing.T) {
	m := newTestManager(t)

	fast := &mockExtensionServiceClient{
		dispatchEventFunc: func(ctx context.Context, in *api.DispatchEventRequest, opts ...grpc.CallOption) (*api.DispatchEventResponse, error) {
			return &api.DispatchEventResponse{
				Response: &api.EventResponse{InjectSystemPrompt: "ok"},
			}, nil
		},
	}
	m.setPluginClientForTest("plugin-a", &api.GRPCClient{Client: fast})

	// plugin-b is not registered - it should be silently skipped.
	resp := m.DispatchEvent(context.Background(), "test_event", "session-1", nil)
	if resp == nil || resp.GetInjectSystemPrompt() != "ok" {
		t.Errorf("expected response from plugin-a only, got %+v", resp)
	}
}

func TestDispatchEvent_SessionLifecycleTimeout(t *testing.T) {
	m := newTestManager(t)

	hang := func(ctx context.Context, in *api.DispatchEventRequest, opts ...grpc.CallOption) (*api.DispatchEventResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	hanging := &mockExtensionServiceClient{dispatchEventFunc: hang}
	m.setPluginClientForTest("plugin-a", &api.GRPCClient{Client: hanging})

	// session_start with a hanging plugin should not block.
	resp := m.DispatchEvent(context.Background(), "session_start", "s-1", &api.EventPayload{
		Kind: &api.EventPayload_Session{Session: &api.SessionEventPayload{SessionId: "s-1"}},
	})
	if resp != nil {
		t.Errorf("expected nil response from timed-out session lifecycle dispatch")
	}

	// session_shutdown similarly.
	resp = m.DispatchEvent(context.Background(), "session_shutdown", "s-1", &api.EventPayload{
		Kind: &api.EventPayload_Session{Session: &api.SessionEventPayload{SessionId: "s-1"}},
	})
	if resp != nil {
		t.Errorf("expected nil response from timed-out session lifecycle dispatch")
	}
}

func TestExecutePluginTool_Timeout(t *testing.T) {
	m := newTestManager(t)

	hanging := &mockExtensionServiceClient{
		executeToolFunc: func(ctx context.Context, in *api.ExecuteToolRequest, opts ...grpc.CallOption) (*api.ExecuteToolResponse, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	m.setPluginClientForTest("plugin-a", &api.GRPCClient{Client: hanging})

	result, err := m.ExecutePluginTool(context.Background(), "plugin-a", "slow_tool", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("ExecutePluginTool should not return error (result wraps it): %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for timeout")
	}
	if result.Content == "" {
		t.Error("expected non-empty error content for timeout")
	}
}

func TestExecutePluginTool_Success(t *testing.T) {
	m := newTestManager(t)

	fast := &mockExtensionServiceClient{
		executeToolFunc: func(ctx context.Context, in *api.ExecuteToolRequest, opts ...grpc.CallOption) (*api.ExecuteToolResponse, error) {
			return &api.ExecuteToolResponse{
				Content: "tool result",
				IsError: false,
			}, nil
		},
	}
	m.setPluginClientForTest("plugin-a", &api.GRPCClient{Client: fast})

	result, err := m.ExecutePluginTool(context.Background(), "plugin-a", "fast_tool", json.RawMessage(`{"key":"value"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Error("expected IsError=false for successful tool execution")
	}
	if result.Content != "tool result" {
		t.Errorf("expected Content %q, got %q", "tool result", result.Content)
	}
}

func TestCommandHandles(t *testing.T) {
	cmd := chat.ExtensionCommand{
		Name: "mcp",
		Subcommands: []chat.ExtensionCommand{
			{Name: "list"}, {Name: "reconnect"}, {Name: "reload"},
		},
	}
	flat := chat.ExtensionCommand{Name: "deploy"}

	cases := []struct {
		cmd  chat.ExtensionCommand
		name string
		want bool
	}{
		{cmd, "mcp", true},           // bare group
		{cmd, "mcp list", true},      // declared sub-action
		{cmd, "mcp reconnect", true}, // declared sub-action
		{cmd, "mcp bogus", false},    // unknown sub-action
		{cmd, "other", false},        // different command
		{cmd, "mcplist", false},      // no space boundary
		{flat, "deploy", true},       // flat command still matches
		{flat, "deploy now", false},  // flat command has no sub-actions
	}
	for _, tc := range cases {
		if got := commandHandles(tc.cmd, tc.name); got != tc.want {
			t.Errorf("commandHandles(%q, %q) = %v, want %v", tc.cmd.Name, tc.name, got, tc.want)
		}
	}
}

func TestConfig_DefaultsApplied(t *testing.T) {
	m, err := NewManager(Config{
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.cfg.EventDispatchTimeout != DefaultEventDispatchTimeout {
		t.Errorf("expected EventDispatchTimeout=%v, got %v", DefaultEventDispatchTimeout, m.cfg.EventDispatchTimeout)
	}
	if m.cfg.ToolExecutionTimeout != DefaultToolExecutionTimeout {
		t.Errorf("expected ToolExecutionTimeout=%v, got %v", DefaultToolExecutionTimeout, m.cfg.ToolExecutionTimeout)
	}
}

func TestConfig_CustomTimeoutApplied(t *testing.T) {
	m, err := NewManager(Config{
		Logger:               slog.New(slog.DiscardHandler),
		EventDispatchTimeout: 5 * time.Second,
		ToolExecutionTimeout: 60 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.cfg.EventDispatchTimeout != 5*time.Second {
		t.Errorf("expected EventDispatchTimeout=5s, got %v", m.cfg.EventDispatchTimeout)
	}
	if m.cfg.ToolExecutionTimeout != 60*time.Second {
		t.Errorf("expected ToolExecutionTimeout=60s, got %v", m.cfg.ToolExecutionTimeout)
	}
}

func TestConfig_MaxViewsPerPluginDefaultApplied(t *testing.T) {
	m, err := NewManager(Config{
		Logger: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.cfg.MaxViewsPerPlugin != DefaultMaxViewsPerPlugin {
		t.Errorf("expected MaxViewsPerPlugin=%d, got %d", DefaultMaxViewsPerPlugin, m.cfg.MaxViewsPerPlugin)
	}
}

// TestUnload_ClosesOpenViewsForEveryPlugin verifies that killing plugins also
// closes any panels they left open, so a process kill (unload or reload)
// never leaves stale UI state on screen.
func TestUnload_ClosesOpenViewsForEveryPlugin(t *testing.T) {
	m := newTestManager(t)
	fvr := &fakeViewRenderer{}
	m.SetViewRenderer(fvr)

	m.setPluginClientForTest("plugin-a", &api.GRPCClient{Client: &mockExtensionServiceClient{}})
	m.setPluginClientForTest("plugin-b", &api.GRPCClient{Client: &mockExtensionServiceClient{}})

	ctx := context.Background()
	if _, err := m.host.RenderView(ctx, &api.RenderViewRequest{PluginName: "plugin-a", View: &api.View{Id: "panel-1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.host.RenderView(ctx, &api.RenderViewRequest{PluginName: "plugin-b", View: &api.View{Id: "panel-2"}}); err != nil {
		t.Fatal(err)
	}

	m.Unload(context.Background())

	if len(fvr.closed) != 2 {
		t.Errorf("expected 2 views closed on unload, got %d: %v", len(fvr.closed), fvr.closed)
	}
}
