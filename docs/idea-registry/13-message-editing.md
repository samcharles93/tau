# 13. Message Editing and Regeneration

## Status: Not yet planned

### Motivation

Users frequently want to edit a previous message and regenerate the assistant's response. This is standard in ChatGPT, Claude, and other chat UIs. Tau currently has no way to edit or retry.

### Design

**Edit previous user message**:

- `/edit <message_index>` opens an inline editor at the specified message
- After editing and pressing Enter, the conversation is truncated to just before the edited message
- The edited message is submitted as a new prompt, generating a fresh response

**Regenerate last response**:

- `/retry` or `/regenerate` command
- Removes the last assistant message and resubmits the previous user prompt
- Works with Ctrl+R keyboard shortcut when idle

**Implementation**:

- `ChatSessionState.TruncateTo(index int)` — removes all messages after the given index
- `ChatSessionState.ReplaceMessage(index int, content string)` — replaces a message at index
- New command: `RetryLastPromptCommand`
- New command: `EditMessageCommand{Index int, NewContent string}`
