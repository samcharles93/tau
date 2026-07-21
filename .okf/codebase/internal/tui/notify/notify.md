---
description: Source module internal/tui/notify/notify.go (107 lines).
resource: internal/tui/notify/notify.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: notify.go
type: Module
---

# Module notify.go

**Path**: `internal/tui/notify/notify.go`  
**Lines**: 107

## Snippet Preview

```
// Package notify provides a queue-based notification system for the TUI.
// Notifications are short-lived messages shown in the status bar that
// auto-dismiss after a configurable duration. Any part of the application
// can publish notifications via the pubsub bus; the TUI subscribes and
// enqueues them for display.
package notify

import "time"

// Level controls how a notification is rendered.
type Level int

const (
	LevelInfo Level = iota
	LevelWarn
	LevelError
)

// Notification is a transient message shown in the status bar.
type Notification struct {
	Message  string
	Level    Level
	Duration time.Duration
}

// entry is an enqueued notification with its computed expiry.
type entry struct {
	notification Notification
	expiresAt    time.Time
}
```
