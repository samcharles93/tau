---
description: Source module internal/store/session.go (143 lines).
resource: internal/store/session.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: session.go
type: Module
---

# Module session.go

**Path**: `internal/store/session.go`  
**Lines**: 143

## Snippet Preview

```
package store

import (
	"context"
	"errors"
	"time"

	"github.com/samcharles93/tau/internal/chat"
)

// ErrSessionActive is returned by ResumeSession when the session's current
// owning instance has not ended - another process is actively running it.
var ErrSessionActive = errors.New("store: session is still active")

// ErrSessionNotFound is returned by ResumeSession when the target session
// does not exist.
var ErrSessionNotFound = errors.New("store: session not found")

// SessionSummary is a metadata-only view of a saved session. It is the wire
// type used for session listing - no full message content is included.
type SessionSummary struct {
	ID              string    `json:"id"`
	ModelID         string    `json:"model_id"`
	Provider        string    `json:"provider"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	Status          string    `json:"status"`
	MessageCount    int       `json:"message_count"`
	InputTokens     int       `json:"input_tokens"`
	OutputTokens    int       `json:"output_tokens"`
```
