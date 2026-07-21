---
description: Source module internal/webui/vite.config.ts (44 lines).
resource: internal/webui/vite.config.ts
tags:
    - ts
    - source
timestamp: "2026-07-21T18:36:12Z"
title: vite.config.ts
type: Module
---

# Module vite.config.ts

**Path**: `internal/webui/vite.config.ts`  
**Lines**: 44

## Snippet Preview

```
import { fileURLToPath, URL } from 'node:url'
import tailwindcss from '@tailwindcss/vite'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [
    vue(),
    tailwindcss(),
    {
      name: 'css-first',
      enforce: 'post',
      transformIndexHtml(html) {
        // Move stylesheet links before the first script tag so CSS loads first.
        // Prevents "Layout was forced before the page was fully loaded" warnings.
        const links: string[] = []
        let out = html.replace(/<link[^>]*rel="stylesheet"[^>]*>/g, (m) => {
          links.push(m)
          return ''
        })
        out = out.replace(/<script/, links.join('\n    ') + '\n    <script')
        return out
      },
    },
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
```
