package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
)

// fakeShellTool is a deterministic stand-in for the real "shell" tool
// (internal/agent/tools/shell.go), avoiding a dependency on an actual shell
// being available in the test environment. If release is non-nil, Execute
// blocks until release is closed or ctx is cancelled, so cancellation can be
// exercised; started (if non-nil) is closed once Execute has begun.
func fakeShellTool(started chan<- struct{}, release <-chan struct{}) tools.Tool {
	return tools.Tool{
		Schema: tools.Schema{Name: "shell", Description: "fake shell for tests"},
		Execute: func(ctx context.Context, params json.RawMessage, bridge tools.UIBridge) (tools.Result, error) {
			var p tools.ShellParams
			_ = json.Unmarshal(params, &p)
			bridge.Log("output for: " + p.Command)
			if started != nil {
				close(started)
			}
			if release != nil {
				select {
				case <-release:
				case <-ctx.Done():
					return tools.Result{Content: "cancelled", IsError: true}, nil
				}
			}
			return tools.Result{Content: "ok: " + p.Command}, nil
		},
	}
}

// TestCoordinatorHandleRunBashCommand_EndToEnd verifies a "!" bash command
// emits the same started/output/completed event trio a real LLM-invoked
// shell tool call would, and appends its result to session history.
func TestCoordinatorHandleRunBashCommand_EndToEnd(t *testing.T) {
	bus := newTestBus(t)
	sub := eventbus.Subscribe[chat.ChatEvent](bus.Client("test"))
	defer sub.Close()

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(fakeShellTool(nil, nil)))

	c, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: bus,
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer: noopStreamer{},
		Registry: reg,
	})
	require.NoError(t, err)
	defer c.Close()

	startTestSession(t, c)

	require.NoError(t, c.Send(chat.RunBashCommand{
		SessionID:   "session-1",
		CallID:      "call-1",
		Command:     "echo hi",
		RequestedAt: time.Now().UTC(),
	}))

	var sawStarted, sawOutput, sawCompleted bool
	deadline := time.After(2 * time.Second)
	for !sawStarted || !sawOutput || !sawCompleted {
		select {
		case ev := <-sub.Events():
			switch e := ev.(type) {
			case chat.ChatToolExecutionStartedEvent:
				require.Equal(t, "call-1", e.CallID)
				require.Equal(t, "shell", e.ToolName)
				sawStarted = true
			case chat.ChatToolOutputEvent:
				require.Equal(t, "call-1", e.CallID)
				sawOutput = true
			case chat.ChatToolExecutionCompletedEvent:
				require.Equal(t, "call-1", e.CallID)
				require.False(t, e.IsError)
				sawCompleted = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for events: started=%v output=%v completed=%v", sawStarted, sawOutput, sawCompleted)
		}
	}

	c.mu.Lock()
	session, ok := c.sessions["session-1"]
	require.True(t, ok)
	messages := session.state.Messages
	c.mu.Unlock()

	require.Len(t, messages, 1)
	require.Equal(t, chat.ChatRoleUser, messages[0].Role)
	require.Contains(t, messages[0].Content, "echo hi")
	require.Contains(t, messages[0].Content, "ok: echo hi")
}

// TestCoordinatorHandleRunBashCommand_ExcludeSkipsHistory verifies a "!!"
// command (Exclude: true) still runs and emits the normal event trio, but
// its result is not appended to session history.
func TestCoordinatorHandleRunBashCommand_ExcludeSkipsHistory(t *testing.T) {
	bus := newTestBus(t)
	sub := eventbus.Subscribe[chat.ChatEvent](bus.Client("test"))
	defer sub.Close()

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(fakeShellTool(nil, nil)))

	c, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: bus,
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer: noopStreamer{},
		Registry: reg,
	})
	require.NoError(t, err)
	defer c.Close()

	startTestSession(t, c)

	require.NoError(t, c.Send(chat.RunBashCommand{
		SessionID:   "session-1",
		CallID:      "call-1",
		Command:     "echo secret",
		Exclude:     true,
		RequestedAt: time.Now().UTC(),
	}))

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-sub.Events():
			if _, ok := ev.(chat.ChatToolExecutionCompletedEvent); ok {
				goto completed
			}
		case <-deadline:
			t.Fatal("timed out waiting for completion event")
		}
	}
completed:

	c.mu.Lock()
	session, ok := c.sessions["session-1"]
	require.True(t, ok)
	messages := session.state.Messages
	c.mu.Unlock()

	require.Empty(t, messages, "excluded (!!) bash command must not be appended to history")
}

// TestCoordinatorHandleRunBashCommand_RealShellTool exercises the actual
// registered shell tool (internal/agent/tools/shell.go) rather than a fake,
// running a real subprocess end to end through handleRunBashCommand - the
// same tool.Execute call an LLM-invoked "shell" tool call would use.
func TestCoordinatorHandleRunBashCommand_RealShellTool(t *testing.T) {
	bus := newTestBus(t)
	sub := eventbus.Subscribe[chat.ChatEvent](bus.Client("test"))
	defer sub.Close()

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(tools.NewShellTool("", tools.NewMutationQueue())))

	c, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: bus,
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer: noopStreamer{},
		Registry: reg,
	})
	require.NoError(t, err)
	defer c.Close()

	startTestSession(t, c)

	require.NoError(t, c.Send(chat.RunBashCommand{
		SessionID:   "session-1",
		CallID:      "call-1",
		Command:     "echo real-subprocess-output",
		RequestedAt: time.Now().UTC(),
	}))

	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-sub.Events():
			if e, ok := ev.(chat.ChatToolExecutionCompletedEvent); ok {
				require.Equal(t, "call-1", e.CallID)
				require.False(t, e.IsError)
				require.Contains(t, e.ResultSummary, "real-subprocess-output")
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for real shell command to complete")
		}
	}
}

// TestCoordinatorHandleCancelBash verifies CancelBashCommand cancels the
// running command's context rather than being a no-op or affecting turn
// cancellation.
func TestCoordinatorHandleCancelBash(t *testing.T) {
	bus := newTestBus(t)
	sub := eventbus.Subscribe[chat.ChatEvent](bus.Client("test"))
	defer sub.Close()

	started := make(chan struct{})
	release := make(chan struct{}) // never closed - only ctx cancellation unblocks Execute

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(fakeShellTool(started, release)))

	c, err := NewCoordinator(context.Background(), CoordinatorConfig{
		Bus: bus,
		TokenSource: func(context.Context, config.ProviderConfig) (string, error) {
			return "", nil
		},
		Streamer: noopStreamer{},
		Registry: reg,
	})
	require.NoError(t, err)
	defer c.Close()

	startTestSession(t, c)

	require.NoError(t, c.Send(chat.RunBashCommand{
		SessionID:   "session-1",
		CallID:      "call-1",
		Command:     "sleep 30",
		RequestedAt: time.Now().UTC(),
	}))

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("fake shell tool never started")
	}

	require.NoError(t, c.Send(chat.CancelBashCommand{
		SessionID:   "session-1",
		RequestedAt: time.Now().UTC(),
	}))

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-sub.Events():
			if e, ok := ev.(chat.ChatToolExecutionCompletedEvent); ok {
				require.True(t, e.IsError, "cancelled bash command should complete as an error")
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for cancellation to complete the bash command")
		}
	}
}
