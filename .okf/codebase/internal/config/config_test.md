---
description: Source module internal/config/config_test.go (1215 lines).
resource: internal/config/config_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: config_test.go
type: Module
---

# Module config_test.go

**Path**: `internal/config/config_test.go`  
**Lines**: 1215

## Snippet Preview

```
package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestConfigUnmarshalYAMLPopulatesMetrics guards against a real bug:
// Config.UnmarshalYAML's internal rawConfig struct omitted the Metrics
// field entirely, so metrics.dir/session/tui in a user's config.yaml were
// silently discarded on every load - cfg.Metrics always ended up as the Go
// zero value in memory regardless of what was actually written to disk.
func TestConfigUnmarshalYAMLPopulatesMetrics(t *testing.T) {
	var cfg Config
	err := yaml.Unmarshal([]byte(`
providers:
  - name: acme
    base_url: https://acme.example
    auth:
      type: none
metrics:
  dir: /custom/metrics/path
  session: true
  tui: true
```
