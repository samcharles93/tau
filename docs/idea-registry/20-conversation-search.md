# 20. Conversation Search (`/find`)

## Status: Not yet planned

### Motivation

Long conversations with the agent can span dozens of messages. Users need a way to search within the current conversation for specific topics, files mentioned, or past results.

### Design

- `/find <query>` searches current conversation messages
- `/find /pattern/` for regex search
- Results shown in a modal list with context snippets
- Enter jumps to that message in the scroll view
- Search scope: current session only (across-branch search later)
