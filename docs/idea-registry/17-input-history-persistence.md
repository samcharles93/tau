# 17. Input History Persistence

## Status: Not yet planned

### Motivation

Shell-like input history (up/down arrow to recall previous prompts) is standard in chat UIs. Currently pressing Up/Down scrolls the message view, but there's no way to recall previous inputs.

### Design

- `ChatPanel` tracks a ring buffer of recent inputs (`[]string`, max 100)
- Up arrow in empty input: recall previous prompt
- Down arrow: move forward through history
- History persists across sessions in `~/.tau/history.jsonl`
- `/history` command to view/search input history
