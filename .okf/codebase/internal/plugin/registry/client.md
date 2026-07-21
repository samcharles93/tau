---
description: Source module internal/plugin/registry/client.go (379 lines).
resource: internal/plugin/registry/client.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: client.go
type: Module
---

# Module client.go

**Path**: `internal/plugin/registry/client.go`  
**Lines**: 379

## Snippet Preview

```
// Package registry provides an HTTP client for the Tau plugin registry API.
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultRegistry is the default plugin registry base URL.
const DefaultRegistry = "https://registry.tau-ai.dev"

// Client is an HTTP client for the Tau plugin registry REST API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient creates a registry API client. baseURL should be the registry
// root, e.g. "https://registry.tau-ai.dev". token is optional (only needed
// for write operations like publish).
func NewClient(baseURL, token string) *Client {
	return &Client{
```
