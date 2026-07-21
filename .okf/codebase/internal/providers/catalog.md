---
description: Source module internal/providers/catalog.go (142 lines).
resource: internal/providers/catalog.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: catalog.go
type: Module
---

# Module catalog.go

**Path**: `internal/providers/catalog.go`  
**Lines**: 142

## Snippet Preview

```
// Package providers manages tau's writable provider/auth layer: a built-in
// catalog of well-known OpenAI-compatible providers, a tau-owned state file
// recording which providers the user has enabled plus any OAuth credentials,
// and a resolver that merges hand-written config, that managed state, and the
// live environment into the effective set of providers tau should use.
//
// The user's hand-written config.yaml / .tau.yaml is treated as authoritative
// and is never written by this package - everything tau manages lives in a
// separate auth.yaml so comments and literal secrets are never clobbered.
package providers

import "strings"

// AuthKind classifies how a catalog provider authenticates.
type AuthKind string

const (
	// AuthAPIKey providers read a bearer token from an environment variable.
	AuthAPIKey AuthKind = "api_key"
	// AuthOAuth providers obtain a token through an interactive login flow and
	// persist the resulting credentials in the managed state file.
	AuthOAuth AuthKind = "oauth"
	// AuthNone providers need no credentials (e.g. a local Ollama server).
	AuthNone AuthKind = "none"
)

// CatalogEntry describes a well-known provider tau can enable without the user
// having to hand-write a config block. For API-key providers, EnvVars lists the
// conventional environment variables to probe (first one set wins).
type CatalogEntry struct {
```
