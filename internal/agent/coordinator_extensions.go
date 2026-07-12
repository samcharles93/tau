package agent

import (
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/chat"
)

func (c *Coordinator) handleReloadExtensions(cmd chat.ReloadExtensionsCommand) {
	now := normalizedTime(cmd.RequestedAt)
	if c.extensionReloader == nil {
		c.emit(chat.ChatNotificationEvent{
			Message:    "Extension reload is not available",
			Level:      chat.ChatNotificationWarn,
			OccurredAt: now,
		})
		return
	}

	idle := c.isIdle()
	if !idle {
		c.emit(chat.ChatNotificationEvent{
			Message:    "Extension reload is only available while idle",
			Level:      chat.ChatNotificationWarn,
			OccurredAt: now,
		})
		return
	}

	result, err := c.extensionReloader.ReloadExtensions(c.ctx, true)
	if err != nil {
		c.emit(chat.ChatNotificationEvent{
			Message:    "Extension reload failed: " + err.Error(),
			Level:      chat.ChatNotificationError,
			OccurredAt: now,
		})
		c.emit(chat.ChatRuntimeErrorEvent{
			Message:    "Extension reload failed: " + err.Error(),
			Fatal:      false,
			OccurredAt: now,
		})
		return
	}

	c.emit(chat.ExtensionsReloadedEvent{Result: result, OccurredAt: now})
	c.emit(chat.ExtensionCommandsChangedEvent{
		Commands:   result.Commands,
		OccurredAt: now,
	})
	c.emit(chat.ChatNotificationEvent{
		Message:    extensionReloadMessage(result),
		Level:      chat.ChatNotificationInfo,
		OccurredAt: now,
	})
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Message == "" {
			continue
		}
		level := chat.ChatNotificationWarn
		if diagnostic.Severity == "error" {
			level = chat.ChatNotificationError
		}
		c.emit(chat.ChatNotificationEvent{
			Message:    extensionDiagnosticMessage(diagnostic),
			Level:      level,
			OccurredAt: now,
		})
	}
}

func (c *Coordinator) handleRunExtensionCommand(cmd chat.RunExtensionCommandCommand) {
	now := normalizedTime(cmd.RequestedAt)
	if c.extensionReloader == nil {
		c.emit(chat.ChatNotificationEvent{
			Message:    "Extension commands are not available",
			Level:      chat.ChatNotificationWarn,
			OccurredAt: now,
		})
		return
	}
	if !c.isIdle() {
		c.emit(chat.ChatNotificationEvent{
			Message:    "Extension commands are only available while idle",
			Level:      chat.ChatNotificationWarn,
			OccurredAt: now,
		})
		return
	}
	name := strings.TrimSpace(cmd.Name)
	args := strings.TrimSpace(cmd.Args)
	c.turnWG.Go(func() {
		output, view, err := c.extensionReloader.RunExtensionCommand(c.ctx, name, args, c.uiBridge)
		at := time.Now().UTC()
		status := "success"
		if err != nil {
			status = "error"
			// A single user-facing error notification.
			c.emit(chat.ChatNotificationEvent{
				Message:    "Extension command failed: " + err.Error(),
				Level:      chat.ChatNotificationError,
				OccurredAt: at,
			})
		}
		c.emitMetrics(chat.MetricEvent{
			Category: chat.MetricCategoryExtension,
			Name:     "extension.command",
			Value:    1,
			Unit:     "count",
			Labels:   map[string]string{"command": name, "status": status},
		})
		if err != nil {
			return
		}
		c.emit(chat.ExtensionCommandResultEvent{Name: name, Output: output, View: view, OccurredAt: at})
		if strings.TrimSpace(output) != "" {
			c.emit(chat.ChatNotificationEvent{
				Message:    "Extension command completed: /" + name,
				Level:      chat.ChatNotificationInfo,
				OccurredAt: at,
			})
		}
	})
}
