# taui — TUI Framework

`pkg/taui` is Tau's standalone terminal UI framework. It provides a widget-based rendering engine for building inline terminal interfaces — no alternate screen, no full-screen mode. All rendering goes directly into the terminal scrollback.

## Design

taui renders widgets as a tree structure, where each widget implements the `Element` interface. The `TUI` engine drives the render loop: it collects widget output, formats it into ANSI escape sequences, and writes directly to stdout.

### Key Principles

- **Inline rendering**: Output scrolls naturally into the terminal scrollback. No alternate screen, curses, or full-screen takeover.
- **Widget tree**: The UI is a tree of widgets (`TUI` → `Box` → `Text`/`LineInput`/`Completions`/...).
- **Reactive via closure**: State changes call `engine.RequestRender()` to schedule the next frame. No reactive state framework — rendering pulls state from widget closures on each frame.
- **Kitty protocol**: Uses the Kitty terminal protocol for advanced features (synchronous rendering, hyperlinks, progress indicators).

## Package Layout

```
pkg/taui/
├── tui.go               # TUI engine — render loop, event handling, lifecycle
├── renderer.go          # ANSI rendering pipeline
├── box.go               # Vertical/horizontal layout container
├── text.go              # Styled text spans with color, bold, italic
├── lineinput.go         # Single-line text input with history
├── completions.go       # Drop-down autocomplete menu
├── paragraph.go         # Word-wrapped text blocks
├── toolrow.go           # Tool call status row with icons
├── loader.go            # Animated spinner
├── terminal.go          # Terminal initialization, raw mode, signal handling
├── stdin_buffer.go      # Buffered stdin reading
├── fuzzy.go             # Fuzzy string matching for completions
├── utils.go             # Shared utilities
├── termkit/
│   ├── color.go         # Color system (16 + 256 + true color)
│   ├── cursor.go        # Cursor movement and visibility
│   ├── hyperlink.go     # OSC 8 hyperlinks
│   ├── progress.go      # Progress indicators
│   ├── spinner.go       # Spinner frames
│   └── toollifecycle.go # Tool lifecycle theme (colors, icons)
```

## TUI Engine

```go
type TUI struct {
    // ...
}

func New(opts ...TUIOption) *TUI
func (t *TUI) Run(root Element) error
func (t *TUI) RequestRender()
func (t *TUI) Quit()
```

### Lifecycle

1. **`New()`** — Initializes the terminal (raw mode, Kitty protocol detection), starts the render loop.
2. **`Run(root)`** — Enters the main loop: reads stdin events, dispatches to the focused widget, renders the widget tree.
3. **`RequestRender()`** — Schedules a re-render for the next frame (debounced, non-blocking).
4. **`Quit()`** — Stops the render loop, restores terminal state.

### Render Loop

Each frame:
1. Process any pending stdin events.
2. Call `root.Render(width, height)` to collect widget output.
3. Format output with ANSI escape sequences.
4. Write to stdout (inline — no screen clearing).

## Element Interface

```go
type Element interface {
    Render(width, height int) RenderResult
    SetFocus(focused bool)
    IsFocused() bool
}
```

### RenderResult

```go
type RenderResult struct {
    Lines       []Line
    PreferredHeight int
    MinHeight   int
}
```

## Widgets

### Box

Layout container. Stacks children vertically or horizontally:

```go
func Box(opts ...BoxOption) Element
func VBox(children ...Element) Element    // vertical stack
func HBox(children ...Element) Element    // horizontal stack
```

Options: `WithBorder`, `WithPadding`, `WithGap`, `WithFlex`.

### Text

Styled text spans:

```go
func Text(content string, opts ...TextOption) Element
```

Options: `WithColor`, `WithBold`, `WithItalic`, `WithUnderline`, `WithBackground`.

### Paragraph

Word-wrapped text blocks that reflow on width changes:

```go
func Paragraph(content string, opts ...ParagraphOption) Element
```

### LineInput

Single-line text input with history navigation:

```go
func LineInput(value string, onChange func(string), opts ...LineInputOption) Element
```

Options: `WithPlaceholder`, `WithHistory`, `WithMultiline`.

### Completions

Drop-down autocomplete menu with fuzzy filtering:

```go
func Completions(source CompletionSet, opts ...CompletionOption) Element
```

`CompletionSet` provides completions for the current input. Results are fuzzy-filtered against the token under the cursor.

### ToolRow

Tool call status display:

```go
func ToolRow(name, args string, status ToolStatus, opts ...ToolRowOption) Element
```

Statuses: `ToolRunning` (spinner), `ToolOK` (check), `ToolError` (X).

### Loader

Animated spinner with optional message:

```go
func Loader(message string, frames []string) Element
```

### KeyValue, List, Table, Divider, Stack, StatusRow

Added for plugin-rendered panels (see [the Plugin SDK's Panels and
Views](./plugins.md#panels-and-views-rendering-structured-ui)), these follow
the package's actual builder-based constructor style rather than the
functional-options sketch above:

```go
func NewKeyValue(entries []KeyValueEntry) *KeyValue        // aligned "key: value" lines
func NewList(items []string, ordered bool) *List           // bulleted/numbered, word-wrapped
func NewTable(headers []string, rows [][]string) *Table    // column-aligned, shrinks to fit width
func NewDivider(label string) *Divider                     // horizontal rule, optional centered label
func NewStack(direction StackDirection, gap int) *Stack    // vertical (Container) or horizontal (zipped columns) layout
func NewStatusRow(label, detail string, state StatusRowState) *StatusRow // ToolRow's spinner/✓/✗ language, generalized
```

`Stack` embeds `Container`; `StackVertical` simply delegates to
`Container.Render` (which only concatenates lines top-to-bottom — there is no
other horizontal-layout primitive in the package), while `StackHorizontal`
renders each child independently and zips their lines side by side with `Gap`
spaces between columns. `StatusRow` adds a fourth, non-animated `Neutral`
state to `ToolRow`'s running/success/failed model, for status that isn't an
in-progress lifecycle.

## Color System

`pkg/taui/termkit/color.go` provides a layered color system:

- **16-color** (ANSI basic) — guaranteed compatibility.
- **256-color** — extended palette.
- **True color** (24-bit RGB) — full color when terminal supports it.

Colors auto-degrade to the best available mode for the terminal.

## Kitty Protocol

taui uses the Kitty terminal protocol for:

- **Synchronous rendering** (`CSI ? 2026 h`) — prevents screen tearing on render.
- **Hyperlinks** (OSC 8) — clickable links in terminal output.
- **Progress indicators** (OSC 9) — taskbar/dock progress on supported terminals.

## Tool Lifecycle Theme

`pkg/taui/termkit/toollifecycle.go` defines the visual theme for tool execution:

- Running tools: amber/yellow with spinner
- Successful tools: green with checkmark
- Failed tools: red with X mark

These are consumed by `ToolRow` in the TUI's tool display.

## Standalone Usage

taui is a standalone package with zero tau-internal imports. It can be used by any Go project:

```go
import "github.com/samcharles93/tau/pkg/taui"

func main() {
    t := taui.New()
    root := taui.VBox(
        taui.Text("Hello, World!", taui.WithColor(taui.ColorGreen)),
        taui.Paragraph("This is a taui application."),
    )
    t.Run(root)
}
```

The framework handles terminal initialization, raw mode, signal handling, and cleanup automatically.
