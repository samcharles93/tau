---
description: Source module internal/skills/tracker_test.go (24 lines).
resource: internal/skills/tracker_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: tracker_test.go
type: Module
---

# Module tracker_test.go

**Path**: `internal/skills/tracker_test.go`  
**Lines**: 24

## Snippet Preview

```
package skills

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrackerDeduplicatesActivations(t *testing.T) {
	t.Parallel()

	tracker := NewTracker()
	skill := &Skill{Name: "pdf-processing"}

	tracker.Activate(skill)
	tracker.Activate(skill)

	require.True(t, tracker.IsActivated("pdf-processing"))
	require.Equal(t, []string{"pdf-processing"}, tracker.Activated())

	tracker.Reset()
	require.False(t, tracker.IsActivated("pdf-processing"))
	require.Empty(t, tracker.Activated())
}
```
