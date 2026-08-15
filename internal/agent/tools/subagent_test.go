package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// sessionBridge is a UIBridge stub that reports a fixed session ID, so the
// subagent tool can be verified to forward the calling session to its runner.
type sessionBridge struct{ sessionID string }

func (b sessionBridge) SessionID() string { return b.sessionID }
func (sessionBridge) Confirm(context.Context, string, string) (bool, error) {
	return false, ErrInteractiveUnsupported
}

func (sessionBridge) Select(context.Context, string, []string) (string, error) {
	return "", ErrInteractiveUnsupported
}

func (sessionBridge) Input(context.Context, string, string) (string, error) {
	return "", ErrInteractiveUnsupported
}
func (sessionBridge) Notify(string, string) {}
func (sessionBridge) Log(string)            {}

func TestRegisterSubagentToolRequiresRunner(t *testing.T) {
	t.Parallel()

	require.Error(t, RegisterSubagentTool(NewRegistry(), nil), "nil runner must be rejected")
}

func TestSubagentToolForwardsSessionAndPrompt(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	var gotSessionID, gotPrompt string
	runner := SubagentRunnerFunc(func(_ context.Context, sessionID, prompt string) (string, error) {
		gotSessionID, gotPrompt = sessionID, prompt
		return "sub-agent conclusion", nil
	})
	require.NoError(t, RegisterSubagentTool(reg, runner))

	tool, ok := reg.Get("subagent")
	require.True(t, ok, "subagent tool must be registered")
	require.Equal(t, "builtin", tool.Source)
	require.Equal(t, "subagent", tool.Schema.Name)

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"prompt":"analyze the parser"}`), sessionBridge{sessionID: "sess-abc"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, "sub-agent conclusion", res.Content)
	require.Equal(t, "sess-abc", gotSessionID)
	require.Equal(t, "analyze the parser", gotPrompt)
}

func TestSubagentToolEmptySessionStillRuns(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	runner := SubagentRunnerFunc(func(_ context.Context, sessionID, prompt string) (string, error) {
		require.Equal(t, "", sessionID)
		return "ok", nil
	})
	require.NoError(t, RegisterSubagentTool(reg, runner))

	tool, _ := reg.Get("subagent")
	// NonInteractiveBridge reports "" — the runner decides what to do with
	// an unknown session (e.g. reject when no model can be resolved).
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"prompt":"analyze"}`), NonInteractiveBridge{})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, "ok", res.Content)
}

func TestSubagentToolRejectsBadArguments(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	runner := SubagentRunnerFunc(func(context.Context, string, string) (string, error) {
		t.Fatal("runner must not be called with invalid arguments")
		return "", nil
	})
	require.NoError(t, RegisterSubagentTool(reg, runner))
	tool, _ := reg.Get("subagent")

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"prompt":""}`), NonInteractiveBridge{})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Content, "prompt is required")

	res, err = tool.Execute(context.Background(), json.RawMessage(`not-json`), NonInteractiveBridge{})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Contains(t, res.Content, "invalid parameters")
}

func TestSubagentToolSurfacesRunnerFailureInBand(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	runner := SubagentRunnerFunc(func(context.Context, string, string) (string, error) {
		return "", errors.New("nesting depth exceeded")
	})
	require.NoError(t, RegisterSubagentTool(reg, runner))
	tool, _ := reg.Get("subagent")

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"prompt":"deep task"}`), NonInteractiveBridge{})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Equal(t, "subagent_error", res.ErrorKind)
	require.Contains(t, res.Content, "sub-agent failed")
	require.Contains(t, res.Content, "nesting depth exceeded")
}
