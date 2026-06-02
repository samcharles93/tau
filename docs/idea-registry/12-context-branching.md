# 12. Conversation Branching (`/branch`)

## Status: Not yet planned

### Motivation

Users often want to explore a different direction from a conversation fork point — try a different prompt, experiment with an alternative approach, or redo a response. Currently the only option is `/new` to start over completely.

### Design

- Each branch is a separate message history sharing a common prefix
- `/branch` creates a branch at the current conversation state
- `/branch list` shows all branches in the current session
- `/branch switch <id>` switches to a different branch
- Visual indicator in the TUI showing branch depth / position
- Branches persisted in the session JSONL with a `branch_id` field per message

### Storage

- JSONL stores `branch_id` alongside each message
- Session metadata tracks `current_branch_id` and `branch_ids` list
- Branches share common messages (by reference, not duplication)
