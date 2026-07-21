---
description: Source module internal/cli/plugins.go (508 lines).
resource: internal/cli/plugins.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: plugins.go
type: Module
---

# Module plugins.go

**Path**: `internal/cli/plugins.go`  
**Lines**: 508

## Snippet Preview

```
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"

	"github.com/samcharles93/tau/internal/config"
	"github.com/samcharles93/tau/internal/plugin"
	"github.com/samcharles93/tau/internal/plugin/registry"
	urfavecli "github.com/urfave/cli/v3"
)

func pluginsCmd() *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "plugin",
		Usage: "Manage plugins from the Tau registry",
		Commands: []*urfavecli.Command{
			pluginSearchCmd(),
			pluginInfoCmd(),
			pluginInstallCmd(),
			pluginListCmd(),
			pluginUninstallCmd(),
			pluginUpdateCmd(),
			pluginPublishCmd(),
```
