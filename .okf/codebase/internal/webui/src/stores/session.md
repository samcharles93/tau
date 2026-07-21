---
description: Source module internal/webui/src/stores/session.ts (695 lines).
resource: internal/webui/src/stores/session.ts
tags:
    - ts
    - source
timestamp: "2026-07-21T18:36:12Z"
title: session.ts
type: Module
---

# Module session.ts

**Path**: `internal/webui/src/stores/session.ts`  
**Lines**: 695

## Snippet Preview

```
import { defineStore } from 'pinia'
import { ref } from 'vue'
import {
  command,
  type ChatCost,
  type ChatModelRef,
  type ChatParameters,
  type ChatReasoningDeltaEvent,
  type ChatResponseCancelledEvent,
  type ChatSessionPatch,
  type ChatSessionState,
  type ChatUsage,
  type ChildAgentResult,
  type ChildAgentStateEvent,
  type CommandRef,
  type CommandsChangedEvent,
  type ExtensionCommand,
  type ExtensionCommandsChangedEvent,
  type ExtensionsReloadedEvent,
  type ChatNotificationEvent,
  type ChatResponseCompletedEvent,
  type ChatResponseDeltaEvent,
  type ChatResponseStartedEvent,
  type ChatRuntimeErrorEvent,
  type ChatSessionSnapshotEvent,
  type ChatToolCallDeltaEvent,
  type ChatToolExecutionCompletedEvent,
  type ChatToolExecutionStartedEvent,
  type ChatToolOutputEvent,
  type Envelope,
```
