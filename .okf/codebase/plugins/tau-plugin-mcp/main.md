---
description: Source module plugins/tau-plugin-mcp/main.go (508 lines).
resource: plugins/tau-plugin-mcp/main.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: main.go
type: Module
---

# Module main.go

**Path**: `plugins/tau-plugin-mcp/main.go`  
**Lines**: 508

## Snippet Preview

```
// Tau MCP Client Plugin - connects to MCP servers and registers their tools
// with tau's agent coordinator via the go-plugin extension architecture.
//
// Build: cd plugins/mcp && go build -o tau-plugin-mcp .
// Install: cp tau-plugin-mcp ~/.config/tau/plugins/
//
// Config (~/.config/tau/config.yaml):
//
//	plugins:
//	  mcp-plugin:
//	    servers:
//	      - name: filesystem            # stdio transport
//	        command: npx
//	        args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
//	      - name: spawn                 # Streamable HTTP transport
//	        url: http://localhost:9343/mcp
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
```
