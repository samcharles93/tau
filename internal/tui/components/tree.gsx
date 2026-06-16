package components

import (
	"strings"
	"github.com/grindlemire/go-tui"
	"github.com/samcharles93/tau/internal/theme"
)

// Tree is a generic reusable tree view component.
type Tree struct {
	Items    *tui.State[[]TreeItem]
	Selected *tui.State[int]
	OnSelect func(TreeItem)
	OnToggle func(TreeItem)
	OnDetail func(TreeItem)
	focused  *tui.State[bool]
}

// NewTree creates a new Tree component.
func NewTree(items *tui.State[[]TreeItem], selected *tui.State[int], onSelect func(TreeItem), onToggle func(TreeItem), onDetail func(TreeItem)) *Tree {
	return &Tree{
		Items:    items,
		Selected: selected,
		OnSelect: onSelect,
		OnToggle: onToggle,
		OnDetail: onDetail,
		focused:  tui.NewState(false),
	}
}

// BindApp wires state to the app.
func (t *Tree) BindApp(app *tui.App) {
	t.Items.BindApp(app)
	t.Selected.BindApp(app)
	t.focused.BindApp(app)
}

func (t *Tree) moveUp() {
	items := t.Items.Get()
	if len(items) == 0 {
		return
	}
	idx := t.Selected.Get() - 1
	if idx < 0 {
		idx = len(items) - 1
	}
	t.Selected.Set(idx)
}

func (t *Tree) moveDown() {
	items := t.Items.Get()
	if len(items) == 0 {
		return
	}
	idx := t.Selected.Get() + 1
	if idx >= len(items) {
		idx = 0
	}
	t.Selected.Set(idx)
}

func (t *Tree) handleLeft() {
	items := t.Items.Get()
	idx := t.Selected.Get()
	if idx < 0 || idx >= len(items) {
		return
	}
	item := items[idx]
	if item.HasChildren && item.Expanded {
		if t.OnToggle != nil {
			t.OnToggle(item)
		}
	}
}

func (t *Tree) handleRight() {
	items := t.Items.Get()
	idx := t.Selected.Get()
	if idx < 0 || idx >= len(items) {
		return
	}
	item := items[idx]
	if item.HasChildren && !item.Expanded {
		if t.OnToggle != nil {
			t.OnToggle(item)
		}
	} else {
		if t.OnDetail != nil {
			t.OnDetail(item)
		}
	}
}

func (t *Tree) handleEnter() {
	items := t.Items.Get()
	idx := t.Selected.Get()
	if idx < 0 || idx >= len(items) {
		return
	}
	item := items[idx]
	if item.HasChildren {
		if t.OnToggle != nil {
			t.OnToggle(item)
		}
	} else {
		if t.OnSelect != nil {
			t.OnSelect(item)
		}
	}
}

// KeyMap defines keyboard bindings for the Tree.
func (t *Tree) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnFocused(tui.KeyUp, func(tui.KeyEvent) { t.moveUp() }),
		tui.OnFocused(tui.KeyDown, func(tui.KeyEvent) { t.moveDown() }),
		tui.OnFocused(tui.KeyLeft, func(tui.KeyEvent) { t.handleLeft() }),
		tui.OnFocused(tui.KeyRight, func(tui.KeyEvent) { t.handleRight() }),
		tui.OnFocused(tui.KeyEnter, func(tui.KeyEvent) { t.handleEnter() }),
	}
}

// Focus marks the tree as focused.
func (t *Tree) Focus() {
	t.focused.Set(true)
}

// Blur marks the tree as blurred.
func (t *Tree) Blur() {
	t.focused.Set(false)
}

// IsFocusable reports true.
func (t *Tree) IsFocusable() bool {
	return true
}

// IsTabStop reports true.
func (t *Tree) IsTabStop() bool {
	return true
}

// IsFocused reports whether the tree is currently focused.
func (t *Tree) IsFocused() bool {
	return t.focused.Get()
}

// HandleEvent is required by Focusable.
func (t *Tree) HandleEvent(tui.Event) bool {
	return false
}

// Debug registration.
func init() {
	Register("tree", func(app *tui.App) (tui.Component, []DebugControl) {
		items := tui.NewStateForApp(app, []TreeItem{
			{ID: "root", Label: "Root node", HasChildren: true, Expanded: true, Depth: 0},
			{ID: "child1", Label: "Child node 1", HasChildren: true, Expanded: false, Depth: 1},
			{ID: "grandchild", Label: "Grandchild", Depth: 2},
			{ID: "child2", Label: "Child node 2", Depth: 1},
		})
		selected := tui.NewStateForApp(app, 0)
		comp := NewTree(items, selected, nil, func(item TreeItem) {
			list := items.Get()
			for i, it := range list {
				if it.ID == item.ID {
					list[i].Expanded = !item.Expanded
					break
				}
			}
			items.Set(list)
		}, nil)
		return comp, []DebugControl{
			{Label: "selected", Type: "text", State: intState(selected)},
		}
	})
}

// Render builds the tree element tree.
templ (t *Tree) Render() {
	<div class="flex-col grow" onFocus={func(*tui.Element) { t.Focus() }} onBlur={func(*tui.Element) { t.Blur() }} scrollable={tui.ScrollVertical} autoFocus={true}>
		for i, item := range t.Items.Get() {
			<div class="flex-row px-1">
				<span textStyle={theme.BodyStyle()}>{strings.Repeat("  ", item.Depth)}</span>
				if item.HasChildren {
					if item.Expanded {
						<span textStyle={theme.DimStyle()}>{"▼ "}</span>
					} else {
						<span textStyle={theme.DimStyle()}>{"▶ "}</span>
					}
				} else {
					<span textStyle={theme.BodyStyle()}>{"  "}</span>
				}

				if i == t.Selected.Get() {
					<span textStyle={theme.SelectedStyle()}>{item.Label}</span>
				} else {
					<span textStyle={theme.BodyStyle()}>{item.Label}</span>
				}
				if item.Description != "" {
					<span textStyle={theme.DescriptionStyle()}>{" — " + item.Description}</span>
				}
			</div>
		}
	</div>
}
