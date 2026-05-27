package agent

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
)

type interactivePromptResponse struct {
	confirmed bool
	canceled  bool
}

type coordinatorUIBridge struct {
	coordinator *Coordinator
}

func (b *coordinatorUIBridge) Confirm(ctx context.Context, title, description string) (bool, error) {
	if b == nil || b.coordinator == nil || !b.coordinator.interactiveUI {
		return false, tools.ErrInteractiveUnsupported
	}
	c := b.coordinator
	requestID := c.nextPromptID()
	ch := make(chan interactivePromptResponse, 1)

	c.promptMu.Lock()
	c.prompts[requestID] = ch
	c.promptMu.Unlock()
	defer c.forgetPrompt(requestID)

	c.emit(chat.InteractivePromptRequestedEvent{
		RequestID:   requestID,
		Kind:        chat.InteractivePromptConfirm,
		Title:       title,
		Message:     description,
		RequestedAt: time.Now().UTC(),
	})

	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case response := <-ch:
		if response.canceled {
			return false, tools.ErrInteractiveCanceled
		}
		return response.confirmed, nil
	}
}

func (b *coordinatorUIBridge) Select(context.Context, string, []string) (string, error) {
	return "", tools.ErrInteractiveUnsupported
}

func (b *coordinatorUIBridge) Input(context.Context, string, string) (string, error) {
	return "", tools.ErrInteractiveUnsupported
}

func (b *coordinatorUIBridge) Notify(title, level string) {
	if b == nil || b.coordinator == nil {
		return
	}
	notificationLevel := chat.ChatNotificationInfo
	switch level {
	case "warn", "warning":
		notificationLevel = chat.ChatNotificationWarn
	case "error":
		notificationLevel = chat.ChatNotificationError
	}
	b.coordinator.emit(chat.ChatNotificationEvent{
		Message:    title,
		Level:      notificationLevel,
		OccurredAt: time.Now().UTC(),
	})
}

func (c *Coordinator) nextPromptID() string {
	seq := atomic.AddUint64(&c.promptSeq, 1)
	return fmt.Sprintf("prompt_%d", seq)
}

func (c *Coordinator) forgetPrompt(requestID string) {
	c.promptMu.Lock()
	delete(c.prompts, requestID)
	c.promptMu.Unlock()
}

func extensionReloadMessage(result chat.ExtensionReloadResult) string {
	message := fmt.Sprintf("Reloaded extensions: %d loaded", result.ExtensionCount)
	if len(result.Diagnostics) > 0 {
		message += fmt.Sprintf(", %d diagnostics", len(result.Diagnostics))
	}
	return message
}

func extensionDiagnosticMessage(diagnostic chat.ExtensionDiagnostic) string {
	prefix := "Extension diagnostic"
	if diagnostic.ExtensionName != "" {
		prefix += " (" + diagnostic.ExtensionName + ")"
	}
	return prefix + ": " + diagnostic.Message
}
