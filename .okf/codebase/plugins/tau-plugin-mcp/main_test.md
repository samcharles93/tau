---
description: Source module plugins/tau-plugin-mcp/main_test.go (89 lines).
resource: plugins/tau-plugin-mcp/main_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: main_test.go
type: Module
---

# Module main_test.go

**Path**: `plugins/tau-plugin-mcp/main_test.go`  
**Lines**: 89

## Snippet Preview

```
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveMCPServer exercises the plugin against a real MCP server end to end:
// it connects, lists the server's tools (proving the Tools() bridge), and can
// optionally invoke one (proving ExecuteTool). It is gated behind MCP_LIVE_URL
// so a normal `go test` run skips it.
//
// List tools from a Streamable HTTP server:
//
//	MCP_LIVE_URL=http://localhost:9343/mcp go test -run TestLiveMCPServer -v .
//
// Also call a tool (the plugin's "<server>.<tool>" name, args are JSON):
//
//	MCP_LIVE_URL=http://localhost:9343/mcp \
//	MCP_LIVE_CALL=spawn.list_agents MCP_LIVE_ARGS='{}' \
//	go test -run TestLiveMCPServer -v .
func TestLiveMCPServer(t *testing.T) {
	url := os.Getenv("MCP_LIVE_URL")
	if url == "" {
		t.Skip("set MCP_LIVE_URL to run the live MCP integration test")
```
