---
description: Source module internal/webui/src/lib/utils.ts (7 lines).
resource: internal/webui/src/lib/utils.ts
tags:
    - ts
    - source
timestamp: "2026-07-21T18:36:12Z"
title: utils.ts
type: Module
---

# Module utils.ts

**Path**: `internal/webui/src/lib/utils.ts`  
**Lines**: 7

## Snippet Preview

```
import type { ClassValue } from "clsx"
import { clsx } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
```
