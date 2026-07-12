package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
)

// handleRunBashCommand runs a "!" (or "!!") bash-mode command entered
// directly at the chat input. It executes the same registered "shell" tool
// the LLM itself uses, outside the normal turn loop, and emits the same
// started/output/completed event trio a real LLM-invoked tool call would —
// so it renders identically in the TUI — before appending the result to
// session history (unless Exclude, the "!!" variant, is set).
func (c *Coordinator) handleRunBashCommand(cmd chat.RunBashCommand) {
	now := normalizedTime(cmd.RequestedAt)

	c.mu.Lock()
	session, ok := c.sessions[cmd.SessionID]
	if !ok || session.state == nil {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    "session not found",
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	ctx, cancel := context.WithCancel(c.ctx)
	session.bashCancel = cancel
	c.mu.Unlock()

	// Executing the tool blocks on the subprocess for the command's whole
	// duration, so it must run off the command-dispatch goroutine — handleCommand
	// is called synchronously from the coordinator's single-threaded command
	// loop (run()), and blocking there would stall every other command
	// (including a CancelBashCommand for this very command) until it finished.
	c.turnWG.Go(func() {
		c.runBashCommand(ctx, cancel, cmd, now)
	})
}

// runBashCommand executes the shell tool for a "!" bash command and reports
// its result, off the coordinator's command-dispatch goroutine (see
// handleRunBashCommand).
func (c *Coordinator) runBashCommand(ctx context.Context, cancel context.CancelFunc, cmd chat.RunBashCommand, now time.Time) {
	defer cancel()

	c.emit(chat.ChatToolExecutionStartedEvent{
		SessionID:        cmd.SessionID,
		CallID:           cmd.CallID,
		ToolName:         "shell",
		ArgumentsSummary: summarizeForUI(cmd.Command),
		StartedAt:        now,
	})

	bridge := &loggingUIBridge{UIBridge: c.uiBridge, sessionID: cmd.SessionID, callID: cmd.CallID, c: c}
	tool, ok := c.registry.Get("shell")
	var result tools.Result
	if !ok {
		result = tools.Result{Content: "shell tool unavailable", IsError: true}
	} else {
		argsJSON, _ := json.Marshal(tools.ShellParams{Command: cmd.Command})
		var toolErr error
		result, toolErr = tool.Execute(ctx, argsJSON, bridge)
		if toolErr != nil {
			result = tools.Result{Content: toolErr.Error(), IsError: true}
		}
	}

	c.mu.Lock()
	session, ok := c.sessions[cmd.SessionID]
	if ok && session.bashCancel != nil {
		session.bashCancel = nil
	}
	// Append to history before emitting the completed event, so observers
	// that treat "completed" as "fully done" never race a subsequent read of
	// session state against this append.
	if ok && !cmd.Exclude {
		content := fmt.Sprintf("Ran `%s`\n\n```\n%s\n```", cmd.Command, result.Content)
		_ = session.state.AppendStandaloneMessage(chat.ChatRoleUser, content, time.Now().UTC())
	}
	c.mu.Unlock()

	result.StartedAt = now
	result.CompletedAt = time.Now().UTC()
	result.Duration = result.CompletedAt.Sub(now)
	if result.ResultBytes == 0 {
		result.ResultBytes = len(result.Content)
	}
	if result.IsError && result.ErrorKind == "" {
		result.ErrorKind = toolErrorKind("shell", result.Content)
	}
	c.emitToolCompleted(cmd.SessionID, "", chat.ChatToolCall{ID: cmd.CallID, Function: chat.ChatFunctionCall{Name: "shell"}}, result)
}

// handleCancelBash stops the session's in-flight bash-mode command, if any.
func (c *Coordinator) handleCancelBash(cmd chat.CancelBashCommand) {
	c.mu.Lock()
	session, ok := c.sessions[cmd.SessionID]
	var cancel context.CancelFunc
	if ok {
		cancel = session.bashCancel
	}
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
