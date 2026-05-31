## 7. Component Debug System (`/debug components`)

### Idea
A `/debug components [NAME]` slash command that lets the developer (end-user) preview and test individual go-tui components in isolation. Without a name, it lists all registered components. With a name, it opens a full-screen debug view showing that component with controllable test state.

### Implementation sketch

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

---

## 8. Initial Component Set (Settings Prerequisites)

### Idea
Before building the settings view, build the component primitives it needs. Order matters: bottom-up, each component tested via `/debug components` before the next depends on it.

### Implementation order

| # | Component | GSX file | Shadcn inspiration | What it is |
|---|-----------|----------|-------------------|-------------|
| 1 | `Select` | `components/select.gsx` | `select.tsx` + `native-select.tsx` | Horizontal option cycler: `‹ model-a ›` with Left/Right arrows, Enter to confirm. Exposes `State[string]`, `Options []string`, `OnChange`. |
| 2 | `Toggle` | `components/toggle.gsx` | `switch.tsx` | Boolean toggle: `[X] Show reasoning` / `[ ] Show reasoning`. Exposes `State[bool]`, `Label string`, `OnChange`. |
| 3 | `List` | `components/list.gsx` | `command.tsx` (item list portion) | Scrollable selectable list: renders `[]Item` with highlighted selection, Up/Down navigation, Enter to select. Exposes `State[[]Item]`, `State[int]` selected index, `OnSelect`. |
| 4 | `Modal` | `layouts/modal.gsx` | `dialog.tsx` | Full-screen backdrop overlay with focus trap, Esc-to-close. Exposes `State[bool]` open/close. Renders a single child component slot. |
| 5 | `Form` | `layouts/form.gsx` | `field.tsx` | Label+control pairs with Tab/Shift+Tab navigation. Exposes `[]FormField` where each field has `Label`, `Description`, and a child component. |

### Component contracts (Go types)

```go
// internal/tui/components/select.go (hand-written, not GSX — just types)
package components

type SelectOption struct {
    Value string
    Label string
}

// Select is a GSX component. Its Go-side type is:
// type Select struct {
//     Value   *gt.State[string]
//     Options []SelectOption
//     OnChange func(string)
// }
```

```go
// internal/tui/components/toggle.go
package components

// Toggle is a GSX component:
// type Toggle struct {
//     Checked  *gt.State[bool]
//     Label    string
//     OnChange func(bool)
// }
```

```go
// internal/tui/components/list.go
package components

type ListItem struct {
    ID          string
    Label       string
    Description string
    Disabled    bool
}

// List GSX component:
// type List struct {
//     Items    *gt.State[[]ListItem]
//     Selected *gt.State[int]
//     OnSelect func(ListItem)
// }
```

```go
// internal/tui/layouts/modal.go
package layouts

// Modal GSX component:
// type Modal struct {
//     Open    *gt.State[bool]
//     Title   string
//     Width   int  // 0 = auto
//     Height  int  // 0 = auto
// }
// Child content is added via GSX children: <Modal><Content /></Modal>
```

```go
// internal/tui/layouts/form.go
package layouts

type FormField struct {
    Label       string
    Description string
    Control     gt.Component  // the child component (Select, Toggle, etc.)
}

// Form GSX component:
// type Form struct {
//     Fields []FormField
// }
```

### After components are built: the Settings view

`internal/tui/views/settings.go` composes:
```
Modal(Open: showSettings)
  └─ Form(Fields: [
       {Label: "Model", Control: Select(Options: models, Value: currentModel)},
       {Label: "Reasoning", Control: Toggle(Checked: showReasoning)},
     ])
```

The view file is ~40 lines of wiring. All rendering, focus, and keyboard behavior lives in the components/layouts below it.

### Debug integration
Each component registers in `init()`:
```go
// In components/select.gsx (or a companion select.go)
func init() {
    Register("select", func(app *gt.App) (gt.Component, []debugControl) {
        value := gt.NewStateForApp(app, "model-a")
        comp := &Select{
            Value:   value,
            Options: []SelectOption{{"model-a", "Model A"}, {"model-b", "Model B"}},
        }
        return comp, []debugControl{
            {Label: "value", State: typedState(value), Type: "cycle", Options: []string{"model-a", "model-b"}},
        }
    })
}
```

Then `tau` → `/debug components` → select "select" from the list → full-screen debug view opens with the Select component cycling through test models and the control panel on the right showing the current state.

---

## 9. Automated Vulnerability Scanning and Issue Creation

### Status: Implemented
Vulnerability scanning was introduced into the GitHub Actions workflows (under `.github/workflows/vuln-check.yml`), which runs daily and raises GitHub issues when code-level vulnerabilities are identified.




