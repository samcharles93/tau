# 21. Clipboard Integration

## Status: Not yet planned

### Motivation

Copying model responses or tool outputs currently requires terminal selection (mouse + Shift). A keyboard shortcut to copy the last assistant response would improve UX significantly.

### Design

- `Ctrl+Y` copies last assistant message to system clipboard
- Uses `github.com/atotto/clipboard` or platform-specific shell commands
- `/copy` slash command with optional index: `/copy 3` copies message #3
- Notification: "Copied 1,234 characters to clipboard"
