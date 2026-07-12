package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/pkg/plugin/api"
)

// runTurn is the agentic turn loop. It streams a completion, and if the
// model returns tool_calls, executes them in parallel, appends results
// to the conversation, and loops. Stops when the model produces a final
// text response or an error occurs.
func (c *Coordinator) runTurn(ctx context.Context, state chat.ChatSessionState) {
	sessionID := state.SessionID
	requestID := state.ActiveRequestID
	now := time.Now().UTC()
	turnStartedAt := now

	c.mu.Lock()
	if s := c.sessions[sessionID]; s != nil {
		s.turnStartedAt = turnStartedAt
	}
	c.mu.Unlock()

	c.publishPluginLifecycleEvent("turn_start", sessionID, &api.EventPayload{
		Kind: &api.EventPayload_Turn{Turn: &api.TurnPayload{Direction: "start"}},
	})

	bearerToken, err := c.tokenSource(ctx, state.Provider)
	if err != nil {
		c.loggerWithTurn(sessionID, requestID).Debug(
			"turn failed — token source error",
			"err", err,
		)
		c.failTurn(sessionID, requestID, err, now)
		return
	}

	c.loggerWithTurn(sessionID, requestID).Debug(
		"turn loop begin",
		"provider", state.ProviderName,
		"model", state.Model.ID,
	)

	// The turn loop: call LLM, if tool_calls → execute → append → repeat.
	for iteration := 0; ; iteration++ {
		if err := ctx.Err(); err != nil {
			c.cancelTurn(sessionID, requestID, time.Now().UTC())
			return
		}

		// Check budget/limit enforcement.
		c.turnCount++
		if status, partial := c.checkLimits(sessionID, requestID); status != "" {
			c.budgetExhausted(sessionID, requestID, status, partial, time.Now().UTC())
			return
		}

		// Inject any steering messages that arrived during tool execution
		// or at the turn start.
		c.injectSteering(sessionID)
		state = c.getSessionState(sessionID)

		// Build tool definitions from registry each iteration so the
		// AllowedTools filter (set by the Skill tool) takes effect.
		state.Tools = c.buildToolDefs()
		reasoningSnapshot := ""
		toolCallSnapshots := make([]chat.ChatToolCall, 0)

		// Emit context event with the full message list so plugins can
		// observe and mutate conversation state before the LLM call.
		contextResp := c.dispatchPluginRequestResponse("context", sessionID, &api.EventPayload{
			Kind: &api.EventPayload_Context{Context: &api.ContextPayload{
				Messages: marshalMessages(state.Messages),
			}},
		})
		if contextResp != nil {
			c.applyPluginMessageModifications(&state, contextResp)
			// Sync mutations back to the stored session so they persist
			// across turn iterations (tool-call loops).
			c.mu.Lock()
			if s, ok := c.sessions[sessionID]; ok {
				s.state.Messages = state.Messages
			}
			c.mu.Unlock()
		}

		if iteration == 0 {
			compactedState, compacted, compactErr := c.maybeAutoCompact(ctx, state, bearerToken)
			if compactErr != nil {
				c.loggerWithTurn(sessionID, requestID).Warn(
					"auto-compaction failed",
					"err", compactErr,
				)
				c.emit(chat.ChatNotificationEvent{
					Message:    "auto-compaction failed: " + compactErr.Error(),
					Level:      chat.ChatNotificationWarn,
					OccurredAt: time.Now().UTC(),
				})
			} else if compacted {
				state = compactedState
			}
		}

		pluginResp := c.dispatchPluginRequestResponse("before_llm_call", sessionID, &api.EventPayload{
			Kind: &api.EventPayload_BeforeLlmCall{
				BeforeLlmCall: &api.BeforeLLMCallPayload{
					ModelId:    state.Model.ID,
					Headers:    map[string]string{"Authorization": "Bearer " + bearerToken},
					Messages:   marshalMessages(state.Messages),
					Parameters: marshalParameters(state.Parameters),
				},
			},
		})

		var extraHeaders map[string]string
		if pluginResp != nil && len(pluginResp.GetAddHeaders()) > 0 {
			extraHeaders = pluginResp.GetAddHeaders()
		}

		// Apply LLM-boundary modifiers for this call only.
		originalSystemPrompt := state.SystemPrompt
		originalModelID := state.Model.ID
		if pluginResp != nil {
			if pluginResp.GetInjectSystemPrompt() != "" {
				state.SystemPrompt = state.SystemPrompt + "\n" + pluginResp.GetInjectSystemPrompt()
			}
			if pluginResp.GetModifiedModelId() != "" {
				state.Model.ID = pluginResp.GetModifiedModelId()
			}
		}

		c.loggerWithTurn(sessionID, requestID).Debug(
			"llm call start",
			"iteration", iteration,
			"model", state.Model.ID,
			"provider", state.ProviderName,
			"msg_count", len(state.Messages),
			"tool_count", len(state.Tools),
		)

		result, err := c.streamer.StreamChatCompletionFull(ctx, state, bearerToken, extraHeaders, chat.StreamCallbacks{
			OnDelta: func(delta string) error {
				return c.appendDelta(sessionID, requestID, delta, time.Now().UTC())
			},
			OnReasoningDelta: func(delta string) error {
				reasoningSnapshot += delta
				c.emitReasoningDelta(sessionID, requestID, delta, reasoningSnapshot, time.Now().UTC())
				return nil
			},
			OnToolCallDelta: func(tcd chat.ChatToolCallDelta) error {
				toolCallSnapshots = mergeToolCallDelta(toolCallSnapshots, tcd)
				call := toolCallSnapshots[tcd.Index]
				callID := call.ID
				if callID == "" {
					callID = fmt.Sprintf("tool_call_%d", tcd.Index)
				}
				argsSummary, truncated := summarizeForUIWithTruncation(call.Function.Arguments)
				c.emit(chat.ChatToolCallDeltaEvent{
					SessionID:        sessionID,
					RequestID:        requestID,
					CallID:           callID,
					Index:            tcd.Index,
					ToolName:         call.Function.Name,
					ArgumentsSummary: argsSummary,
					Truncated:        truncated,
					ReceivedAt:       time.Now().UTC(),
				})
				return nil
			},
		})
		if err != nil {
			// Restore per-call overrides before error handling.
			state.SystemPrompt = originalSystemPrompt
			state.Model.ID = originalModelID
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
				c.loggerWithTurn(sessionID, requestID).Debug(
					"llm call cancelled",
					"iteration", iteration,
					"err", err,
				)
				c.cancelTurn(sessionID, requestID, time.Now().UTC())
				return
			}
			c.loggerWithTurn(sessionID, requestID).Warn(
				"llm call failed",
				"iteration", iteration,
				"err", err,
			)
			c.failTurn(sessionID, requestID, err, time.Now().UTC())
			return
		}

		// Snapshot effective model ID before restoring, so after_llm_call
		// reports the model that was actually used.
		effectiveModelID := state.Model.ID

		// Restore per-call overrides.
		state.SystemPrompt = originalSystemPrompt
		state.Model.ID = originalModelID

		c.publishPluginLifecycleEvent("after_llm_call", sessionID, &api.EventPayload{
			Kind: &api.EventPayload_AfterLlmCall{
				AfterLlmCall: &api.AfterLLMCallPayload{
					ModelId:      effectiveModelID,
					FinishReason: result.FinishReason,
				},
			},
		})

		c.loggerWithTurn(sessionID, requestID).Debug(
			"llm call complete",
			"iteration", iteration,
			"finish_reason", result.FinishReason,
			"input_tokens", result.Usage.PromptTokens,
			"output_tokens", result.Usage.CompletionTokens,
			"total_tokens", result.Usage.TotalTokens,
			"tool_calls", len(result.ToolCalls),
		)

		// Accumulate token usage for budget enforcement.
		c.cumulativeTokens += result.Usage.TotalTokens

		// No tool calls → final response. Complete the turn.
		if len(result.ToolCalls) == 0 {
			c.completeTurn(sessionID, requestID, result, time.Now().UTC())
			return
		}

		// Tool calls detected. Validate arguments before committing —
		// malformed JSON in tool call arguments poisons the session history
		// and causes downstream API 400 errors on subsequent turns.
		toolNames := make([]string, len(result.ToolCalls))
		for i, tc := range result.ToolCalls {
			toolNames[i] = tc.Function.Name
		}
		c.loggerWithTurn(sessionID, requestID).Debug(
			"tool calls detected",
			"iteration", iteration,
			"count", len(result.ToolCalls),
			"tools", toolNames,
		)

		sanitized := sanitizeToolCallArguments(result.ToolCalls)
		if sanitized > 0 {
			c.emitMetrics(chat.MetricEvent{
				Category: chat.MetricCategoryTool,
				Name:     "tool.arguments.malformed",
				Value:    float64(sanitized),
				Unit:     "count",
				Labels: map[string]string{
					"total_tool_calls": fmt.Sprintf("%d", len(result.ToolCalls)),
				},
				SessionID: sessionID,
			})
		}

		// Commit the assistant's response with sanitized tool calls.
		c.commitAssistantMessage(sessionID, c.getPendingContent(sessionID), result.ReasoningContent, result.ToolCalls)

		// Execute tool calls in parallel.
		toolResults, hardStopReason := c.executeToolsParallel(ctx, sessionID, requestID, result.ToolCalls)

		// Append tool result messages to the session.
		for i, tc := range result.ToolCalls {
			c.appendToolResult(sessionID, requestID, tc, toolResults[i])
		}

		// The tool-call loop breaker tripped: a call was blocked without
		// ever being justified toolLoopHardBlockLimit times in a row. End
		// the turn now rather than let it iterate forever.
		if hardStopReason != "" {
			c.failTurn(sessionID, requestID, errors.New(hardStopReason), time.Now().UTC())
			return
		}

		// Refresh state for the next iteration.
		state = c.getSessionState(sessionID)
		if state.SessionID == "" {
			return // session gone
		}

		// Clear pending assistant for the next LLM call.
		c.clearPending(sessionID)
	}
}

// toolLoopSoftThreshold is how many consecutive identical tool calls (same
// name+arguments, ignoring the repeat_justification escape hatch below) are
// allowed before the coordinator starts requiring the model to explicitly
// justify continuing.
const toolLoopSoftThreshold = 3

// toolLoopHardBlockLimit ends the turn outright once a call has been
// blocked this many times in a row without ever being justified — a
// backstop for a model that never engages with the block message at all
// (e.g. a model stuck in decoding-level token repetition, which by
// definition can't spontaneously produce a novel justification string).
// There is deliberately no cap on justified repeats: a model that keeps
// affirming a genuinely repeated action (re-running the same test, polling
// for a state change) can continue indefinitely.
const toolLoopHardBlockLimit = 3

// toolLoopVerdict is what checkToolLoop decided for one tool call.
type toolLoopVerdict struct {
	blocked   bool // true: don't execute the real tool; return message as a synthetic error result instead
	hardStop  bool // true: end the whole turn, not just this call
	justified bool // true: at/past the threshold, but let through by an explicit repeat_justification
	streak    int  // consecutive identical calls so far, for logging/metrics
	blockedN  int  // consecutive unjustified blocks so far, for logging/metrics
	message   string
}

// parseToolCallKey unmarshals the tool call arguments once and returns both
// the normalized comparison key (name+args, excluding repeat_justification)
// and any non-empty repeat_justification string.
func parseToolCallKey(name, argsJSON string) (key string, justification string) {
	var m map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &m); err != nil {
		return name + "\x00" + argsJSON, ""
	}
	s, _ := m["repeat_justification"].(string)
	justification = strings.TrimSpace(s)
	delete(m, "repeat_justification")
	normalized, err := json.Marshal(m)
	if err != nil {
		return name + "\x00" + argsJSON, justification
	}
	return name + "\x00" + string(normalized), justification
}

// checkToolLoop tracks consecutive identical tool calls (same name+args,
// ignoring repeat_justification) per session, to break a decoding-level
// repetition loop — a real incident had a model call grep with byte-
// identical arguments 103 times in a row, each getting a valid, non-empty,
// identical result, without ever producing a final answer. Past
// toolLoopSoftThreshold, a call must carry a non-empty top-level
// "repeat_justification" argument to proceed; toolLoopHardBlockLimit
// consecutive unjustified blocks for the same call ends the turn outright.
func (c *Coordinator) checkToolLoop(sessionID string, tc chat.ChatToolCall) toolLoopVerdict {
	c.mu.Lock()
	sess := c.sessions[sessionID]
	c.mu.Unlock()
	if sess == nil {
		return toolLoopVerdict{}
	}

	key, justification := parseToolCallKey(tc.Function.Name, tc.Function.Arguments)

	sess.toolLoopMu.Lock()
	defer sess.toolLoopMu.Unlock()

	if key != sess.lastToolKey {
		sess.lastToolKey = key
		sess.lastToolStreak = 1
		sess.lastToolBlocked = 0
		return toolLoopVerdict{}
	}
	sess.lastToolStreak++

	if sess.lastToolStreak <= toolLoopSoftThreshold {
		return toolLoopVerdict{}
	}
	if justification != "" {
		sess.lastToolBlocked = 0
		return toolLoopVerdict{justified: true, streak: sess.lastToolStreak}
	}

	sess.lastToolBlocked++
	if sess.lastToolBlocked >= toolLoopHardBlockLimit {
		return toolLoopVerdict{
			hardStop: true,
			streak:   sess.lastToolStreak,
			blockedN: sess.lastToolBlocked,
			message: fmt.Sprintf(
				"tool %q was called %d times in a row with identical arguments and blocked %d times without ever being justified — stopping the turn to avoid a runaway loop",
				tc.Function.Name, sess.lastToolStreak, sess.lastToolBlocked,
			),
		}
	}
	return toolLoopVerdict{
		blocked:  true,
		streak:   sess.lastToolStreak,
		blockedN: sess.lastToolBlocked,
		message: fmt.Sprintf(
			"This exact %s call has now been made %d times in a row with identical arguments. If repeating it is genuinely necessary (e.g. re-running the same test, polling for a state change), call it again with an added top-level argument %s explaining why — otherwise, try a different approach.",
			tc.Function.Name, sess.lastToolStreak, `"repeat_justification": "<short reason>"`,
		),
	}
}

// executeToolsParallel runs tool calls and returns results in input order.
// When parallelToolCalls is true, calls run concurrently; otherwise sequentially.
func (c *Coordinator) executeToolsParallel(ctx context.Context, sessionID, requestID string, calls []chat.ChatToolCall) (results []tools.Result, hardStopReason string) {
	results = make([]tools.Result, len(calls))

	toolNames := make([]string, len(calls))
	for i, tc := range calls {
		toolNames[i] = tc.Function.Name
	}
	c.loggerWithTurn(sessionID, requestID).Debug(
		"executing tools",
		"count", len(calls),
		"tools", toolNames,
		"parallel", c.parallelToolCalls,
	)

	var hardStopMu sync.Mutex

	executeTool := func(i int, tc chat.ChatToolCall) {
		startedAt := time.Now().UTC()
		effectiveArgs := tc.Function.Arguments

		loopVerdict := c.checkToolLoop(sessionID, tc)
		switch {
		case loopVerdict.hardStop:
			hardStopMu.Lock()
			if hardStopReason == "" {
				hardStopReason = loopVerdict.message
			}
			hardStopMu.Unlock()
			c.loggerWithTurn(sessionID, requestID).Warn(
				"tool loop breaker: hard stop",
				"tool", tc.Function.Name,
				"call_id", tc.ID,
				"streak", loopVerdict.streak,
				"blocked", loopVerdict.blockedN,
			)
			c.emitMetrics(chat.MetricEvent{
				Category:  chat.MetricCategoryTool,
				Name:      "tool.loop.hard_stop",
				Value:     1,
				Unit:      "count",
				Labels:    map[string]string{"tool": tc.Function.Name, "streak": fmt.Sprintf("%d", loopVerdict.streak)},
				SessionID: sessionID,
			})

		case loopVerdict.blocked:
			c.loggerWithTurn(sessionID, requestID).Debug(
				"tool loop breaker: blocked pending justification",
				"tool", tc.Function.Name,
				"call_id", tc.ID,
				"streak", loopVerdict.streak,
				"blocked", loopVerdict.blockedN,
			)
			c.emitMetrics(chat.MetricEvent{
				Category:  chat.MetricCategoryTool,
				Name:      "tool.loop.blocked",
				Value:     1,
				Unit:      "count",
				Labels:    map[string]string{"tool": tc.Function.Name, "streak": fmt.Sprintf("%d", loopVerdict.streak)},
				SessionID: sessionID,
			})

		case loopVerdict.justified:
			c.loggerWithTurn(sessionID, requestID).Debug(
				"tool loop breaker: justified override accepted",
				"tool", tc.Function.Name,
				"call_id", tc.ID,
				"streak", loopVerdict.streak,
			)
			c.emitMetrics(chat.MetricEvent{
				Category:  chat.MetricCategoryTool,
				Name:      "tool.loop.justified",
				Value:     1,
				Unit:      "count",
				Labels:    map[string]string{"tool": tc.Function.Name, "streak": fmt.Sprintf("%d", loopVerdict.streak)},
				SessionID: sessionID,
			})
		}

		// Lifecycle event: tool execution is about to start.
		c.publishPluginLifecycleEvent("tool_execution_start", sessionID, &api.EventPayload{
			Kind: &api.EventPayload_BeforeToolExec{
				BeforeToolExec: &api.ToolCallPayload{
					ToolName:  tc.Function.Name,
					Arguments: tc.Function.Arguments,
					CallId:    tc.ID,
				},
			},
		})

		// Mutation hook: plugins may block the tool or modify its arguments.
		beforeResp := c.dispatchPluginRequestResponse("before_tool_exec", sessionID, &api.EventPayload{
			Kind: &api.EventPayload_BeforeToolExec{
				BeforeToolExec: &api.ToolCallPayload{
					ToolName:  tc.Function.Name,
					Arguments: tc.Function.Arguments,
					CallId:    tc.ID,
				},
			},
		})

		// Determine the result, applying permission gates and argument rewriting.
		// The started event fires AFTER mutation hooks so it reflects effective args.
		var result tools.Result
		var toolErr error

		bridge := &loggingUIBridge{
			UIBridge:  c.uiBridge,
			sessionID: sessionID,
			requestID: requestID,
			callID:    tc.ID,
			c:         c,
		}

		// Resolve which tool.Execute call (if any) this turn needs, without
		// running it yet, so the started event — and the "pending" ->
		// "running" transition it drives in the UI — fires before a
		// long-running tool actually executes rather than after it
		// returns (previously the whole call sat in "pending" for its
		// entire duration).
		var run func() (tools.Result, error)
		switch {
		case loopVerdict.blocked || loopVerdict.hardStop:
			result = tools.Result{Content: loopVerdict.message, IsError: true}

		case beforeResp != nil && beforeResp.GetBlockToolExecution():
			reason := beforeResp.GetBlockReason()
			if reason == "" {
				reason = "tool execution blocked by plugin"
			}
			result = tools.Result{Content: reason, IsError: true}

		case beforeResp != nil && beforeResp.GetModifiedToolArguments() != "":
			if !json.Valid([]byte(beforeResp.GetModifiedToolArguments())) {
				result = tools.Result{
					Content: "plugin returned invalid modified_tool_arguments (not valid JSON)",
					IsError: true,
				}
			} else {
				effectiveArgs = beforeResp.GetModifiedToolArguments()
				if tool, ok := c.registry.Get(tc.Function.Name); ok && c.toolAllowed(tc.Function.Name) {
					run = func() (tools.Result, error) {
						return tool.Execute(ctx, json.RawMessage(effectiveArgs), bridge)
					}
				} else {
					result = tools.Result{
						Content: fmt.Sprintf("unknown tool: %s", tc.Function.Name),
						IsError: true,
					}
				}
			}

		default:
			if tool, ok := c.registry.Get(tc.Function.Name); ok && c.toolAllowed(tc.Function.Name) {
				run = func() (tools.Result, error) {
					return tool.Execute(ctx, json.RawMessage(effectiveArgs), bridge)
				}
			} else {
				result = tools.Result{
					Content: fmt.Sprintf("unknown tool: %s", tc.Function.Name),
					IsError: true,
				}
			}
		}

		// Emit started event with effective args AFTER plugin hooks but
		// BEFORE tool.Execute runs, so the UI reflects "running" for the
		// actual duration of the call instead of only after it completes.
		c.emit(chat.ChatToolExecutionStartedEvent{
			SessionID:        sessionID,
			RequestID:        requestID,
			CallID:           tc.ID,
			ToolName:         tc.Function.Name,
			ArgumentsSummary: summarizeForUI(effectiveArgs),
			Summary:          extractToolCallSummary(effectiveArgs),
			StartedAt:        startedAt,
		})

		if run != nil {
			result, toolErr = run()
			if toolErr != nil {
				result = tools.Result{
					Content: fmt.Sprintf("tool execution error: %v", toolErr),
					IsError: true,
				}
			}
		}

		// Mutation hook: plugins may modify the result. Dispatch on all paths.
		afterResp := c.dispatchPluginRequestResponse("after_tool_exec", sessionID, &api.EventPayload{
			Kind: &api.EventPayload_AfterToolExec{
				AfterToolExec: &api.ToolResultPayload{
					ToolName:  tc.Function.Name,
					Arguments: effectiveArgs,
					Result:    result.Content,
					IsError:   result.IsError,
					CallId:    tc.ID,
				},
			},
		})
		if afterResp != nil && afterResp.GetModifiedToolResult() != "" {
			result.Content = afterResp.GetModifiedToolResult()
		}

		// Preserve the original size before truncating model-visible content.
		if result.ResultBytes == 0 {
			result.ResultBytes = len(result.Content)
		}

		// Truncate after plugin modifications.
		tr := tools.TruncateHead(result.Content, tools.DefaultMaxLines, tools.DefaultMaxBytes)
		result.Content = tr.Content
		result.StartedAt = startedAt
		result.CompletedAt = time.Now().UTC()
		result.Duration = result.CompletedAt.Sub(startedAt)
		result.Truncated = result.Truncated || tr.Truncated
		if result.IsError && result.ErrorKind == "" {
			result.ErrorKind = toolErrorKind(tc.Function.Name, result.Content)
		}

		results[i] = result
		c.loggerWithTurn(sessionID, requestID).Debug(
			"tool executed",
			"tool", tc.Function.Name,
			"call_id", tc.ID,
			"is_error", result.IsError,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"result_bytes", len(result.Content),
		)
		c.emitToolCompleted(sessionID, requestID, tc, result)

		// Skill activations via the LLM-invoked "skill" tool (the dominant
		// path) need their own metric; handleRunSkill covers the user-command
		// slash command path.
		if tc.Function.Name == "skill" && !result.IsError {
			skillName := tc.Function.Name // fallback
			if args := strings.TrimSpace(tc.Function.Arguments); args != "" {
				var skillArgs struct {
					Name string `json:"name"`
				}
				if json.Unmarshal([]byte(args), &skillArgs) == nil && skillArgs.Name != "" {
					skillName = skillArgs.Name
				}
			}
			c.emitMetrics(chat.MetricEvent{
				Category:  chat.MetricCategorySkill,
				Name:      "skill.activated",
				Value:     1,
				Unit:      "count",
				Labels:    map[string]string{"skill_name": skillName},
				SessionID: sessionID,
			})
		}

		c.publishPluginLifecycleEvent("tool_execution_end", sessionID, &api.EventPayload{
			Kind: &api.EventPayload_AfterToolExec{
				AfterToolExec: &api.ToolResultPayload{
					ToolName: tc.Function.Name,
					CallId:   tc.ID,
				},
			},
		})
	}

	if c.parallelToolCalls {
		var wg sync.WaitGroup
		for i, tc := range calls {
			wg.Add(1)
			go func(i int, tc chat.ChatToolCall) {
				defer wg.Done()
				executeTool(i, tc)
			}(i, tc)
		}
		wg.Wait()
	} else {
		for i, tc := range calls {
			executeTool(i, tc)
		}
	}

	return results, hardStopReason
}

// SetAllowedTools sets the allowed tool filter for the next LLM call.
// When non-empty, only tools whose names are in the set are included in
// the tool schemas sent to the LLM. The "skill" tool is always allowed so
// that the model can switch skills mid-conversation.
// SetAllowedTools sets the active tool filter for the current mode/skill.
// The filter is always intersected with the coordinator's immutable effectiveTools
// ceiling (set at construction from --tools / spec.tools). An empty or nil
// toolNames list reverts to the effectiveTools ceiling; passing specific names
// narrows the active set below the ceiling but never widens beyond it.
func (c *Coordinator) SetAllowedTools(toolNames []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Empty list: revert to ceiling (or nil if unrestricted).
	if len(toolNames) == 0 {
		c.allowedTools = nil
		return
	}

	m := make(map[string]bool, len(toolNames)+1)
	for _, name := range toolNames {
		// Intersect with the immutable ceiling: a mode can never widen.
		if c.effectiveTools == nil || c.effectiveTools[name] {
			m[name] = true
		}
	}
	// Always allow switching skills regardless of ceiling.
	m["skill"] = true
	c.allowedTools = m
}

// toolAllowed returns true if the named tool is permitted by both the
// effectiveTools ceiling and the allowedTools active filter. Used as an
// execution-time guard: a tool hidden from the LLM by buildToolDefs must
// also be blocked if the LLM hallucinates it anyway.
func (c *Coordinator) toolAllowed(name string) bool {
	c.mu.Lock()
	effective := c.effectiveTools
	allowed := c.allowedTools
	c.mu.Unlock()

	if effective != nil && !effective[name] {
		return false
	}
	if allowed != nil && !allowed[name] {
		return false
	}
	return true
}

func (c *Coordinator) buildToolDefs() []chat.ChatToolDef {
	schemas := c.registry.Schemas()

	c.mu.Lock()
	effective := c.effectiveTools
	allowed := c.allowedTools
	c.mu.Unlock()

	var filtered []tools.Schema
	for _, s := range schemas {
		// Apply immutable ceiling first, then active mode filter.
		// nil means "no restriction" — the tool passes that tier.
		if effective != nil && !effective[s.Name] {
			continue
		}
		if allowed != nil && !allowed[s.Name] {
			continue
		}
		filtered = append(filtered, s)
	}

	defs := make([]chat.ChatToolDef, len(filtered))
	for i, s := range filtered {
		defs[i] = chat.ChatToolDef{
			Type: "function",
			Function: chat.ChatToolDefFunction{
				Name:        s.Name,
				Description: s.Description,
				Parameters:  injectSummarySchemaProperty(s.Parameters),
			},
		}
	}
	return defs
}

func mergeToolCallDelta(calls []chat.ChatToolCall, delta chat.ChatToolCallDelta) []chat.ChatToolCall {
	for len(calls) <= delta.Index {
		calls = append(calls, chat.ChatToolCall{Type: "function"})
	}

	tc := &calls[delta.Index]
	if delta.ID != "" {
		tc.ID = delta.ID
	}
	if delta.Type != "" {
		tc.Type = delta.Type
	}
	if delta.Function.Name != "" {
		// The function name arrives whole in a single delta for every provider
		// observed so far (unlike Arguments, which streams token-by-token) —
		// some providers (e.g. deepseek) resend the full name on every
		// subsequent delta for the same call rather than sending it once, so
		// this must replace, not accumulate, or the name duplicates once per
		// delta (e.g. "shell" -> "shellshellshell...").
		tc.Function.Name = delta.Function.Name
	}
	if delta.Function.Arguments != "" {
		tc.Function.Arguments += delta.Function.Arguments
	}
	return calls
}

// sanitizeToolCallArguments checks each tool call's arguments for valid JSON.
// Malformed arguments are replaced with "{}" so they don't poison the session
// history and cause downstream API 400 errors on subsequent turns. Returns the
// number of arguments that were sanitized.
func sanitizeToolCallArguments(calls []chat.ChatToolCall) int {
	sanitized := 0
	for i := range calls {
		args := strings.TrimSpace(calls[i].Function.Arguments)
		if args == "" {
			calls[i].Function.Arguments = "{}"
			sanitized++
			continue
		}
		if !json.Valid([]byte(args)) {
			calls[i].Function.Arguments = "{}"
			sanitized++
		}
	}
	return sanitized
}

func (c *Coordinator) emitReasoningDelta(sessionID, requestID, delta, snapshot string, at time.Time) {
	c.emit(chat.ChatReasoningDeltaEvent{
		SessionID:  sessionID,
		RequestID:  requestID,
		Delta:      delta,
		Snapshot:   snapshot,
		ReceivedAt: at,
	})
}

// emitToolCompleted always renders tool results as a text summary via
// summarizeForUI, even for plugin-provided tools - ExecuteToolResponse has
// no View field. The chat.Widget/ExtensionView schema (see
// ExtensionCommandResultEvent) is intentionally reusable here later without
// a breaking wire change, but that's out of scope for this phase.
func (c *Coordinator) emitToolCompleted(
	sessionID string,
	requestID string,
	tc chat.ChatToolCall,
	result tools.Result,
) {
	status := "success"
	if result.IsError {
		status = "error"
	}
	event := chat.ChatToolExecutionCompletedEvent{
		SessionID:     sessionID,
		RequestID:     requestID,
		CallID:        tc.ID,
		ToolName:      tc.Function.Name,
		Status:        status,
		Duration:      result.Duration,
		ResultSummary: summarizeResultForUI(result.Content),
		IsError:       result.IsError,
		Truncated:     result.Truncated,
		CompletedAt:   result.CompletedAt,
		Details:       result.Details,
	}
	c.emit(event)
	state := c.getSessionState(sessionID)
	metricLabels := make(map[string]string, len(result.MetricLabels)+9)
	maps.Copy(metricLabels, result.MetricLabels)
	// Authoritative correlation fields are assigned last so tool-specific
	// dimensions cannot override execution identity or status.
	maps.Copy(metricLabels, map[string]string{
		"tool":         tc.Function.Name,
		"status":       status,
		"call_id":      tc.ID,
		"request_id":   requestID,
		"error_kind":   result.ErrorKind,
		"truncated":    strconv.FormatBool(result.Truncated),
		"result_bytes": strconv.Itoa(result.ResultBytes),
		"provider":     state.ProviderName,
		"model":        state.Model.ID,
	})
	c.emitMetrics(chat.MetricEvent{
		Category:  chat.MetricCategoryTool,
		Name:      "tool." + tc.Function.Name + ".duration",
		Value:     float64(event.Duration.Milliseconds()),
		Unit:      "ms",
		Labels:    metricLabels,
		SessionID: sessionID,
		RequestID: requestID,
		CallID:    tc.ID,
	})
}

func summarizeForUI(value string) string {
	summary, _ := summarizeForUIWithTruncation(value)
	return summary
}

func summarizeForUIWithTruncation(value string) (string, bool) {
	value = strings.TrimSpace(value)
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= toolSummaryMaxBytes {
		return value, false
	}
	return value[:toolSummaryMaxBytes] + "…", true
}

func summarizeResultForUI(value string) string {
	value = strings.TrimSpace(value)
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	value = strings.Join(lines, "\n")
	if len(value) <= toolSummaryMaxBytes {
		return value
	}
	return value[:toolSummaryMaxBytes] + "…"
}

// summarizeResultForUI truncates like summarizeForUI but preserves line
// breaks (only collapsing horizontal whitespace runs within each line) so
// multi-line tool output (ls, grep, etc.) still renders as rows rather than
// one run-together line in consumers like tui2's expanded tool box.
// injectSummarySchemaProperty adds an optional "summary" string property to
// a tool's JSON schema so the model can supply a one-line description of
// what a given call is doing, surfaced live in the status bar while it
// runs. Applied centrally in buildToolDefs rather than per-tool so every
// tool gets it without each definition needing to declare it. Falls back to
// the unmodified schema if it isn't a JSON object or already declares the
// property — defensive, since every tool schema is expected to be a plain
// object and none currently uses this name.
func injectSummarySchemaProperty(params json.RawMessage) json.RawMessage {
	if len(params) == 0 {
		return params
	}
	var schema map[string]any
	if err := json.Unmarshal(params, &schema); err != nil {
		return params
	}
	props, _ := schema["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
	}
	if _, exists := props[toolCallSummaryProperty]; exists {
		return params
	}
	props[toolCallSummaryProperty] = map[string]any{
		"type":        "string",
		"description": "Optional one-line (<=80 char) description of what this call is doing, shown live in the UI while it runs. Not used by the tool itself.",
	}
	schema["properties"] = props
	out, err := json.Marshal(schema)
	if err != nil {
		return params
	}
	return out
}

// extractToolCallSummary pulls the optional model-authored "summary" field
// out of a tool call's arguments for display in the status bar. It never
// fails the call: malformed JSON, a missing field, or a wrong type all just
// yield "". What it does return is sanitized for direct terminal rendering —
// control/escape characters stripped, collapsed to a single line, and
// truncated — since it comes straight from the model.
func extractToolCallSummary(argsJSON string) string {
	var probe struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &probe); err != nil {
		return ""
	}
	return sanitizeToolCallSummary(probe.Summary)
}

func sanitizeToolCallSummary(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return ' '
		case r < 0x20 || r == 0x7f:
			return -1 // drop control/escape characters, incl. ESC (0x1b)
		default:
			return r
		}
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	if runes := []rune(s); len(runes) > toolCallSummaryMaxChars {
		s = strings.TrimSpace(string(runes[:toolCallSummaryMaxChars])) + "…"
	}
	return s
}

// isSuccessFinish returns true for finish reasons that indicate a normal, successful
// completion. All other reasons (including empty, "error", "content_filter",
// "length", "cancelled") are treated as error outcomes for metric purposes.
func isSuccessFinish(reason string) bool {
	return reason == "stop" || reason == "tool_calls"
}

// computeCost calculates the USD cost of a completion from model pricing
// and usage data. Returns 0 when pricing is not configured.
func computeCost(costCfg tauconfig.CostConfig, usage chat.ChatUsage) float64 {
	if costCfg.Input == 0 && costCfg.Output == 0 {
		return 0
	}
	inputCost := float64(usage.PromptTokens) * costCfg.Input / chat.CostPerMillionTokens
	outputCost := float64(usage.CompletionTokens) * costCfg.Output / chat.CostPerMillionTokens
	return inputCost + outputCost
}

// emitMetrics publishes a MetricEvent on the bus. It is a no-op when no
// subscribers are listening for MetricEvent, avoiding allocation on the
// hot path (tool execution, LLM response).
func (c *Coordinator) emitMetrics(e chat.MetricEvent) {
	if c.metricsPub == nil || !c.metricsPub.ShouldPublish() {
		return
	}
	e.Timestamp = time.Now().UTC()
	c.metricsPub.Publish(e)
}

func (c *Coordinator) appendDelta(sessionID, requestID, delta string, at time.Time) error {
	c.mu.Lock()
	session, ok := c.sessions[sessionID]
	if !ok {
		c.mu.Unlock()
		return errors.New("session not found")
	}
	if session.state.ActiveRequestID != requestID {
		c.mu.Unlock()
		return context.Canceled
	}
	if err := session.state.AppendAssistantDelta(delta, at); err != nil {
		c.mu.Unlock()
		return err
	}
	snapshot := session.state.PendingAssistant
	c.mu.Unlock()

	c.emit(chat.ChatResponseDeltaEvent{
		SessionID:  sessionID,
		RequestID:  requestID,
		Delta:      delta,
		Snapshot:   snapshot,
		ReceivedAt: at,
	})
	return nil
}

func (c *Coordinator) getPendingContent(sessionID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	session, ok := c.sessions[sessionID]
	if !ok {
		return ""
	}
	return session.state.PendingAssistant
}

func (c *Coordinator) commitAssistantMessage(sessionID string, content string, reasoningContent string, calls []chat.ChatToolCall) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session, ok := c.sessions[sessionID]
	if !ok {
		return
	}
	_ = session.state.AppendAssistantToolCallMessageWithReasoning(content, reasoningContent, calls, time.Now().UTC())
}

func (c *Coordinator) appendToolResult(sessionID, requestID string, tc chat.ChatToolCall, result tools.Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session, ok := c.sessions[sessionID]
	if !ok {
		return
	}
	status := "success"
	if result.IsError {
		status = "error"
	}
	metadata := &chat.ToolResultMetadata{
		ToolName: tc.Function.Name, RequestID: requestID, Status: status,
		ErrorKind: result.ErrorKind, Duration: result.Duration,
		Truncated: result.Truncated, ResultBytes: result.ResultBytes,
		StartedAt: result.StartedAt, CompletedAt: result.CompletedAt,
	}
	_ = session.state.AppendToolResultMessageWithMetadata(tc.ID, result.Content, metadata, result.CompletedAt)
}

func toolErrorKind(toolName, content string) string {
	lower := strings.ToLower(strings.TrimSpace(content))
	switch {
	case strings.HasPrefix(lower, "[timeout after"):
		return "timeout"
	case strings.HasPrefix(lower, "[exit code:"):
		return "command_exit"
	case strings.Contains(lower, "unknown tool"):
		return "unknown_tool"
	case strings.Contains(lower, "blocked by plugin"):
		return "blocked"
	case strings.Contains(lower, "invalid parameter"), strings.Contains(lower, " is required"), strings.Contains(lower, "must not be empty"):
		return "invalid_arguments"
	case strings.Contains(lower, "escapes working directory"):
		return "sandbox_escape"
	case toolName == "edit" && strings.Contains(lower, "old_text"):
		return "stale_edit"
	default:
		return "execution_error"
	}
}

func (c *Coordinator) clearPending(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session, ok := c.sessions[sessionID]
	if !ok {
		return
	}
	session.state.ClearPendingAssistant(time.Now().UTC())
}

func (c *Coordinator) getSessionState(sessionID string) chat.ChatSessionState {
	c.mu.Lock()
	defer c.mu.Unlock()
	session, ok := c.sessions[sessionID]
	if !ok {
		return chat.ChatSessionState{}
	}
	return chat.CloneChatSessionState(session.state)
}

func (c *Coordinator) completeTurn(sessionID, requestID string, result chat.CompletionResult, at time.Time) {
	c.mu.Lock()
	session, ok := c.sessions[sessionID]
	if !ok || session.state.ActiveRequestID != requestID {
		c.mu.Unlock()
		return
	}
	turnStartedAt := session.turnStartedAt
	_ = session.state.CompleteTurnWithReasoning(result.FinishReason, result.Usage, result.ReasoningContent, at)
	session.cancel = nil
	session.steeringMu.Lock()
	session.pendingSteering = nil
	session.steeringMu.Unlock()
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	c.loggerWithTurn(sessionID, requestID).Debug(
		"turn completed",
		"finish_reason", result.FinishReason,
		"total_tokens", result.Usage.TotalTokens,
		"duration_ms", time.Since(turnStartedAt).Milliseconds(),
	)

	// Emit LLM latency metric when we have a turn start timestamp.
	if !turnStartedAt.IsZero() {
		c.emitMetrics(chat.MetricEvent{
			Category: chat.MetricCategoryLLM,
			Name:     "turn.duration",
			Value:    float64(time.Since(turnStartedAt).Milliseconds()),
			Unit:     "ms",
			Labels: map[string]string{
				"provider": snapshot.Provider.Name,
				"model":    snapshot.Model.ID,
			},
			SessionID: sessionID,
		})
	}

	// Emit error metric for non-success finish reasons.  error_kind
	// differentiates between cancellation, content filtering, length
	// truncation, and provider-specific refusals (e.g. Gemini safety).
	if !isSuccessFinish(result.FinishReason) {
		c.emitMetrics(chat.MetricEvent{
			Category: chat.MetricCategoryLLM,
			Name:     "llm.error",
			Value:    1,
			Unit:     "count",
			Labels: map[string]string{
				"provider":      snapshot.Provider.Name,
				"model":         snapshot.Model.ID,
				"finish_reason": result.FinishReason,
			},
			SessionID: sessionID,
		})
	}

	// Emit LLM response metrics with token counts and cost.
	cost := computeCost(snapshot.Model.Config.Cost, result.Usage)
	c.emitMetrics(chat.MetricEvent{
		Category: chat.MetricCategoryLLM,
		Name:     "llm.response",
		Value:    float64(result.Usage.TotalTokens),
		Unit:     "tokens",
		Labels: map[string]string{
			"provider":          snapshot.Provider.Name,
			"model":             snapshot.Model.ID,
			"prompt_tokens":     fmt.Sprintf("%d", result.Usage.PromptTokens),
			"completion_tokens": fmt.Sprintf("%d", result.Usage.CompletionTokens),
			"finish_reason":     result.FinishReason,
		},
		SessionID: sessionID,
	})
	if cost > 0 {
		c.emitMetrics(chat.MetricEvent{
			Category:  chat.MetricCategoryLLM,
			Name:      "llm.cost",
			Value:     cost,
			Unit:      "usd",
			Labels:    map[string]string{"provider": snapshot.Provider.Name, "model": snapshot.Model.ID},
			SessionID: sessionID,
		})
	}

	c.emit(chat.ChatResponseCompletedEvent{
		State:        snapshot,
		RequestID:    requestID,
		FinishReason: result.FinishReason,
		Usage:        result.Usage,
		CompletedAt:  at,
	})

	c.publishPluginLifecycleEvent("turn_end", sessionID, &api.EventPayload{
		Kind: &api.EventPayload_Turn{Turn: &api.TurnPayload{Direction: "end"}},
	})
}

func (c *Coordinator) cancelTurn(sessionID, requestID string, at time.Time) {
	c.mu.Lock()
	session, ok := c.sessions[sessionID]
	if !ok || session.state.ActiveRequestID != requestID {
		c.mu.Unlock()
		return
	}
	_ = session.state.CancelTurn(at)
	session.cancel = nil
	session.steeringMu.Lock()
	session.pendingSteering = nil
	session.steeringMu.Unlock()
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	c.loggerWithTurn(sessionID, requestID).Debug(
		"turn cancelled",
		"provider", snapshot.Provider.Name,
		"model", snapshot.Model.ID,
	)

	c.emitMetrics(chat.MetricEvent{
		Category: chat.MetricCategoryLLM,
		Name:     "llm.error",
		Value:    1,
		Unit:     "count",
		Labels: map[string]string{
			"provider":      snapshot.Provider.Name,
			"model":         snapshot.Model.ID,
			"finish_reason": "cancelled",
		},
		SessionID: sessionID,
	})

	c.emit(chat.ChatResponseCancelledEvent{
		State:       snapshot,
		RequestID:   requestID,
		CancelledAt: at,
	})
}

func (c *Coordinator) failTurn(sessionID, requestID string, err error, at time.Time) {
	c.mu.Lock()
	session, ok := c.sessions[sessionID]
	if !ok || session.state.ActiveRequestID != requestID {
		c.mu.Unlock()
		return
	}
	_ = session.state.FailTurn(err, at)
	session.cancel = nil
	session.steeringMu.Lock()
	session.pendingSteering = nil
	session.steeringMu.Unlock()
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	c.loggerWithTurn(sessionID, requestID).Warn(
		"turn failed",
		"provider", snapshot.Provider.Name,
		"model", snapshot.Model.ID,
		"err", err,
	)

	c.emitMetrics(chat.MetricEvent{
		Category: chat.MetricCategoryLLM,
		Name:     "llm.error",
		Value:    1,
		Unit:     "count",
		Labels: map[string]string{
			"provider":      snapshot.Provider.Name,
			"model":         snapshot.Model.ID,
			"finish_reason": "error",
			"error_kind":    "stream_error",
		},
		SessionID: sessionID,
	})

	c.emit(chat.ChatSessionSnapshotEvent{State: snapshot})
	c.emit(chat.ChatRuntimeErrorEvent{
		SessionID:  sessionID,
		RequestID:  requestID,
		Message:    err.Error(),
		Fatal:      false,
		OccurredAt: at,
	})
}

func (c *Coordinator) cancelAllSessions() {
	c.mu.Lock()
	states := make([]chat.ChatSessionState, 0, len(c.sessions))
	sessionIDs := make([]string, 0, len(c.sessions))
	for id, session := range c.sessions {
		if session.cancel != nil {
			session.cancel()
		}
		states = append(states, chat.CloneChatSessionState(session.state))
		sessionIDs = append(sessionIDs, id)
	}
	c.mu.Unlock()

	for _, state := range states {
		sessionID := state.SessionID
		// Dedup shutdown events (preserving existing dedup logic).
		c.mu.Lock()
		if _, exists := c.shutdown[sessionID]; exists {
			c.mu.Unlock()
			c.persistSession(state, time.Since(state.CreatedAt))
			continue
		}
		c.shutdown[sessionID] = struct{}{}
		c.mu.Unlock()
		c.publishPluginLifecycleEvent("session_shutdown", sessionID, &api.EventPayload{
			Kind: &api.EventPayload_Session{Session: &api.SessionEventPayload{
				SessionId: sessionID,
				ModelId:   state.Model.ID,
				Provider:  state.Provider.Name,
			}},
		})
		c.persistSession(state, time.Since(state.CreatedAt))
	}

	// Release session and shutdown map entries after all sessions are persisted.
	c.mu.Lock()
	for _, id := range sessionIDs {
		delete(c.sessions, id)
	}
	c.shutdown = make(map[string]struct{})
	c.mu.Unlock()
}

// checkLimits returns the breach status ("", "budget_exhausted", or
// "timed_out") and whether partial output is available. An empty status
// means no limit has been breached — continue the turn.
func (c *Coordinator) checkLimits(sessionID, requestID string) (status string, partial bool) {
	// Structural turn cap.
	if c.maxTurns > 0 && c.turnCount > c.maxTurns {
		return "budget_exhausted", true
	}

	// Wall-clock: check deadline first (more specific), then timeout.
	n := time.Now()
	if !c.deadline.IsZero() && n.After(c.deadline) {
		return "timed_out", true
	}
	if c.timeout > 0 && n.After(c.startedAt.Add(c.timeout)) {
		return "timed_out", true
	}

	// Token budget.
	if c.maxTokens > 0 && c.cumulativeTokens >= c.maxTokens {
		return "budget_exhausted", true
	}

	return "", false
}

// budgetExhausted emits a ChatResponseCompletedEvent with a budget/timed_out
// status and the partial output flag set. It leaves the session in a
// completed state so the caller (child entry) can persist and report result.
func (c *Coordinator) budgetExhausted(sessionID, requestID, status string, partialOutput bool, at time.Time) {
	c.mu.Lock()
	session, ok := c.sessions[sessionID]
	if !ok || session.state.ActiveRequestID != requestID {
		c.mu.Unlock()
		return
	}

	// Force-terminate the turn: consume any pending assistant text and mark complete.
	pending := session.state.PendingAssistant
	session.state.PendingAssistant = ""
	session.state.ActiveRequestID = ""
	if pending != "" {
		msg := chat.ChatMessage{
			Role:    chat.ChatRoleAssistant,
			Content: pending,
		}
		session.state.Messages = append(session.state.Messages, msg)
	}
	session.state.Status = chat.ChatSessionIdle
	session.cancel = nil
	session.steeringMu.Lock()
	session.pendingSteering = nil
	session.steeringMu.Unlock()
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	// Emit the completed event with budget/timed_out metadata.
	event := chat.ChatResponseCompletedEvent{
		State:           snapshot,
		RequestID:       requestID,
		CompletedAt:     at,
		BudgetExhausted: status == "budget_exhausted",
		TimedOut:        status == "timed_out",
		Partial:         partialOutput,
	}
	c.emit(event)
}
