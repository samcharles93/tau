package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	aisagent "github.com/samcharles93/ai-sdk/agent"
	aiscore "github.com/samcharles93/ai-sdk/core"
	aisruntime "github.com/samcharles93/ai-sdk/runtime"
	aistoolkit "github.com/samcharles93/ai-sdk/toolkit"

	"github.com/samcharles93/tau/internal/agent"
)

// subagentSystemPrompt frames the nested agent as a focused, autonomous
// worker: it must return a self-contained conclusion and must not ask the
// parent for clarification mid-task.
const subagentSystemPrompt = `You are a focused sub-agent delegated a single, self-contained task by the main Tau agent. Work autonomously using your tools. Do not ask for clarification; make reasonable assumptions and state them. Return your conclusion as complete, self-contained text: findings, code, or explanation.`

// newSubagentExecutor builds the executor the coordinator invokes when the
// parent agent calls the "subagent" tool. Each call runs a fresh nested
// agent (ai-sdk agent.Subagent) in a new context window — the parent's
// history is never sent — on the provider/model the calling session is
// currently using, and returns the sub-agent's final conclusion.
//
// The sub-agent's toolset is analysis plus verification: read/grep/find and
// shell. File mutations (edit/write) are deliberately excluded so a nested
// agent cannot change the workspace without the parent reviewing the result.
func newSubagentExecutor(rt *aisruntime.Runtime, cwd string) agent.SubagentExecutor {
	tools := subagentToolset(cwd)
	return func(ctx context.Context, provider, model, prompt string) (string, error) {
		prov, modelID, err := rt.ChatProvider(ctx, provider+"/"+model)
		if err != nil {
			return "", fmt.Errorf("resolving sub-agent provider: %w", err)
		}
		sub := aisagent.Subagent{
			Provider: prov,
			Model:    modelID,
			System:   subagentSystemPrompt,
			Tools:    tools,
		}
		out, err := sub.Run(ctx, prompt)
		if err != nil {
			return "", fmt.Errorf("sub-agent: %w", err)
		}
		return out, nil
	}
}

// subagentExecutorOrNil returns the subagent executor when a runtime is
// available, or nil — which disables the "subagent" tool — when it is not
// (e.g. headless paths that never resolve providers).
func subagentExecutorOrNil(rt *aisruntime.Runtime, cwd string) agent.SubagentExecutor {
	if rt == nil {
		return nil
	}
	return newSubagentExecutor(rt, cwd)
}

// subagentToolset returns the core.ToolSet the sub-agent may call, adapted
// from the ai-sdk toolkit. NonInteractiveBridge keeps the nested run silent:
// a delegated task must not raise interactive prompts.
func subagentToolset(cwd string) aiscore.ToolSet {
	reg := aistoolkit.NewRegistry()
	if err := aistoolkit.RegisterBuiltins(reg, cwd); err != nil {
		slog.Warn("subagent toolkit registration failed; sub-agent will be analysis-only", "err", err)
	}
	allowed := map[string]bool{"read": true, "grep": true, "find": true, "shell": true}
	set := make(aiscore.ToolSet)
	for _, tool := range reg.All() {
		if !allowed[tool.Schema.Name] {
			continue
		}
		set[tool.Schema.Name] = aiscore.NewTool(
			tool.Schema.Name,
			tool.Schema.Description,
			tool.Schema.Parameters,
			func(ctx context.Context, input string) (string, error) {
				res, err := tool.Execute(ctx, json.RawMessage(input), aistoolkit.NonInteractiveBridge{})
				if err != nil {
					if ctx.Err() != nil {
						return "", err
					}
					return "tool error: " + err.Error(), nil
				}
				return res.Content, nil
			},
		)
	}
	return set
}
