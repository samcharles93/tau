---
description: Source module internal/webui/src/vite-env.d.ts (7 lines).
resource: internal/webui/src/vite-env.d.ts
tags:
    - ts
    - source
timestamp: "2026-07-21T18:36:12Z"
title: vite-env.d.ts
type: Module
---

# Module vite-env.d.ts

**Path**: `internal/webui/src/vite-env.d.ts`  
**Lines**: 7

## Snippet Preview

```
/// <reference types="vite/client" />

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<Record<string, unknown>, Record<string, unknown>, unknown>
  export default component
}
```
