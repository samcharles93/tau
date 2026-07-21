---
description: Source module plugins/tau-plugin-hello/main.go (322 lines).
resource: plugins/tau-plugin-hello/main.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: main.go
type: Module
---

# Module main.go

**Path**: `plugins/tau-plugin-hello/main.go`  
**Lines**: 322

## Snippet Preview

```
// Tau Hello Plugin - minimal example of the go-plugin extension API.
//
// Build:
//
//	cd plugins/hello
//	go build -o tau-plugin-hello .
//
// Install:
//
//	mkdir -p ~/.config/tau/plugins
//	cp tau-plugin-hello ~/.config/tau/plugins/
//
// Run tau and type "/hello world" or let the agent call the hello_greet tool.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"

	pluginapi "github.com/samcharles93/tau/pkg/plugin/api"
```
