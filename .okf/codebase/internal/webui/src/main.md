---
description: Source module internal/webui/src/main.ts (12 lines).
resource: internal/webui/src/main.ts
tags:
    - ts
    - source
timestamp: "2026-07-21T18:36:12Z"
title: main.ts
type: Module
---

# Module main.ts

**Path**: `internal/webui/src/main.ts`  
**Lines**: 12

## Snippet Preview

```
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'

import './assets/index.css'
import 'highlight.js/styles/github-dark.css'

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.mount('#app')
```
