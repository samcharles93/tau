# Component Debug System (`/debug components`)

## Idea
A `/debug components [NAME]` slash command that lets the developer (end-user) preview and test individual go-tui components in isolation. Without a name, it lists all registered components. With a name, it opens a full-screen debug view showing that component with controllable test state.

## Implementation sketch

**`internal/tui/components/registry.go`** — a global registry mapping component names to test constructors:

```go
type DebugFactory func(app *gt.App) (gt.Component, []debugControl)

type debugControl struct {
    Label string
    State *gt.State[string]  // or interface for bool/int/string
    Type  string              // "toggle", "cycle", "text"
}
```

Each component registers itself via `init()` or a `Register(name string, factory DebugFactory)` call. The registry lives in the `components` package so components can self-register.

**`internal/tui/views/debug.go`** — the debug view:

- Opens as a full-screen view (via `/debug components toggle`)
- Left half: the component under test, rendered with test state
- Right half: control panel showing each `debugControl` with buttons/sliders to mutate state
- Footer: component name + "Esc to close"
- Keymap: Esc dismisses, Tab cycles controls

**`/debug components` (no name)** — opens a `Modal` layout containing a `List` component showing all registered component names. Enter opens the debug view for the selected one.

**Why this matters:** With GSX components, you can't just `go run` a single component in isolation — they need an app context, state binding, and the full go-tui lifecycle. The debug view provides that scaffolding so you can iterate on a component's look-and-feel without launching the full chat TUI.
