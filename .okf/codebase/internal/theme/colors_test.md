---
description: Source module internal/theme/colors_test.go (52 lines).
resource: internal/theme/colors_test.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: colors_test.go
type: Module
---

# Module colors_test.go

**Path**: `internal/theme/colors_test.go`  
**Lines**: 52

## Snippet Preview

```
package theme

import (
	"testing"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// TestBrandPaletteHexValues locks the six semantic brand colors to their
// specified hex values, so a future edit can't silently drift the palette.
func TestBrandPaletteHexValues(t *testing.T) {
	cases := []struct {
		name string
		got  termkit.Color
		want termkit.Color
	}{
		{"AccentColor (Warm Ochre)", AccentColor, termkit.Color{0xD1, 0x9A, 0x66}},
		{"SuccessColor (Sage Green)", SuccessColor, termkit.Color{0x7C, 0x9C, 0x72}},
		{"ErrorColor (Salmon Pink)", ErrorColor, termkit.Color{0xE0, 0x6C, 0x75}},
		{"PrimaryColor (Soft Off-White)", PrimaryColor, termkit.Color{0xAB, 0xB2, 0xBF}},
		{"SecondaryColor (Slate Blue-Grey)", SecondaryColor, termkit.Color{0x28, 0x33, 0x47}},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// TestBrandPaletteContrastAgainstCommonBackgrounds guards against the
```
