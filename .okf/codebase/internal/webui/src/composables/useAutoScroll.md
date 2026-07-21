---
description: Source module internal/webui/src/composables/useAutoScroll.ts (34 lines).
resource: internal/webui/src/composables/useAutoScroll.ts
tags:
    - ts
    - source
timestamp: "2026-07-21T18:36:12Z"
title: useAutoScroll.ts
type: Module
---

# Module useAutoScroll.ts

**Path**: `internal/webui/src/composables/useAutoScroll.ts`  
**Lines**: 34

## Snippet Preview

```
import { nextTick, onMounted, ref, type Ref } from 'vue'

/**
 * useAutoScroll keeps a scroll container pinned to the bottom as content grows,
 * unless the user has scrolled up, in which case it stays put so they can read
 * back through history.
 */
export function useAutoScroll(): {
  container: Ref<HTMLElement | null>
  scrollToBottom: () => void
  onScroll: () => void
} {
  const container = ref<HTMLElement | null>(null)
  const pinned = ref(true)

  function scrollToBottom() {
    const el = container.value
    if (!el || !pinned.value) return
    nextTick(() => {
      el.scrollTop = el.scrollHeight
    })
  }

  function onScroll() {
    const el = container.value
    if (!el) return
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight
    pinned.value = distanceFromBottom < 80
  }

```
