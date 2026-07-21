---
description: Source module internal/plugin/manager.go (531 lines).
resource: internal/plugin/manager.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: manager.go
type: Module
---

# Module manager.go

**Path**: `internal/plugin/manager.go`  
**Lines**: 531

## Snippet Preview

```
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/pkg/plugin/api"
	"google.golang.org/grpc"
)

// Config configures the plugin manager.
type Config struct {
	PluginsDir           string          // directory containing plugin binaries, e.g. <config dir>/plugins
	ToolRegistry         *tools.Registry // tool registry for registering plugin tools
	Logger               *slog.Logger
```
