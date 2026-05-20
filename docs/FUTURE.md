# Crush-Filtered Notes

This file captures the parts of the Crush design guide that are worth carrying
forward into AIM, filtered against AIM's actual scope.

## Adopt Now

- Use the Charm terminal stack: `charm.land/bubbletea/v2`,
	`charm.land/lipgloss/v2`, and `charm.land/glamour/v2`.
- Keep configuration and runtime wiring as services/packages, not global state.
- Keep the command/event seam between the chat runtime and the UI.
- Use a local `internal/pubsub` package for decoupled in-process fan-out.

## Adopt Later If AIM Grows Into It

- Add SQLite plus `sqlc` once persistence moves beyond a placeholder and AIM
	needs a real history/session store.
- Add a UI-specific `AGENTS.md` once `internal/chat/tui` exists and there is
	enough rendering/state complexity to justify local guidance.
- Consider richer tool metadata, prompt templates, or agent coordination only
	if AIM grows beyond a focused MaaS/OpenShift CLI.

## Explicitly Not For Now

- `charm.land/fantasy`
- Crush's provider matrix and login flows for unrelated vendors
- MCP, LSP, hooks, telemetry, and other agent-platform subsystems
- Wholesale vendoring of Crush internal packages

Crush is useful as a reference for interaction patterns and package boundaries.
It is not AIM's architecture by default.
