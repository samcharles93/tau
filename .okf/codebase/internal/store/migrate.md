---
description: Source module internal/store/migrate.go (174 lines).
resource: internal/store/migrate.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: migrate.go
type: Module
---

# Module migrate.go

**Path**: `internal/store/migrate.go`  
**Lines**: 174

## Snippet Preview

```
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// currentSchemaVersion is the latest migration number. Bump this when adding
// new migrations and append the SQL to the migrations map below.
const currentSchemaVersion = 7

// Migrate runs all pending schema migrations in order. It is idempotent -
// already-applied migrations are skipped.
func Migrate(ctx context.Context, db *sql.DB) error {
	if err := ensureSchemaVersionTable(db, ctx); err != nil {
		return err
	}

	current, err := appliedVersion(db, ctx)
	if err != nil {
		return err
	}

	migrations := map[int]string{
		1: migrationV1,
		2: migrationV2,
		3: migrationV3,
		4: migrationV4,
```
