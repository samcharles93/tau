---
description: Source module internal/agent/compact.go (217 lines).
resource: internal/agent/compact.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: compact.go
type: Module
---

# Module compact.go

**Path**: `internal/agent/compact.go`  
**Lines**: 217

## Snippet Preview

```
package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agentspec "github.com/samcharles93/tau/internal/agent/spec"
	"github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
)

const (
	defaultAutoCompactThresholdRatio = 0.75
	defaultAutoCompactTargetRatio    = 0.35
	autoCompactMinMessages           = 4
	estimatedCharsPerToken           = 4
)

func normalizeAutoCompactConfig(cfg tauconfig.AutoCompactConfig) tauconfig.AutoCompactConfig {
	if cfg.ThresholdRatio <= 0 || cfg.ThresholdRatio >= 1 {
		cfg.ThresholdRatio = defaultAutoCompactThresholdRatio
	}
	if cfg.TargetRatio <= 0 || cfg.TargetRatio >= cfg.ThresholdRatio {
		cfg.TargetRatio = defaultAutoCompactTargetRatio
	}
	return cfg
}
```
