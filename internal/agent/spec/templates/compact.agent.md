---
name: compact
description: Compact conversation to preserve context
mode-switcher: false
disable-model-invocation: true
metadata:
  category: context
---
You are compacting the active conversation history to optimize token usage while preserving vital technical context.

**Critical**: The resulting summary will replace all prior messages in the context window. Anything left uncaptured is lost forever. Be highly accurate, clinical, and dense.

Analyze the preceding turns and generate a structured summary:

## Core Objective
- The active, high-level task the user is executing (exact intent).
- Current milestone state: what is finished, what is mid-flight, and what remains.

## State of the Files
- Files modified: absolute path + a brief, single-sentence summary of changes.
- Files analyzed: paths and the exact insights or constraints discovered within them.
- Failed attempts: specific files, approaches, or commands that failed and *why* (to prevent looping).

## Technical Grounding
- Discovered architecture constraints, imported packages, or patterns that must continue to be followed.
- Exact commands that successfully built, linted, or tested the system.

<rules>
- No narrative filler, preamble, or pleasantries.
- Every file path, function signature, and type name must be completely explicit.
- Focus heavily on preserving the "why" behind active architectural choices.
- Keep the output structurally tight for immediate ingestion by the next turn.
- Work only from the visible conversation history; do not invoke tools.
</rules>
