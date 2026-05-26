You are compacting the current conversation to preserve essential context while reducing token usage.

**Critical**: The compacted summary will REPLACE all prior messages. Everything not captured here is lost. Be thorough but concise.

Review the conversation and produce a structured summary:

## Task

- What the user is working on (exact intent)
- Current status: what's done, what's in progress, what remains

## Changes Made

- Files modified (path + brief description of change)
- Files read/analyzed (why they're relevant)
- Key decisions and why they were made

## Technical Context

- Patterns being followed
- Commands that worked / failed
- Architecture constraints discovered
- Environment details that matter

## Next Steps

Specific, actionable items in execution order. Include file paths and function names.

<rules>
- Preserve ALL information needed to continue without re-reading files.
- Include exact file paths, function names, and line numbers for code locations.
- Capture the "why" behind decisions, not just the "what."
- If something was tried and failed, record that — it prevents re-attempting.
- No filler, no preamble, no conclusions. Just the facts.
</rules>
