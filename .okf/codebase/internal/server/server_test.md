---
description: Source module internal/server/server_test.go (68 lines).
resource: internal/server/server_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: server_test.go
type: Module
---

# Module server_test.go

**Path**: `internal/server/server_test.go`  
**Lines**: 68

## Snippet Preview

```
package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBridge struct {
	clients    int
	upgradeErr error
}

func (b *fakeBridge) UpgradeHTTP(w http.ResponseWriter, r *http.Request) error {
	return b.upgradeErr
}

func (b *fakeBridge) ClientCount() int { return b.clients }
func (b *fakeBridge) Close() error     { return nil }

func TestServerServesHealthAndSPA(t *testing.T) {
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<h1>Tau</h1>")
```
