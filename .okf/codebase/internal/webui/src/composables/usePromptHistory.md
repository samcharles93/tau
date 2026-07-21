---
description: Source module internal/webui/src/composables/usePromptHistory.ts (56 lines).
resource: internal/webui/src/composables/usePromptHistory.ts
tags:
    - ts
    - source
timestamp: "2026-07-21T18:36:12Z"
title: usePromptHistory.ts
type: Module
---

# Module usePromptHistory.ts

**Path**: `internal/webui/src/composables/usePromptHistory.ts`  
**Lines**: 56

## Snippet Preview

```
import { ref } from 'vue'
import { useLocalStorage, watchDebounced } from '@vueuse/core'

const HISTORY_KEY = 'tau:prompt-history'
const DRAFT_KEY = 'tau:draft'
const MAX_ENTRIES = 100

export function usePromptHistory() {
  const history = useLocalStorage<string[]>(HISTORY_KEY, [])
  const draftStorage = useLocalStorage<string>(DRAFT_KEY, '')

  const cursor = ref(-1)
  const pendingDraft = ref('')

  function push(text: string) {
    if (!text.trim()) return
    const idx = history.value.indexOf(text)
    if (idx !== -1) history.value.splice(idx, 1)
    history.value.unshift(text)
    if (history.value.length > MAX_ENTRIES) history.value.length = MAX_ENTRIES
    cursor.value = -1
  }

  function prev(): string | null {
    if (history.value.length === 0) return null
    const next = cursor.value + 1
    if (next >= history.value.length) return null
    cursor.value = next
    return history.value[cursor.value]
  }
```
