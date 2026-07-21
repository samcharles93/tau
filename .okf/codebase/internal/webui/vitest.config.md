---
description: Source module internal/webui/vitest.config.ts (16 lines).
resource: internal/webui/vitest.config.ts
tags:
    - ts
    - source
timestamp: "2026-07-21T18:36:12Z"
title: vitest.config.ts
type: Module
---

# Module vitest.config.ts

**Path**: `internal/webui/vitest.config.ts`  
**Lines**: 16

## Snippet Preview

```
import { fileURLToPath, URL } from 'node:url'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.{test,spec}.ts'],
  },
})
```
