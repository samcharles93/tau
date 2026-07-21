---
description: Source module internal/agent/tools/bridge_test.go (11 lines).
resource: internal/agent/tools/bridge_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: bridge_test.go
type: Module
---

# Module bridge_test.go

**Path**: `internal/agent/tools/bridge_test.go`  
**Lines**: 11

## Snippet Preview

```
package tools

import (
	"testing"
)

func TestNonInteractiveBridgeLog(t *testing.T) {
	var bridge UIBridge = NonInteractiveBridge{}
	// This will fail to compile until Log is added to UIBridge and NonInteractiveBridge
	bridge.Log("test chunk")
}
```
