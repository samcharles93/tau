---
description: Source module internal/providerui/login_test.go (111 lines).
resource: internal/providerui/login_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: login_test.go
type: Module
---

# Module login_test.go

**Path**: `internal/providerui/login_test.go`  
**Lines**: 111

## Snippet Preview

```
package providerui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/pkg/browser"
	"github.com/samcharles93/tau/internal/providers"
)

func TestDeviceCodeMessageBrowserOpened(t *testing.T) {
	msg := DeviceCodeMessage("GitHub Copilot", providers.DeviceCode{
		VerificationURI: "https://github.com/login/device",
		UserCode:        "ABCD-1234",
	}, true, true)
	for _, want := range []string{
		"GitHub Copilot authorization",
		"1. Browser opened",
		"https://github.com/login/device",
		"2. Paste code (copied)",
		"ABCD-1234",
		"Waiting for authorization",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected %q to contain %q", msg, want)
		}
```
