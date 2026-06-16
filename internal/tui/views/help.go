package views

import (
	gt "github.com/grindlemire/go-tui"
	"github.com/samcharles93/tau/internal/theme"
)

// HelpView renders a full-screen alternate-screen help reference. It is opened
// with the /help (or /?) slash command, and closed with Esc or ?.
type HelpView struct {
	show *gt.State[bool]
}

// NewHelpView creates the help view.
func NewHelpView(show *gt.State[bool]) *HelpView {
	return &HelpView{show: show}
}

// BindApp wires reactive state.
func (h *HelpView) BindApp(app *gt.App) { h.show.BindApp(app) }

// KeyMap returns the help view key bindings.
func (h *HelpView) KeyMap() gt.KeyMap {
	return gt.KeyMap{
		gt.On(gt.KeyEscape, func(ke gt.KeyEvent) { h.show.Set(false) }),
		gt.On(gt.Rune('?'), func(ke gt.KeyEvent) { h.show.Set(false) }),
		gt.On(gt.Rune('q'), func(ke gt.KeyEvent) { h.show.Set(false) }),
	}
}

// Render builds the full-screen help view.
func (h *HelpView) Render(app *gt.App) *gt.Element {
	root := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithWidthPercent(100),
		gt.WithHeightPercent(100),
		gt.WithPadding(2),
		gt.WithGap(1),
	)

	root.AddChild(gt.New(
		gt.WithText("Tau Help"),
		gt.WithTextStyle(theme.BrandStyle()),
	))

	columns := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Row),
		gt.WithFlexGrow(1),
		gt.WithGap(4),
	)

	chat := helpColumn("Chat", chatHelpLines())
	cmds := helpColumn("Commands", slashHelpLines())
	nav := helpColumn("Navigation", navHelpLines())

	columns.AddChild(chat)
	columns.AddChild(cmds)
	columns.AddChild(nav)
	root.AddChild(columns)

	root.AddChild(gt.New(
		gt.WithText("? or Esc or q: close"),
		gt.WithTextStyle(theme.DescriptionStyle()),
		gt.WithFlexShrink(0),
	))

	return root
}

func helpColumn(title string, lines []helpLine) *gt.Element {
	col := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithFlexGrow(1),
		gt.WithGap(0),
	)
	col.AddChild(gt.New(
		gt.WithText(title),
		gt.WithTextStyle(theme.BrandStyle()),
	))
	for _, line := range lines {
		row := gt.New(
			gt.WithDisplay(gt.DisplayFlex),
			gt.WithDirection(gt.Row),
			gt.WithGap(2),
		)
		row.AddChild(gt.New(
			gt.WithText(line.key),
			gt.WithTextStyle(theme.SelectedStyle()),
			gt.WithWidth(18),
		))
		row.AddChild(gt.New(
			gt.WithText(line.desc),
			gt.WithTextStyle(theme.BodyStyle()),
			gt.WithTruncate(true),
		))
		col.AddChild(row)
	}
	return col
}

type helpLine struct {
	key  string
	desc string
}

func chatHelpLines() []helpLine {
	return []helpLine{
		{"Enter", "send message"},
		{"Shift+Enter", "insert a newline"},
		{"Ctrl+S", "steer / correct mid-stream"},
		{"Alt+Up", "recall last queued message"},
		{"Tab", "accept completion"},
		{"↑/↓", "navigate completions"},
	}
}

func navHelpLines() []helpLine {
	return []helpLine{
		{"Esc", "close view / clear input"},
		{"Ctrl+R", "toggle reasoning"},
		{"Ctrl+C", "cancel streaming / quit"},
		{"?", "toggle this help"},
	}
}

func slashHelpLines() []helpLine {
	return []helpLine{
		{"/help, /?", "toggle help"},
		{"/model <id>", "switch model"},
		{"/reasoning", "toggle reasoning"},
		{"/system", "set system prompt"},
		{"/settings", "open settings"},
		{"/session list", "list sessions"},
		{"/session tree", "session tree"},
		{"/resume <id>", "resume session"},
		{"/reload", "reload extensions"},
		{"/refresh", "refresh models"},
		{"/new, /clear", "new conversation"},
		{"/exit, /q", "quit"},
	}
}

var _ gt.KeyListener = (*HelpView)(nil)
