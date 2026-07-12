package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/pkg/plugin/api"
)

type interactivePromptResponse struct {
	confirmed bool
	canceled  bool
	response  string
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
	case <-c.ctx.Done():
		return false, c.ctx.Err()
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

func (b *coordinatorUIBridge) Input(ctx context.Context, title, placeholder string) (string, error) {
	if b == nil || b.coordinator == nil || !b.coordinator.interactiveUI {
		return "", tools.ErrInteractiveUnsupported
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
		Kind:        chat.InteractivePromptQuestion,
		Title:       title,
		Message:     placeholder,
		RequestedAt: time.Now().UTC(),
	})

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-c.ctx.Done():
		return "", c.ctx.Err()
	case response := <-ch:
		if response.canceled {
			return "", tools.ErrInteractiveCanceled
		}
		return response.response, nil
	}
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

// RenderView implements plugin.ViewRenderer, translating a plugin's pushed
// panel into an ExtensionViewRenderedEvent on the chat event bus. ViewID is
// host-qualified (pluginName + ":" + the plugin-local View.ID) so the TUI's
// panel index can't collide or be spoofed across plugins.
func (b *coordinatorUIBridge) RenderView(ctx context.Context, pluginName string, view *api.View) error {
	if b == nil || b.coordinator == nil {
		return nil
	}
	chatView := api.ProtoViewToChat(view)
	if chatView == nil {
		return fmt.Errorf("plugin ui bridge: render view: nil view")
	}
	b.coordinator.emit(chat.ExtensionViewRenderedEvent{
		PluginName: pluginName,
		ViewID:     pluginName + ":" + chatView.ID,
		View:       *chatView,
		OccurredAt: time.Now().UTC(),
	})
	return nil
}

// CloseView implements plugin.ViewRenderer.
func (b *coordinatorUIBridge) CloseView(ctx context.Context, pluginName, viewID string) error {
	if b == nil || b.coordinator == nil {
		return nil
	}
	b.coordinator.emit(chat.ExtensionViewClosedEvent{
		PluginName: pluginName,
		ViewID:     pluginName + ":" + viewID,
		OccurredAt: time.Now().UTC(),
	})
	return nil
}

func (b *coordinatorUIBridge) Log(chunk string) {
	if b == nil || b.coordinator == nil {
		return
	}
	b.coordinator.emit(chat.ChatToolOutputEvent{
		Chunk:      chunk,
		ReceivedAt: time.Now().UTC(),
	})
}

func (c *Coordinator) nextPromptID() string {
	seq := c.promptSeq.Add(1)
	return fmt.Sprintf("prompt_%d", seq)
}

func (c *Coordinator) forgetPrompt(requestID string) {
	c.promptMu.Lock()
	delete(c.prompts, requestID)
	c.promptMu.Unlock()
}

func (c *Coordinator) cancelInteractivePrompts() {
	c.promptMu.Lock()
	prompts := c.prompts
	c.prompts = make(map[string]chan interactivePromptResponse)
	c.promptMu.Unlock()
	for _, ch := range prompts {
		ch <- interactivePromptResponse{canceled: true}
	}
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

type loggingUIBridge struct {
	tools.UIBridge
	sessionID string
	requestID string
	callID    string
	c         *Coordinator
}

func (b *loggingUIBridge) Log(chunk string) {
	b.c.emit(chat.ChatToolOutputEvent{
		SessionID:  b.sessionID,
		RequestID:  b.requestID,
		CallID:     b.callID,
		Chunk:      chunk,
		ReceivedAt: time.Now().UTC(),
	})
}
