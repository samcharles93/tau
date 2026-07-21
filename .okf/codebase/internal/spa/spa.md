---
description: Source module internal/spa/spa.go (47 lines).
resource: internal/spa/spa.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: spa.go
type: Module
---

# Module spa.go

**Path**: `internal/spa/spa.go`  
**Lines**: 47

## Snippet Preview

```
// Package spa serves the embedded Vue 3 web UI build output.
package spa

import (
	"bytes"
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

//go:embed dist/*
var distDir embed.FS

// Handler serves the SPA with proper MIME types and SPA-routing fallback.
func Handler() http.Handler {
	sub, err := fs.Sub(distDir, "dist")
	if err != nil {
		panic("spa: embed dist: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")

		f, err := sub.Open(path)
		if err == nil {
			_ = f.Close()
```
