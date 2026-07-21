---
description: Source module internal/app/chat.go (907 lines).
resource: internal/app/chat.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: chat.go
type: Module
---

# Module chat.go

**Path**: `internal/app/chat.go`  
**Lines**: 907

## Snippet Preview

```
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/pkg/browser"
	aisdkchat "github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/runtime"

	"github.com/samcharles93/tau/internal/agent"
	"github.com/samcharles93/tau/internal/agent/tools"
	tauchat "github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/indexing"
	"github.com/samcharles93/tau/internal/plugin"
	"github.com/samcharles93/tau/internal/providers"
	"github.com/samcharles93/tau/internal/providers/snapshot"
	commandreg "github.com/samcharles93/tau/internal/registry"
	"github.com/samcharles93/tau/internal/sessions"
```
