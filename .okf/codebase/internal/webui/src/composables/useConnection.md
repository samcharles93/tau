---
description: Source module internal/webui/src/composables/useConnection.ts (16 lines).
resource: internal/webui/src/composables/useConnection.ts
tags:
    - ts
    - source
timestamp: "2026-07-21T18:36:12Z"
title: useConnection.ts
type: Module
---

# Module useConnection.ts

**Path**: `internal/webui/src/composables/useConnection.ts`  
**Lines**: 16

## Snippet Preview

```
import { inject, provide, type InjectionKey, type Ref } from 'vue'
import type { ConnectionStatus } from '@/composables/useWebSocket'

const ConnectionKey: InjectionKey<Ref<ConnectionStatus>> = Symbol('connection')

/** Provide the live WebSocket connection status to descendant components. */
export function provideConnection(status: Ref<ConnectionStatus>) {
  provide(ConnectionKey, status)
}

/** Inject the connection status; throws if no provider is mounted above. */
export function useConnection(): Ref<ConnectionStatus> {
  const status = inject(ConnectionKey)
  if (!status) throw new Error('useConnection requires provideConnection in an ancestor')
  return status
}
```
