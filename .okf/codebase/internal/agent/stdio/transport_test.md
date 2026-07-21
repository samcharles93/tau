---
description: Source module internal/agent/stdio/transport_test.go (244 lines).
resource: internal/agent/stdio/transport_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: transport_test.go
type: Module
---

# Module transport_test.go

**Path**: `internal/agent/stdio/transport_test.go`  
**Lines**: 244

## Snippet Preview

```
package stdio

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	r := NewReader(&buf)

	msg := map[string]any{"hello": "world", "num": 42}
	if err := w.WriteMessage(msg); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	data, err := r.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
```
