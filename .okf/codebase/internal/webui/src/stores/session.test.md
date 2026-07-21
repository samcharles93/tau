---
description: Source module internal/webui/src/stores/session.test.ts (397 lines).
resource: internal/webui/src/stores/session.test.ts
tags:
    - ts
    - source
timestamp: "2026-07-21T18:36:12Z"
title: session.test.ts
type: Module
---

# Module session.test.ts

**Path**: `internal/webui/src/stores/session.test.ts`  
**Lines**: 397

## Snippet Preview

```
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { messageReasoning, messageText, messageTools, useSessionStore } from './session'
import type { Envelope } from '@/lib/protocol'

function event(type: string, payload: unknown) {
  return { type, payload } as { type: string; [k: string]: unknown }
}

describe('session store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('applies the init message', () => {
    const s = useSessionStore()
    s.apply({
      type: 'init',
      session_id: 'sess-1',
      model: 'deepseek-v4-flash',
      provider: 'deepseek',
      commands: [{ name: '/model', label: 'model' }],
    })
    expect(s.sessionId).toBe('sess-1')
    expect(s.model).toBe('deepseek-v4-flash')
    expect(s.provider).toBe('deepseek')
    expect(s.commands).toHaveLength(1)
  })

  it('streams an assistant message from response deltas', () => {
```
