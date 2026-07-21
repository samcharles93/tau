---
description: Source module internal/webui/src/router/index.ts (19 lines).
resource: internal/webui/src/router/index.ts
tags:
    - ts
    - source
timestamp: "2026-07-21T18:36:12Z"
title: index.ts
type: Module
---

# Module index.ts

**Path**: `internal/webui/src/router/index.ts`  
**Lines**: 19

## Snippet Preview

```
import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory('/'),
  routes: [
    {
      path: '/',
      name: 'chat',
      component: () => import('@/pages/ChatPage.vue'),
    },
    // Unknown paths fall back to the chat view (SPA single-page behavior).
    {
      path: '/:pathMatch(.*)*',
      redirect: '/',
    },
  ],
})

export default router
```
