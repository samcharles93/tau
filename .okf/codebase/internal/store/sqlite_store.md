---
description: Source module internal/store/sqlite_store.go (863 lines).
resource: internal/store/sqlite_store.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: sqlite_store.go
type: Module
---

# Module sqlite_store.go

**Path**: `internal/store/sqlite_store.go`  
**Lines**: 863

## Snippet Preview

```
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/config"

	sqlite "modernc.org/sqlite"
)

// SQLiteStore implements SessionStore backed by a local SQLite database.
// Messages are stored in the messages table. JSONL export files are written
// as a convenience artifact but the app never reads them.
type SQLiteStore struct {
	db          *sql.DB
	sessionsDir string // directory for JSONL export files
}

// NewSQLiteStore opens or creates the SQLite database at dbPath and runs
// pending migrations. sessionsDir is where JSONL export files are written.
func NewSQLiteStore(ctx context.Context, dbPath, sessionsDir string) (*SQLiteStore, error) {
	// modernc.org/sqlite ignores unrecognized DSN query keys like
	// _journal_mode/_busy_timeout/_foreign_keys, so none of these actually
```
