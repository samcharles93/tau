package views

import (
	gt "github.com/grindlemire/go-tui"
	"github.com/samcharles93/tau/internal/theme"
)

// HelpView renders a scrollable help panel showing keybindings and slash
// commands. It is opened with the /help (or /?) slash command.
type HelpView struct {
	show  *gt.State[bool]
	modal *gt.Modal
}

// NewHelpView creates the help view.
func NewHelpView(show *gt.State[bool]) *HelpView {
	return &HelpView{
		show: show,
		modal: gt.NewModal(
			gt.WithModalOpen(show),
			gt.WithModalBackdrop("dim"),
			gt.WithModalTrapFocus(false),
		),
	}
}

// BindApp wires reactive state.
func (h *HelpView) BindApp(app *gt.App) {
	h.show.BindApp(app)
	h.modal.BindApp(app)
}

// KeyMap returns the help view key bindings.
func (h *HelpView) KeyMap() gt.KeyMap {
	return gt.KeyMap{
		gt.On(gt.KeyEscape, func(ke gt.KeyEvent) {
			h.show.Set(false)
		}),
	}
}

// Render builds the help modal.
func (h *HelpView) Render(app *gt.App) *gt.Element {
	modalEl := app.MountPersistent(h, 1, func() gt.Component {
		return h.modal
	})

	content := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithWidth(56),
		gt.WithHeight(22),
		gt.WithBorder(gt.BorderRounded),
		gt.WithBorderStyle(theme.BrandStyle()),
		gt.WithPadding(1),
		gt.WithGap(1),
		gt.WithScrollable(gt.ScrollVertical),
	)

	content.AddChild(gt.New(
		gt.WithText("Tau Help"),
		gt.WithTextStyle(theme.BrandStyle()),
	))

	for _, section := range helpSections() {
		content.AddChild(gt.New(
			gt.WithText(section.title),
			gt.WithTextStyle(theme.BodyStyle().Bold()),
		))
		for _, line := range section.lines {
			content.AddChild(gt.New(
				gt.WithText(line),
				gt.WithTextStyle(theme.DimStyle()),
				gt.WithTruncate(true),
			))
		}
	}

	content.AddChild(gt.New(gt.WithHeight(1)))
	content.AddChild(gt.New(
		gt.WithText("Esc: close"),
		gt.WithTextStyle(theme.DimStyle().Italic()),
	))

	modalEl.AddChild(content)

	wrapper := gt.New(gt.WithOverlay(true))
	wrapper.AddChild(modalEl)
	return wrapper
}

type helpSection struct {
	title string
	lines []string
}

func helpSections() []helpSection {
	return []helpSection{
		{
			title: "Chat",
			lines: []string{
				"Enter         send message",
				"Shift+Enter   insert a newline",
				"Ctrl+S        steer / correct the model mid-stream",
				"Alt+Up        recall the last queued message",
				"Tab           accept the highlighted completion",
				"↑/↓           navigate completions",
			},
		},
		{
			title: "Navigation",
			lines: []string{
				"Esc           close the current view or clear input",
				"Ctrl+R        toggle reasoning visibility",
				"Ctrl+C        cancel streaming (or quit when idle)",
			},
		},
		{
			title: "Slash commands",
			lines: []string{
				"/help, /?              toggle this help panel",
				"/model <id>            switch model",
				"/reasoning [on|off]    toggle reasoning",
				"/system <prompt>       set the system prompt",
				"/settings              open settings",
				"/session list          list saved sessions",
				"/session tree          open session tree dashboard",
				"/session info <id>     show session details",
				"/session export <id>   export session to JSONL",
				"/session delete <id>   delete a session",
				"/resume <id>           resume a saved session",
				"/reload                reload extensions",
				"/refresh, /models      refresh available models",
				"/new, /clear, /reset   start a new conversation",
				"/exit, /quit, /q       quit",
			},
		},
		{
			title: "Developer",
			lines: []string{
				"/debug components [name]  inspect TUI components",
			},
		},
	}
}

var _ gt.KeyListener = (*HelpView)(nil)
