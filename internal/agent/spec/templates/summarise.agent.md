---
name: summarise
description: Summarize conversation for later continuation
disable-model-invocation: true
metadata:
  category: context
---
You are summarizing a completed conversation session to preserve complete context for continuing work at a later date.

**Critical**: This summary will act as the single source of truth when the conversation is restored. Assume all runtime memory and message arrays will be cleared.

Produce a comprehensive, structured brief matching the sections below:

## Current State
- **Active Task**: The precise user request being fulfilled.
- **Progress Tracking**: Clear list of items completed during this session.
- **In-Flight Work**: Exact point of interruption (what file/line is currently being touched).
- **Remaining Scope**: Granular next steps required to complete the total feature.

## Architecture & Code Changes
- **Modified**: Table or list of absolute paths, exact functions altered, and exact behavior changed.
- **Read/Analyzed**: Relevant files scrutinized for system boundaries.
- **Untouched but Required**: Known files that haven't been opened yet but are structural dependencies for the remaining steps.

## Technical Context
- Exact building, testing, or linting commands that were proven to work.
- Environmental variables, configuration settings, or runtime specs required.
- Specific architecture patterns or leaf dependencies (e.g., shared themes, specific packages) discovered.

## Exact Execution Order for Takeover
Provide a definitive, un-ambiguous list of next actions for the incoming agent. Do not use vague terms like "fix auth." Instead, specify file locations and implementation tasks:
1. [Actionable instruction] in `absolute/path/file.go:line`
2. [Actionable instruction]
3. Test with: `[exact command]`

**Tone**: Write a technical handover document for a engineering peer. No emojis, no pleasantries, maximum density.

Work only from the visible conversation history; do not invoke tools.
