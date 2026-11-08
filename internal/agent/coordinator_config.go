package agent

import (
	"context"
	"fmt"

	"github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
)

// handleConfigCheck answers ConfigCheckCommand (/config check) with a full
// validation report of the loaded runtime config. Unlike load-time
// validation - which fails on the first problem - this runs
// tauconfig.CheckAll and emits every issue (errors and warnings) as a
// ConfigCheckedEvent, plus a one-line notification so the result is visible
// even in headless event consumers that don't render the report.
func (c *Coordinator) handleConfigCheck(_ context.Context, cmd chat.ConfigCheckCommand) {
	now := normalizedTime(cmd.RequestedAt)

	if c.appConfig == nil {
		c.emit(chat.ChatNotificationEvent{
			Message:    "config check unavailable: no configuration is loaded in this runtime",
			Level:      chat.ChatNotificationWarn,
			OccurredAt: now,
		})
		return
	}

	issues := tauconfig.CheckAll(*c.appConfig)
	errCount, warnCount := 0, 0
	for _, issue := range issues {
		switch {
		case issue.IsError():
			errCount++
		default:
			warnCount++
		}
	}
	valid := errCount == 0

	c.emit(chat.ConfigCheckedEvent{
		Issues:     issues,
		Valid:      valid,
		OccurredAt: now,
	})

	level := chat.ChatNotificationInfo
	if errCount > 0 {
		level = chat.ChatNotificationError
	} else if warnCount > 0 {
		level = chat.ChatNotificationWarn
	}
	var message string
	switch {
	case errCount == 0 && warnCount == 0:
		message = "config check: no issues found"
	case errCount > 0:
		message = fmt.Sprintf("config check: %d error(s), %d warning(s) - see report", errCount, warnCount)
	default:
		message = fmt.Sprintf("config check: no errors, %d warning(s) - see report", warnCount)
	}
	c.emit(chat.ChatNotificationEvent{
		Message:    message,
		Level:      level,
		OccurredAt: now,
	})

	c.emitMetrics(chat.MetricEvent{
		Category: chat.MetricCategorySession,
		Name:     "config.checked",
		Value:    float64(len(issues)),
		Unit:     "issues",
		Labels: map[string]string{
			"valid":    fmt.Sprintf("%t", valid),
			"errors":   fmt.Sprintf("%d", errCount),
			"warnings": fmt.Sprintf("%d", warnCount),
		},
	})
}
