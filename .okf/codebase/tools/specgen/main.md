---
description: Source module tools/specgen/main.go (213 lines).
resource: tools/specgen/main.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: main.go
type: Module
---

# Module main.go

**Path**: `tools/specgen/main.go`  
**Lines**: 213

## Snippet Preview

```
// Command specgen (re)generates docs/asyncapi/tau.yaml from the wire
// registries in internal/bridge (EventTypes, CommandTypes) plus the
// InitMessage struct. It is the only thing that should ever write that
// file's `components.schemas` section - hand edits get overwritten on the
// next run.
//
// Usage (from the repo root):
//
//	go -C tools run ./specgen
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/invopop/jsonschema"
	"gopkg.in/yaml.v3"

	"github.com/samcharles93/tau/internal/bridge"
)

func main() {
	repoRoot := flag.String("repo-root", "..", "path to the tau repo root")
	header := flag.String("header", "docs/asyncapi/header.yaml", "static AsyncAPI preamble, relative to repo-root")
```
