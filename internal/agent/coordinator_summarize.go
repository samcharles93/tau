package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agentspec "github.com/samcharles93/tau/internal/agent/spec"
	"github.com/samcharles93/tau/internal/chat"
)

// fallbackSummarizeSystemPrompt is used when the embedded "summarise" agent
// spec can't be loaded (it always can today - the templates are embedded -
// but the handler must not fail because of it).
const fallbackSummarizeSystemPrompt = "You are a precise conversation summarizer. " +
	"Summarize the conversation you are shown, capturing the user's goals, decisions made, " +
	"files and code touched, open questions, and next steps. Be dense and factual; no preamble."

// summarizeInstruction is the final user message appended after the
// conversation window. It states explicitly that the summary is display-only,
// so the model doesn't write it as if the history were being replaced (the
// embedded summarise spec is written for takeover-style summaries).
func summarizeInstruction(lastN, total int) string {
	if lastN > 0 && lastN < total {
		return fmt.Sprintf(
			"Summarize the conversation above (the last %d of %d messages). "+
				"This summary is shown to the user only; the conversation history is NOT being modified or replaced.",
			lastN, total,
		)
	}
	return "Summarize the conversation above. " +
		"This summary is shown to the user only; the conversation history is NOT being modified or replaced."
}

// handleSummarizeSession runs a NON-destructive summarization of the
// session's conversation in response to SummarizeSessionCommand (/summarize).
// The session's message history is cloned for the LLM call and never written
// back - the result is emitted only as a SessionSummarizedEvent. The work
// runs on turnWG (not the command loop) so a slow summarization call doesn't
// block other commands like cancel.
func (c *Coordinator) handleSummarizeSession(ctx context.Context, cmd chat.SummarizeSessionCommand) {
	now := normalizedTime(cmd.RequestedAt)

	c.mu.Lock()
	session, ok := c.sessions[cmd.SessionID]
	if !ok {
		c.mu.Unlock()
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  cmd.SessionID,
			Message:    "session not found",
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}
	state := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	if len(state.Messages) == 0 {
		c.emit(chat.ChatNotificationEvent{
			Message:    "nothing to summarize yet - the conversation is empty",
			Level:      chat.ChatNotificationInfo,
			OccurredAt: now,
		})
		return
	}
	if strings.TrimSpace(state.Model.ID) == "" || strings.TrimSpace(state.Provider.Name) == "" {
		c.emit(chat.ChatNotificationEvent{
			Message:    "no model selected for this session - pick one with /model first",
			Level:      chat.ChatNotificationWarn,
			OccurredAt: now,
		})
		return
	}

	c.turnWG.Go(func() {
		c.runSummarize(ctx, state, cmd.LastN, now)
	})
}

// runSummarize performs the actual LLM summarization call against a cloned,
// tool-less copy of the session state. It never touches c.sessions.
func (c *Coordinator) runSummarize(ctx context.Context, state chat.ChatSessionState, lastN int, requestedAt time.Time) {
	sessionID := state.SessionID
	now := time.Now().UTC()

	bearerToken, err := c.tokenSource(ctx, state.Provider)
	if err != nil {
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  sessionID,
			Message:    "summarize: " + err.Error(),
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}

	messages := summarizeWindow(state.Messages, lastN)

	summaryState := chat.CloneChatSessionState(&state)
	summaryState.SystemPrompt = summarizeSystemPrompt(c.projectDir)
	summaryState.Messages = append([]chat.ChatMessage(nil), messages...)
	summaryState.Messages = append(summaryState.Messages, chat.ChatMessage{
		ID:        chat.NewMessageID(),
		Role:      chat.ChatRoleUser,
		Content:   summarizeInstruction(lastN, len(state.Messages)),
		CreatedAt: now,
	})
	// Tools disabled: summarization is a pure read of the history.
	summaryState.Tools = nil
	summaryState.PendingAssistant = ""
	summaryState.ActiveRequestID = ""
	summaryState.Status = chat.ChatSessionIdle

	var summary strings.Builder
	result, err := c.streamer.StreamChatCompletionFull(ctx, summaryState, bearerToken, nil, chat.StreamCallbacks{
		OnDelta: func(delta string) error {
			summary.WriteString(delta)
			return nil
		},
	})
	if err != nil {
		if errors.Is(err, context.Canceled) && requestedAt.Before(now) {
			// Shutdown/cancel during summarization - not a user-facing failure.
			c.loggerWith(sessionID).Debug("summarize cancelled", "err", err)
			return
		}
		c.emit(chat.ChatRuntimeErrorEvent{
			SessionID:  sessionID,
			Message:    "summarize failed: " + err.Error(),
			Fatal:      false,
			OccurredAt: time.Now().UTC(),
		})
		return
	}
	summaryText := strings.TrimSpace(summary.String())
	if summaryText == "" {
		c.emit(chat.ChatNotificationEvent{
			Message:    "summarize produced an empty summary",
			Level:      chat.ChatNotificationWarn,
			OccurredAt: time.Now().UTC(),
		})
		return
	}

	c.emit(chat.SessionSummarizedEvent{
		SessionID:    sessionID,
		Summary:      summaryText,
		MessageCount: len(messages),
		Usage:        result.Usage,
		OccurredAt:   time.Now().UTC(),
	})
	c.emitMetrics(chat.MetricEvent{
		Category:  chat.MetricCategorySession,
		Name:      "session.summarized",
		Value:     1,
		Unit:      "count",
		SessionID: sessionID,
		Labels: map[string]string{
			"message_count": fmt.Sprintf("%d", len(messages)),
			"summary_bytes": fmt.Sprintf("%d", len(summaryText)),
			"total_tokens":  fmt.Sprintf("%d", result.Usage.TotalTokens),
		},
	})
}

// summarizeWindow returns the messages to summarize: the whole conversation,
// or the last lastN messages. When a window starts mid-tool-exchange (its
// first message is a tool result whose originating assistant message fell
// outside the window), leading tool messages are dropped so the transcript
// stays well-formed for strict providers.
func summarizeWindow(messages []chat.ChatMessage, lastN int) []chat.ChatMessage {
	if lastN <= 0 || lastN >= len(messages) {
		return messages
	}
	window := messages[len(messages)-lastN:]
	for len(window) > 0 && window[0].Role == chat.ChatRoleTool {
		window = window[1:]
	}
	return window
}

// summarizeSystemPrompt renders the embedded "summarise" agent spec, the
// same prompt family auto-compaction's "compact" spec belongs to.
func summarizeSystemPrompt(projectDir string) string {
	if def, ok := agentspec.Lookup("summarise"); ok {
		return RenderAgentPrompt(def, projectDir)
	}
	return fallbackSummarizeSystemPrompt
}
