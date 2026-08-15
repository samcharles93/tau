package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// SubagentRunner executes a delegated subtask as a nested agent running in a
// fresh context window (the parent's history is never sent). sessionID is the
// calling session ("" when unknown); the runner resolves the sub-agent's
// provider/model from it. Run returns the sub-agent's final conclusion text.
type SubagentRunner interface {
	Run(ctx context.Context, sessionID, prompt string) (string, error)
}

// SubagentRunnerFunc adapts a plain function to SubagentRunner.
type SubagentRunnerFunc func(ctx context.Context, sessionID, prompt string) (string, error)

// Run implements SubagentRunner.
func (f SubagentRunnerFunc) Run(ctx context.Context, sessionID, prompt string) (string, error) {
	return f(ctx, sessionID, prompt)
}

// RegisterSubagentTool registers the built-in "subagent" delegation tool: it
// delegates a self-contained subtask to a nested agent that works in a fresh
// context window and returns its conclusion, so the parent can hand off
// analysis or synthesis that would otherwise bloat its own context.
//
// The runner is provided by the coordinator (it owns session state and the
// ai-sdk runtime wiring); this function only defines the tool surface and
// argument parsing. Failures are returned in-band as error results so the
// model sees a clear, retryable message rather than a bare Go error.
func RegisterSubagentTool(reg *Registry, runner SubagentRunner) error {
	if runner == nil {
		return errors.New("subagent runner is required")
	}
	return reg.Register(Tool{
		Schema: Schema{
			Name:        "subagent",
			Description: "Delegate a self-contained subtask to a nested agent that works in a fresh context window and returns its conclusion. Use for analysis or synthesis that would otherwise bloat this conversation.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"prompt": {
						"type": "string",
						"description": "The self-contained subtask to delegate. The sub-agent runs it in a fresh context window and returns its conclusion."
					}
				},
				"required": ["prompt"]
			}`),
		},
		Execute: func(ctx context.Context, params json.RawMessage, ui UIBridge) (Result, error) {
			var args struct {
				Prompt string `json:"prompt"`
			}
			if err := json.Unmarshal(params, &args); err != nil {
				//nolint:nilerr // Folded into the in-band rejection so the model sees a clear, retryable message; a bare Go error would surface as an opaque string.
				return Result{Content: "subagent call failed: invalid parameters: " + err.Error(), IsError: true, ErrorKind: "invalid_params"}, nil
			}
			if strings.TrimSpace(args.Prompt) == "" {
				return Result{Content: "subagent call failed: prompt is required", IsError: true, ErrorKind: "invalid_params"}, nil
			}
			sessionID := ""
			if ui != nil {
				sessionID = ui.SessionID()
			}
			out, err := runner.Run(ctx, sessionID, args.Prompt)
			if err != nil {
				//nolint:nilerr // Folded into the in-band rejection so the model sees the failure and can adapt its next call.
				return Result{Content: "sub-agent failed: " + err.Error(), IsError: true, ErrorKind: "subagent_error"}, nil
			}
			return Result{Content: out}, nil
		},
		Source: "builtin",
	})
}
