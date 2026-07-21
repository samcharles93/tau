---
description: Source module internal/providerui/login.go (96 lines).
resource: internal/providerui/login.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: login.go
type: Module
---

# Module login.go

**Path**: `internal/providerui/login.go`  
**Lines**: 96

## Snippet Preview

```
package providerui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/pkg/browser"
	"github.com/samcharles93/tau/internal/providers"
)

// StartMessage is the first scrollback line for a provider OAuth login.
func StartMessage(displayName string) string {
	return fmt.Sprintf("%s login\n\n  Starting device authorization...", displayName)
}

// DeviceCodeMessage renders the browser/device-code step. opened reports
// whether Tau successfully asked the OS to open the verification URL; copied
// reports whether Tau attempted to copy the user code to the clipboard.
func DeviceCodeMessage(displayName string, code providers.DeviceCode, opened bool, copied bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s authorization\n\n", displayName)
	if opened {
		b.WriteString("  1. Browser opened\n")
	} else {
		b.WriteString("  1. Open this URL\n")
	}
	if strings.TrimSpace(code.VerificationURI) != "" {
```
