---
description: Source module internal/tui2/model_test.go (624 lines).
resource: internal/tui2/model_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: model_test.go
type: Module
---

# Module model_test.go

**Path**: `internal/tui2/model_test.go`  
**Lines**: 624

## Snippet Preview

```
package tui2

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/providers"
)

var errIntentional = errors.New("intentional")

func TestMain(m *testing.M) {
	notificationClearDelay = time.Millisecond
	notifyWarnDuration = time.Millisecond
	notifyInfoDuration = time.Millisecond
	os.Exit(m.Run())
}
```
