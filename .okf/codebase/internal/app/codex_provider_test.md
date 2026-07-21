---
description: Source module internal/app/codex_provider_test.go (38 lines).
resource: internal/app/codex_provider_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: codex_provider_test.go
type: Module
---

# Module codex_provider_test.go

**Path**: `internal/app/codex_provider_test.go`  
**Lines**: 38

## Snippet Preview

```
package app

import "testing"

func TestCodexSSEChunkReadsNestedFunctionCallItem(t *testing.T) {
	chunk, ok := codexSSEChunk([]byte(`{
		"type":"response.output_item.added",
		"output_index":2,
		"item":{"id":"item_1","call_id":"call_1","name":"read","arguments":"{\"path\":\"README.md\"}"}
	}`))
	if !ok || len(chunk.ToolCallDeltas) != 1 {
		t.Fatalf("chunk = %#v, ok = %v", chunk, ok)
	}
	delta := chunk.ToolCallDeltas[0]
	if delta.ID != "call_1" || delta.Name != "read" || delta.ArgsDelta == "" || delta.Index != 2 {
		t.Fatalf("delta = %#v", delta)
	}
}

func TestCodexSSEChunkBackfillsFunctionNameWhenArgumentsDone(t *testing.T) {
	chunk, ok := codexSSEChunk([]byte(`{
		"type":"response.function_call_arguments.done",
		"output_index":0,
		"item_id":"fc_123",
		"name":"spawn_agent",
		"arguments":"{\"task_name\":\"investigate\"}"
	}`))
	if !ok || len(chunk.ToolCallDeltas) != 1 {
		t.Fatalf("chunk = %#v, ok = %v", chunk, ok)
	}
```
