// Package views provides full-screen views composed from layouts and components.
package views

import (
	"fmt"
	"hash/fnv"

	gt "github.com/grindlemire/go-tui"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/internal/tui/components"
)

// DebugView renders a component in isolation with a control panel
// for mutating its state. Used by /debug components <name>.
type DebugView struct {
	name      string
	show      *gt.State[bool]
	modal     *gt.Modal
	factory   components.DebugFactory
	component gt.Component
	controls  []components.DebugControl
}

// NewDebugView creates a debug view for the named component.
// Returns nil if the component is not registered.
func NewDebugView(name string, show *gt.State[bool]) *DebugView {
	factory := components.Get(name)
	if factory == nil {
		return nil
	}
	return &DebugView{
		name: name,
		show: show,
		modal: gt.NewModal(
			gt.WithModalOpen(show),
			gt.WithModalBackdrop("dim"),
			gt.WithModalTrapFocus(true),
		),
	}
}

// BindApp creates the component instance and wires state.
func (d *DebugView) BindApp(app *gt.App) {
	d.show.BindApp(app)
	d.modal.BindApp(app)
	if d.factory == nil {
		d.factory = components.Get(d.name)
	}
	if d.factory == nil {
		return
	}
	if d.component == nil {
		comp, controls := d.factory(app)
		d.component = comp
		d.controls = controls
	}
}

func (d *DebugView) UpdateProps(fresh gt.Component) {
	f, ok := fresh.(*DebugView)
	if !ok {
		return
	}
	if d.name != f.name {
		d.name = f.name
		d.factory = f.factory
		d.component = nil
		d.controls = nil
	}
}

// Render builds the debug view: left half shows the component,
// right half shows the control panel.
func (d *DebugView) Render(app *gt.App) *gt.Element {
	modalEl := app.MountPersistent(d, 1, func() gt.Component {
		return d.modal
	})

	if d.component == nil {
		modalEl.AddChild(gt.New(gt.WithText("component not found: " + d.name)))
		wrapper := gt.New(gt.WithOverlay(true))
		wrapper.AddChild(modalEl)
		return wrapper
	}

	root := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithWidthPercent(100),
		gt.WithHeightPercent(100),
	)

	// Header
	header := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Row),
		gt.WithBorder(gt.BorderSingle),
		gt.WithBorderStyle(theme.BorderStyle()),
		gt.WithPaddingTRBL(0, 1, 0, 1),
		gt.WithFlexShrink(0),
	)
	header.AddChild(gt.New(
		gt.WithText("Debug: "+d.name),
		gt.WithTextStyle(theme.BrandStyle()),
	))
	header.AddChild(gt.New(gt.WithFlexGrow(1)))
	header.AddChild(gt.New(
		gt.WithText("Esc to close"),
		gt.WithTextStyle(theme.DimStyle()),
	))
	root.AddChild(header)

	// Body: component (left) + controls (right)
	body := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Row),
		gt.WithFlexGrow(1),
		gt.WithGap(2),
		gt.WithPadding(1),
	)

	baseIndex := componentBaseIndex(d.name)

	// Left: component preview
	left := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithFlexGrow(1),
		gt.WithBorder(gt.BorderSingle),
		gt.WithBorderStyle(theme.BorderStyle()),
		gt.WithPadding(1),
	)
	compEl := app.MountPersistent(d, baseIndex, func() gt.Component {
		return d.component
	})
	left.AddChild(compEl)
	body.AddChild(left)

	// Right: control panel
	right := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithWidth(40),
		gt.WithBorder(gt.BorderSingle),
		gt.WithBorderStyle(theme.BorderStyle()),
		gt.WithPadding(1),
		gt.WithGap(1),
	)
	right.AddChild(gt.New(
		gt.WithText("Controls"),
		gt.WithTextStyle(theme.BrandStyle()),
	))
	for i, ctrl := range d.controls {
		ctrlComp := &DebugControlComp{
			ctrl:    ctrl,
			focused: gt.NewState(false),
		}
		ctrlEl := app.MountPersistent(d, baseIndex+i+1, func() gt.Component {
			return ctrlComp
		})
		right.AddChild(ctrlEl)
	}
	body.AddChild(right)
	root.AddChild(body)

	// Footer
	footer := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Row),
		gt.WithBorder(gt.BorderSingle),
		gt.WithBorderStyle(theme.BorderStyle()),
		gt.WithPaddingTRBL(0, 1, 0, 1),
		gt.WithFlexShrink(0),
	)
	footer.AddChild(gt.New(
		gt.WithText("Tab: cycle controls · Esc: close"),
		gt.WithTextStyle(theme.DimStyle()),
	))
	modalEl.AddChild(root)

	wrapper := gt.New(gt.WithOverlay(true))
	wrapper.AddChild(modalEl)
	return wrapper
}

// KeyMap for the debug view.
func (d *DebugView) KeyMap() gt.KeyMap {
	return gt.KeyMap{
		gt.On(gt.KeyEscape, func(ke gt.KeyEvent) {
			d.show.Set(false)
		}),
	}
}

// DebugControlComp is a helper component that wraps a DebugControl to make it focusable and interactive.
type DebugControlComp struct {
	ctrl    components.DebugControl
	focused *gt.State[bool]
}

func (c *DebugControlComp) BindApp(app *gt.App) {
	c.focused.BindApp(app)
	c.ctrl.State.BindApp(app)
}

func (c *DebugControlComp) IsFocusable() bool {
	return c.ctrl.Type == "toggle" || c.ctrl.Type == "cycle"
}

func (c *DebugControlComp) IsTabStop() bool {
	return c.ctrl.Type == "toggle" || c.ctrl.Type == "cycle"
}

func (c *DebugControlComp) Focus() {
	c.focused.Set(true)
}

func (c *DebugControlComp) Blur() {
	c.focused.Set(false)
}

func (c *DebugControlComp) IsFocused() bool {
	return c.focused.Get()
}

func (c *DebugControlComp) HandleEvent(gt.Event) bool {
	return false
}

func (c *DebugControlComp) KeyMap() gt.KeyMap {
	km := gt.KeyMap{}
	switch c.ctrl.Type {
	case "toggle":
		km = append(km, gt.OnFocused(gt.KeyEnter, func(gt.KeyEvent) {
			c.toggle()
		}))
		km = append(km, gt.OnFocused(gt.Rune(' '), func(gt.KeyEvent) {
			c.toggle()
		}))
	case "cycle":
		km = append(km, gt.OnFocused(gt.KeyRight, func(gt.KeyEvent) {
			c.cycle(1)
		}))
		km = append(km, gt.OnFocused(gt.KeyLeft, func(gt.KeyEvent) {
			c.cycle(-1)
		}))
		km = append(km, gt.OnFocused(gt.KeyEnter, func(gt.KeyEvent) {
			c.cycle(1)
		}))
		km = append(km, gt.OnFocused(gt.Rune(' '), func(gt.KeyEvent) {
			c.cycle(1)
		}))
	}
	return km
}

func (c *DebugControlComp) toggle() {
	val := c.ctrl.State.Get()
	if val == "true" {
		c.ctrl.State.Set("false")
	} else {
		c.ctrl.State.Set("true")
	}
}

func (c *DebugControlComp) cycle(dir int) {
	if len(c.ctrl.Options) == 0 {
		return
	}
	current := c.ctrl.State.Get()
	idx := -1
	for i, opt := range c.ctrl.Options {
		if opt == current {
			idx = i
			break
		}
	}
	if idx == -1 {
		idx = 0
	} else {
		idx = (idx + dir + len(c.ctrl.Options)) % len(c.ctrl.Options)
	}
	c.ctrl.State.Set(c.ctrl.Options[idx])
}

func (c *DebugControlComp) Render(app *gt.App) *gt.Element {
	row := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithGap(0),
	)

	var labelStyle gt.Style
	if c.focused.Get() {
		labelStyle = theme.BrandStyle().Bold()
	} else {
		labelStyle = theme.BodyStyle().Bold()
	}

	row.AddChild(gt.New(
		gt.WithText(c.ctrl.Label),
		gt.WithTextStyle(labelStyle),
	))

	switch c.ctrl.Type {
	case "toggle":
		checked := c.ctrl.State.Get() == "true"
		mark := "[ ]"
		if checked {
			mark = "[X]"
		}
		var valStyle gt.Style
		if c.focused.Get() {
			valStyle = theme.SelectedStyle()
		} else {
			valStyle = theme.DimStyle()
		}
		row.AddChild(gt.New(
			gt.WithText(fmt.Sprintf("%s  (Enter/Space to toggle)", mark)),
			gt.WithTextStyle(valStyle),
		))
	case "cycle":
		var valStyle gt.Style
		if c.focused.Get() {
			valStyle = theme.SelectedStyle()
		} else {
			valStyle = theme.BodyStyle()
		}
		row.AddChild(gt.New(
			gt.WithText(fmt.Sprintf("‹ %s ›  (Left/Right to cycle)", c.ctrl.State.Get())),
			gt.WithTextStyle(valStyle),
		))
	default:
		row.AddChild(gt.New(
			gt.WithText(c.ctrl.State.Get()),
			gt.WithTextStyle(theme.BodyStyle()),
		))
	}

	rowEl := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithOnFocus(func(*gt.Element) { c.Focus() }),
		gt.WithOnBlur(func(*gt.Element) { c.Blur() }),
	)
	rowEl.AddChild(row)

	return rowEl
}

func (c *DebugControlComp) UpdateProps(fresh gt.Component) {
	f, ok := fresh.(*DebugControlComp)
	if !ok {
		return
	}
	c.ctrl = f.ctrl
}

func componentBaseIndex(name string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	val := int(h.Sum32() % 50)
	return 1000 + val*1000
}

// DebugListView shows all registered components in a modal list.
type DebugListView struct {
	show   *gt.State[bool]
	modal  *gt.Modal
	items  *gt.State[[]components.ListItem]
	sel    *gt.State[int]
	onOpen func(name string)
}

// NewDebugListView creates the component list view.
func NewDebugListView(show *gt.State[bool], onOpen func(name string)) *DebugListView {
	names := components.ListNames()
	items := make([]components.ListItem, len(names))
	for i, n := range names {
		items[i] = components.ListItem{ID: n, Label: n}
	}
	return &DebugListView{
		show: show,
		modal: gt.NewModal(
			gt.WithModalOpen(show),
			gt.WithModalBackdrop("dim"),
			gt.WithModalTrapFocus(true),
		),
		items:  gt.NewState(items),
		sel:    gt.NewState(0),
		onOpen: onOpen,
	}
}

// BindApp wires state.
func (l *DebugListView) BindApp(app *gt.App) {
	l.show.BindApp(app)
	l.modal.BindApp(app)
	l.items.BindApp(app)
	l.sel.BindApp(app)
}

// KeyMap for the debug list view.
func (l *DebugListView) KeyMap() gt.KeyMap {
	return gt.KeyMap{
		gt.On(gt.KeyEscape, func(ke gt.KeyEvent) {
			l.show.Set(false)
		}),
	}
}

// Render builds the debug list view.
func (l *DebugListView) Render(app *gt.App) *gt.Element {
	modalEl := app.MountPersistent(l, 1, func() gt.Component {
		return l.modal
	})

	content := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithWidth(48),
		gt.WithHeight(20),
		gt.WithBorder(gt.BorderRounded),
		gt.WithBorderStyle(theme.BrandStyle()),
		gt.WithPadding(1),
		gt.WithGap(1),
	)
	content.AddChild(gt.New(
		gt.WithText("Debug Components"),
		gt.WithTextStyle(theme.BrandStyle()),
	))

	list := components.NewList(l.items, l.sel, func(item components.ListItem) {
		if l.onOpen != nil {
			l.show.Set(false)
			l.onOpen(item.ID)
		}
	})
	listEl := app.MountPersistent(l, 0, func() gt.Component {
		return list
	})
	content.AddChild(listEl)

	content.AddChild(gt.New(
		gt.WithText("Enter: open  ·  Esc: close"),
		gt.WithTextStyle(theme.DimStyle().Italic()),
	))

	modalEl.AddChild(content)

	wrapper := gt.New(gt.WithOverlay(true))
	wrapper.AddChild(modalEl)
	return wrapper
}
