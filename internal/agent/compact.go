package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agentspec "github.com/samcharles93/tau/internal/agent/spec"
	"github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
)

const (
	defaultAutoCompactThresholdRatio = 0.75
	defaultAutoCompactTargetRatio    = 0.35
	autoCompactMinMessages           = 4
	estimatedCharsPerToken           = 4
)

func normalizeAutoCompactConfig(cfg tauconfig.AutoCompactConfig) tauconfig.AutoCompactConfig {
	if cfg.ThresholdRatio <= 0 || cfg.ThresholdRatio >= 1 {
		cfg.ThresholdRatio = defaultAutoCompactThresholdRatio
	}
	if cfg.TargetRatio <= 0 || cfg.TargetRatio >= cfg.ThresholdRatio {
		cfg.TargetRatio = defaultAutoCompactTargetRatio
	}
	return cfg
}

func (c *Coordinator) maybeAutoCompact(ctx context.Context, state chat.ChatSessionState, bearerToken string) (chat.ChatSessionState, bool, error) {
	cfg := normalizeAutoCompactConfig(c.autoCompact)
	if !cfg.Enabled {
		return state, false, nil
	}
	if len(state.Messages) < autoCompactMinMessages {
		return state, false, nil
	}
	contextWindow := state.Model.Config.ContextWindow
	if contextWindow <= 0 {
		return state, false, nil
	}
	estimatedTokens := estimateRequestTokens(state)
	thresholdTokens := int(float64(contextWindow) * cfg.ThresholdRatio)
	if thresholdTokens <= 0 || estimatedTokens < thresholdTokens {
		return state, false, nil
	}

	compacted, err := c.compactState(ctx, state, bearerToken, cfg)
	if err != nil {
		return state, false, err
	}
	if len(compacted.Messages) >= len(state.Messages) {
		return state, false, nil
	}

	now := time.Now().UTC()
	c.mu.Lock()
	session, ok := c.sessions[state.SessionID]
	if !ok || session.state == nil {
		c.mu.Unlock()
		return state, false, errors.New("session not found")
	}
	if session.state.ActiveRequestID != state.ActiveRequestID {
		c.mu.Unlock()
		return state, false, errors.New("active request changed during compaction")
	}
	session.state.Messages = compacted.Messages
	session.state.UpdatedAt = now
	snapshot := chat.CloneChatSessionState(session.state)
	c.mu.Unlock()

	c.emit(chat.ChatNotificationEvent{
		Message: fmt.Sprintf(
			"auto-compacted history: %d messages -> %d messages",
			len(state.Messages),
			len(snapshot.Messages),
		),
		Level:      chat.ChatNotificationInfo,
		OccurredAt: now,
	})
	c.emit(chat.ChatSessionSnapshotEvent{State: snapshot})
	c.emitMetrics(chat.MetricEvent{
		Category:  chat.MetricCategorySession,
		Name:      "session.auto_compacted",
		Value:     float64(len(state.Messages) - len(snapshot.Messages)),
		Unit:      "messages",
		SessionID: state.SessionID,
		Labels: map[string]string{
			"before_messages":   fmt.Sprintf("%d", len(state.Messages)),
			"after_messages":    fmt.Sprintf("%d", len(snapshot.Messages)),
			"estimated_tokens":  fmt.Sprintf("%d", estimatedTokens),
			"context_window":    fmt.Sprintf("%d", contextWindow),
			"threshold_percent": fmt.Sprintf("%.0f", cfg.ThresholdRatio*100),
		},
	})
	return snapshot, true, nil
}

func (c *Coordinator) compactState(ctx context.Context, state chat.ChatSessionState, bearerToken string, cfg tauconfig.AutoCompactConfig) (chat.ChatSessionState, error) {
	history, current := splitCompactionHistory(state.Messages)
	if len(history) == 0 {
		return state, nil
	}
	def, ok := agentspec.Lookup("compact")
	if !ok {
		return state, errors.New("compact agent not found")
	}

	compactState := chat.CloneChatSessionState(&state)
	compactState.SystemPrompt = RenderAgentPrompt(def, c.projectDir)
	compactState.Messages = append([]chat.ChatMessage(nil), history...)
	compactState.Messages = append(compactState.Messages, chat.ChatMessage{
		ID:        chat.NewMessageID(),
		Role:      chat.ChatRoleUser,
		Content:   compactionInstruction(state, cfg),
		CreatedAt: time.Now().UTC(),
	})
	compactState.Tools = nil
	compactState.PendingAssistant = ""
	compactState.ActiveRequestID = ""
	compactState.Status = chat.ChatSessionIdle
	if cfg.Model != "" {
		compactState.Model.ID = cfg.Model
		if c.modelLookup != nil {
			if full := c.modelLookup(cfg.Model); full != nil {
				compactState.Model = *full
			}
		}
	}
	if state.Model.Config.ContextWindow > 0 {
		compactState.Parameters.MaxTokens = int(float64(state.Model.Config.ContextWindow) * cfg.TargetRatio)
	}

	var summary strings.Builder
	result, err := c.streamer.StreamChatCompletionFull(ctx, compactState, bearerToken, nil, chat.StreamCallbacks{
		OnDelta: func(delta string) error {
			summary.WriteString(delta)
			return nil
		},
	})
	if err != nil {
		return state, fmt.Errorf("compact history: %w", err)
	}
	if summary.Len() == 0 && strings.TrimSpace(result.FinishReason) == "" {
		return state, errors.New("compact history: empty response")
	}
	summaryText := strings.TrimSpace(summary.String())
	if summaryText == "" {
		return state, errors.New("compact history: empty summary")
	}

	replacement := []chat.ChatMessage{{
		ID:        chat.NewMessageID(),
		Role:      chat.ChatRoleUser,
		Content:   "Conversation summary before auto-compaction:\n\n" + summaryText,
		CreatedAt: time.Now().UTC(),
	}}
	if current != nil {
		replacement = append(replacement, *current)
	}
	state.Messages = replacement
	state.UpdatedAt = time.Now().UTC()
	return state, nil
}

func splitCompactionHistory(messages []chat.ChatMessage) ([]chat.ChatMessage, *chat.ChatMessage) {
	if len(messages) == 0 {
		return nil, nil
	}
	last := messages[len(messages)-1]
	if last.Role == chat.ChatRoleUser {
		current := last
		return messages[:len(messages)-1], &current
	}
	return messages, nil
}

func compactionInstruction(state chat.ChatSessionState, cfg tauconfig.AutoCompactConfig) string {
	targetTokens := 0
	if state.Model.Config.ContextWindow > 0 {
		targetTokens = int(float64(state.Model.Config.ContextWindow) * cfg.TargetRatio)
	}
	if targetTokens <= 0 {
		return "Compact the conversation history above into a dense continuation summary."
	}
	return fmt.Sprintf(
		"Compact the conversation history above into a dense continuation summary under roughly %d tokens.",
		targetTokens,
	)
}

func estimateRequestTokens(state chat.ChatSessionState) int {
	chars := len(state.SystemPrompt)
	for _, msg := range state.Messages {
		chars += len(msg.Content)
		chars += len(msg.ReasoningContent)
		chars += len(msg.ToolCallID)
		for _, call := range msg.ToolCalls {
			chars += len(call.ID)
			chars += len(call.Function.Name)
			chars += len(call.Function.Arguments)
		}
	}
	for _, tool := range state.Tools {
		chars += len(tool.Function.Name)
		chars += len(tool.Function.Description)
		chars += len(tool.Function.Parameters)
	}
	tokens := (chars + estimatedCharsPerToken - 1) / estimatedCharsPerToken
	tokens += len(state.Messages) * 4
	if tokens < 0 {
		return 0
	}
	return tokens
}
