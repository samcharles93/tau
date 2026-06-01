// Package views provides full-screen views composed from layouts and components.
package views

import (
	"fmt"

	gt "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
)

// SessionListView shows a paginated, searchable list of saved sessions.
type SessionListView struct {
	show       *gt.State[bool]
	modal      *gt.Modal
	sessions   *gt.State[[]tauchat.SessionSummary]
	selected   *gt.State[int]
	cursor     string          // next page cursor
	hasMore    *gt.State[bool] // true when more pages are available
	filter     *gt.State[string]
	onSelect   func(tauchat.SessionSummary)
	onLoadMore func() // called when user requests next page
}

// NewSessionListView creates the session list modal.
func NewSessionListView(
	show *gt.State[bool],
	sessions *gt.State[[]tauchat.SessionSummary],
	selected *gt.State[int],
	onSelect func(tauchat.SessionSummary),
	onLoadMore func(),
) *SessionListView {
	return &SessionListView{
		show: show,
		modal: gt.NewModal(
			gt.WithModalOpen(show),
			gt.WithModalBackdrop("dim"),
			gt.WithModalTrapFocus(true),
		),
		sessions:   sessions,
		selected:   selected,
		hasMore:    gt.NewState(false),
		filter:     gt.NewState(""),
		onSelect:   onSelect,
		onLoadMore: onLoadMore,
	}
}

// BindApp wires reactive state.
func (l *SessionListView) BindApp(app *gt.App) {
	l.show.BindApp(app)
	l.modal.BindApp(app)
	l.sessions.BindApp(app)
	l.selected.BindApp(app)
	l.hasMore.BindApp(app)
	l.filter.BindApp(app)
}

// SetCursor updates pagination state.
func (l *SessionListView) SetCursor(nextCursor string, hasMore bool) {
	l.cursor = nextCursor
	l.hasMore.Set(hasMore)
}

// Render builds the session list modal.
func (l *SessionListView) Render(app *gt.App) *gt.Element {
	modalEl := app.MountPersistent(l, 1, func() gt.Component {
		return l.modal
	})

	content := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithWidth(60),
		gt.WithMaxHeight(24),
		gt.WithBorder(gt.BorderRounded),
		gt.WithBorderStyle(theme.BrandStyle()),
		gt.WithPadding(1),
		gt.WithGap(1),
	)

	// Title.
	content.AddChild(gt.New(
		gt.WithText("Sessions"),
		gt.WithTextStyle(theme.BrandStyle()),
	))

	items := l.sessions.Get()
	if len(items) == 0 {
		content.AddChild(gt.New(
			gt.WithText("No saved sessions."),
			gt.WithTextStyle(theme.DimStyle().Italic()),
		))
	} else {
		list := gt.New(
			gt.WithDisplay(gt.DisplayFlex),
			gt.WithDirection(gt.Column),
			gt.WithGap(0),
		)

		sel := clamp(l.selected.Get(), 0, len(items)-1)
		for i, session := range items {
			list.AddChild(l.renderRow(session, i == sel))
		}

		content.AddChild(list)

		// Load more button.
		if l.hasMore.Get() && l.onLoadMore != nil {
			loadMore := gt.New(
				gt.WithText("↓ Load more…"),
				gt.WithTextStyle(theme.DimStyle()),
				gt.WithTextAlign(gt.TextAlignCenter),
				gt.WithFocusable(true),
			)
			loadMore.SetOnFocus(func(*gt.Element) {
				l.onLoadMore()
			})
			content.AddChild(loadMore)
		}
	}

	content.AddChild(gt.New(
		gt.WithText("↑↓: navigate  ·  Enter: resume  ·  Esc: close"),
		gt.WithTextStyle(theme.DimStyle().Italic()),
	))

	modalEl.AddChild(content)

	wrapper := gt.New(gt.WithOverlay(true))
	wrapper.AddChild(modalEl)
	return wrapper
}

func (l *SessionListView) renderRow(session tauchat.SessionSummary, selected bool) *gt.Element {
	row := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Row),
		gt.WithGap(1),
		gt.WithPaddingTRBL(0, 0, 0, 0),
	)

	prefix := "  "
	style := theme.DimStyle()
	if selected {
		prefix = "› "
		style = theme.BrandStyle()
	}

	date := session.CreatedAt.Format("2006-01-02 15:04")

	line := fmt.Sprintf("%s%-20s  %-6s  msgs:%3d  %8s  $%s",
		prefix,
		date,
		truncateString(session.ModelID, 20),
		session.MessageCount,
		formatTokens(session.TotalTokens),
		formatCost(session.Cost),
	)

	row.AddChild(gt.New(
		gt.WithText(line),
		gt.WithTextStyle(style),
		gt.WithTruncate(true),
	))
	return row
}

// KeyMap for the session list view — Enter selects, Esc closes.
func (l *SessionListView) KeyMap() gt.KeyMap {
	return gt.KeyMap{
		gt.On(gt.KeyEnter, func(ke gt.KeyEvent) {
			items := l.sessions.Get()
			if len(items) == 0 {
				return
			}
			idx := clamp(l.selected.Get(), 0, len(items)-1)
			if l.onSelect != nil {
				l.onSelect(items[idx])
			}
		}),
		gt.On(gt.KeyEscape, func(ke gt.KeyEvent) {
			l.show.Set(false)
		}),
		gt.On(gt.KeyUp, func(ke gt.KeyEvent) {
			items := l.sessions.Get()
			if len(items) == 0 {
				return
			}
			idx := l.selected.Get() - 1
			if idx < 0 {
				idx = len(items) - 1
			}
			l.selected.Set(idx)
		}),
		gt.On(gt.KeyDown, func(ke gt.KeyEvent) {
			items := l.sessions.Get()
			if len(items) == 0 {
				return
			}
			idx := l.selected.Get() + 1
			if idx >= len(items) {
				idx = 0
			}
			l.selected.Set(idx)
		}),
	}
}

// --- Helpers ---

func formatTokens(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%dK", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

func formatCost(c float64) string {
	if c < 0.01 {
		return fmt.Sprintf("%.4f", c)
	}
	return fmt.Sprintf("%.2f", c)
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

func clamp(v, low, high int) int {
	if high < low {
		return low
	}
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}
