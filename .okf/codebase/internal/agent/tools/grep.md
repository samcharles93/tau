---
description: Source module internal/agent/tools/grep.go (515 lines).
resource: internal/agent/tools/grep.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: grep.go
type: Module
---

# Module grep.go

**Path**: `internal/agent/tools/grep.go`  
**Lines**: 515

## Snippet Preview

```
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/samcharles93/tau/internal/agent/tools/rg"
)

const (
	// grepMaxLineChars caps each output line so a single long line (e.g. in
	// minified or generated files) cannot blow out the context window.
	grepMaxLineChars = 500

	// grepDefaultLimit is the default maximum number of matches returned.
	grepDefaultLimit = 100
	grepMaxBytes     = 24 * 1024
)

// grepMatchLineRe recognises a match line in ripgrep-style output
// (path:line:content). Context lines use '-' separators instead.
var grepMatchLineRe = regexp.MustCompile(`^.+?:\d+:`)
```
