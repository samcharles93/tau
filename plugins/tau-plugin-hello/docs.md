# Hello Plugin

Minimal example plugin demonstrating the go-plugin extension API: slash commands, a tool, panels, and lifecycle events.

## Commands

- `/hello [name]` - say hello.
- `/hello panel` - render a one-shot demo panel.
- `/hello watch` - open a live, self-refreshing panel.
- `/hello close` - close the panel opened by `/hello watch`.

## Tools

- `hello_greet(name, enthusiasm)` - greet someone with `enthusiasm` exclamation marks.

## Source

See `plugins/tau-plugin-hello/main.go` in the tau repo for the full implementation, including how it exposes this file via
`Docs()`.
