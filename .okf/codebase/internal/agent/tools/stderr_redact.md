---
description: Source module internal/agent/tools/stderr_redact.go (102 lines).
resource: internal/agent/tools/stderr_redact.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: stderr_redact.go
type: Module
---

# Module stderr_redact.go

**Path**: `internal/agent/tools/stderr_redact.go`  
**Lines**: 102

## Snippet Preview

```
package tools

import (
	"bufio"
	"io"
	"log/slog"
	"regexp"
	"time"
)

// Redaction patterns per docs/specs/agents/03-wire-protocol.md (stderr
// handling: Redaction) and 02-spawning-and-lifecycle.md (Redaction
// contract). reBearerToken has no minimum length - the spec's own
// redaction test requirement uses a 10-character example token
// ("tok_abc123"), shorter than the {20,} it documents elsewhere for the
// sk- pattern, so this errs toward redacting too much rather than missing
// the documented test case.
var (
	reBearerToken = regexp.MustCompile(`Bearer\s+[a-zA-Z0-9_\-.]+`)
	reSkKey       = regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`)
	reEnvSecret   = regexp.MustCompile(`(?i)\b([A-Za-z_]*(?:KEY|TOKEN|SECRET|PASSWORD)[A-Za-z_]*)=\S+`)
)

// redactSecrets replaces known secret patterns in a single stderr line with
// [REDACTED]. Order matters only for overlap avoidance; re-matching an
// already-redacted "[REDACTED]" value is harmless (idempotent).
func redactSecrets(line string) string {
	line = reBearerToken.ReplaceAllString(line, "Bearer [REDACTED]")
	line = reSkKey.ReplaceAllString(line, "[REDACTED]")
	line = reEnvSecret.ReplaceAllString(line, "$1=[REDACTED]")
```
