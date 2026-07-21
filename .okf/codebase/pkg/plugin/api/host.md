---
description: Source module pkg/plugin/api/host.go (124 lines).
resource: pkg/plugin/api/host.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: host.go
type: Module
---

# Module host.go

**Path**: `pkg/plugin/api/host.go`  
**Lines**: 124

## Snippet Preview

```
package api

import (
	"context"
	"errors"
)

// ErrPromptCanceled is returned by Host.Confirm/Input when the user cancels.
var ErrPromptCanceled = errors.New("prompt canceled by user")

// Host is the plugin-facing handle to tau host services. A plugin that wants to
// read config, query session state, or push notifications/logs back to the host
// implements HostAware; the host injects a Host once it calls Init.
type Host interface {
	// GetConfig returns a JSON-encoded value for the plugin's config block.
	// An empty key returns the whole block. found is false when unset.
	GetConfig(ctx context.Context, key string) (value string, found bool, err error)
	// SetConfig persists a JSON-encoded value under key for this plugin.
	SetConfig(ctx context.Context, key, value string) error
	// GetSessionState returns the JSON-encoded chat session state. An empty
	// sessionID targets the active session.
	GetSessionState(ctx context.Context, sessionID string) (stateJSON string, found bool, err error)
	// GetAvailableModels lists model ids the host knows about.
	GetAvailableModels(ctx context.Context) ([]string, error)
	// Notify pushes a user-visible notification to the host UI (TUI + web).
	Notify(ctx context.Context, level, message string) error
	// Confirm asks the user for confirmation. Returns false if canceled.
	Confirm(ctx context.Context, title, description string) (bool, error)
	// Input prompts the user for free-form text input.
	Input(ctx context.Context, title, placeholder string) (string, error)
```
