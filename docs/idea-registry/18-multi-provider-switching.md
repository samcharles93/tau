# 18. Multi-Provider Switching in TUI

## Status: Not yet planned

### Motivation

Users configure multiple providers (OpenAI, Anthropic, local Ollama) but can only use one per session. Switching requires editing config or restarting with `--provider`. The TUI should allow on-the-fly provider switching.

### Design

- `/provider <name>` slash command to switch providers
- Provider list in Settings modal with current provider highlighted
- On switch: create a new coordinator session or reconfigure the existing one
- Model list refreshes automatically for the new provider
- Provider name shown in header and status bar (already partially done)

### Challenges

- Auth token may need re-resolution for the new provider
- Different providers may have different model APIs and compat configs
- The streaming layer needs to handle provider-specific quirks
