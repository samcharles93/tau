---
description: Source module internal/config/config.go (1582 lines).
resource: internal/config/config.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: config.go
type: Module
---

# Module config.go

**Path**: `internal/config/config.go`  
**Lines**: 1582

## Snippet Preview

```
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const localConfigName = ".tau.yaml"

// Config holds Tau user preferences loaded from global and project config files.
type Config struct {
	DefaultProvider string           `yaml:"default_provider"`
	DefaultModel    string           `yaml:"default_model"`
	Providers       []ProviderConfig `yaml:"providers"`
	UI              UIConfig         `yaml:"ui"`
	Debug           bool             `yaml:"debug"`
	// Registry configures the plugin registry connection.
	Registry RegistryConfig `yaml:"registry"`
	// Plugins holds per-plugin config blocks (`plugins.<name>:`), passed through
	// to plugins via the HostService.GetConfig reverse RPC.
	Plugins map[string]map[string]any `yaml:"plugins"`
	// DisabledSkills lists skill names to exclude from the active catalog.
```
