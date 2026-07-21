---
description: Source module internal/tui/skills_command_test.go (65 lines).
resource: internal/tui/skills_command_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: skills_command_test.go
type: Module
---

# Module skills_command_test.go

**Path**: `internal/tui/skills_command_test.go`  
**Lines**: 65

## Snippet Preview

```
package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// TestSkillsCommandIntegration tests the /skills list command integration.
func TestSkillsCommandIntegration(t *testing.T) {
	// Test that the command is in the help
	helpText := printHelpForTest()
	if !strings.Contains(helpText, "/skills list") {
		t.Errorf("Help text should contain '/skills list', got: %s", helpText)
	}

	// Test that the command sends the right command type
	runtime := &mockRuntime{}
	chat := &inlineChat{
		runtime: runtime,
	}

	chat.handleSlashCommand("/skills list")
	time.Sleep(10 * time.Millisecond)

	if len(runtime.commands) == 0 {
		t.Fatal("Expected a command to be sent")
```
