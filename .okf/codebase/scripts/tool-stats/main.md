---
description: Source module scripts/tool-stats/main.go (1172 lines).
resource: scripts/tool-stats/main.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: main.go
type: Module
---

# Module main.go

**Path**: `scripts/tool-stats/main.go`  
**Lines**: 1172

## Snippet Preview

```
// Command tool-stats analyses tau session files and reports how the agent's
// tools are actually used: call counts, result sizes, estimated token cost,
// and error rates per tool, plus a breakdown of what shell commands run.
//
// Usage:
//
//	go run ./scripts/tool-stats [--sessions-dir <dir>] [--metrics-dir <dir>] [--output <file.html>] [--json]
//
// It reads every *.jsonl and *.jsonl.tmp session file, prints a summary table
// to stdout, and writes a self-contained HTML report.
//
// New sessions persist structured tool-result status. Legacy sessions fall
// back to conservative tool-specific result-text classification. When a
// metrics.jsonl file exists, duration events add an independent live view.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
```
