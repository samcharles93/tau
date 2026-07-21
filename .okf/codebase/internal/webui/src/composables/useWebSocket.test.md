---
description: Source module internal/webui/src/composables/useWebSocket.test.ts (134 lines).
resource: internal/webui/src/composables/useWebSocket.test.ts
tags:
    - ts
    - source
timestamp: "2026-07-21T18:36:12Z"
title: useWebSocket.test.ts
type: Module
---

# Module useWebSocket.test.ts

**Path**: `internal/webui/src/composables/useWebSocket.test.ts`  
**Lines**: 134

## Snippet Preview

```
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope } from 'vue'
import { useWebSocket } from './useWebSocket'
import type { Envelope } from '@/lib/protocol'

class MockWebSocket {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSED = 3

  static instances: MockWebSocket[] = []

  readyState = MockWebSocket.CONNECTING
  sent: string[] = []
  onopen: (() => void) | null = null
  onmessage: ((ev: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null

  constructor(public url: string) {
    MockWebSocket.instances.push(this)
  }

  send(data: string) {
    this.sent.push(data)
  }

  close() {
    this.readyState = MockWebSocket.CLOSED
    this.onclose?.()
```
