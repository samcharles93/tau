---
description: Source module internal/tui2/renderer_benchmark_test.go (258 lines).
resource: internal/tui2/renderer_benchmark_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: renderer_benchmark_test.go
type: Module
---

# Module renderer_benchmark_test.go

**Path**: `internal/tui2/renderer_benchmark_test.go`  
**Lines**: 258

## Snippet Preview

```
package tui2

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/samcharles93/tau/internal/theme"
)

func BenchmarkRendererPipeline(b *testing.B) {
	const width = 120
	for _, size := range []int{4 << 10, 16 << 10, 64 << 10} {
		payload := rendererBenchmarkPayload(size)
		plainPayload := rendererPlainBenchmarkPayload(size)
		name := fmt.Sprintf("%dKiB", size>>10)

		b.Run(name+"/glamour", func(b *testing.B) {
			m := newTestModel(&fakeRuntime{}, nil)
			ensureMDRenderer(m.mdCache, width-8)
			r := m.mdCache[mdCacheWidth(width-8)]
			md := "```result.md\n" + payload + "\n```"
			b.ReportAllocs()
			for b.Loop() {
				if _, err := r.Render(md); err != nil {
					b.Fatal(err)
				}
			}
		})
```
