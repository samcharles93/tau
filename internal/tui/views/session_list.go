package views

import (
	"fmt"

	gt "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/internal/tui/components"
)

// SessionListView shows a paginated list of saved sessions in a full-screen
// alternate-screen view. It uses the reusable List component so focus and
// keyboard navigation work correctly.
type SessionListView struct {
	show       *gt.State[bool]
	sessions   *gt.State[[]tauchat.SessionSummary]
	selected   *gt.State[int]
	hasMore    *gt.State[bool]
	onSelect   func(tauchat.SessionSummary)
	onLoadMore func()
	list       *components.List
	items      *gt.State[[]components.ListItem]
}

// NewSessionListView creates the session list view.
func NewSessionListView(
	show *gt.State[bool],
	sessions *gt.State[[]tauchat.SessionSummary],
	selected *gt.State[int],
	onSelect func(tauchat.SessionSummary),
	onLoadMore func(),
) *SessionListView {
	items := gt.NewState([]components.ListItem{})
	v := &SessionListView{
		show:       show,
		sessions:   sessions,
		selected:   selected,
		hasMore:    gt.NewState(false),
		onSelect:   onSelect,
		onLoadMore: onLoadMore,
		items:      items,
	}
	v.list = components.NewList(items, selected, v.handleListSelect)
	return v
}

// BindApp wires reactive state.
func (l *SessionListView) BindApp(app *gt.App) {
	l.show.BindApp(app)
	l.sessions.BindApp(app)
	l.selected.BindApp(app)
	l.hasMore.BindApp(app)
	l.items.BindApp(app)
	l.list.BindApp(app)
}

// SetCursor updates pagination state.
func (l *SessionListView) SetCursor(_ string, hasMore bool) {
	l.hasMore.Set(hasMore)
}

// KeyMap returns the view key bindings.
func (l *SessionListView) KeyMap() gt.KeyMap {
	return gt.KeyMap{
		gt.On(gt.KeyEscape, func(ke gt.KeyEvent) {
			l.show.Set(false)
		}),
	}
}

// Render builds the full-screen session list.
func (l *SessionListView) Render(app *gt.App) *gt.Element {
	l.items.Set(l.buildItems())

	root := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithWidthPercent(100),
		gt.WithHeightPercent(100),
		gt.WithPadding(1),
		gt.WithGap(1),
	)

	header := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Row),
		gt.WithJustify(gt.JustifySpaceBetween),
		gt.WithFlexShrink(0),
	)
	header.AddChild(gt.New(
		gt.WithText("Sessions"),
		gt.WithTextStyle(theme.BrandStyle()),
	))
	header.AddChild(gt.New(
		gt.WithText(fmt.Sprintf("%d saved", len(l.sessions.Get()))),
		gt.WithTextStyle(theme.DimStyle()),
	))
	root.AddChild(header)

	if len(l.sessions.Get()) == 0 {
		root.AddChild(gt.New(
			gt.WithText("No saved sessions."),
			gt.WithTextStyle(theme.DimStyle().Italic()),
			gt.WithFlexGrow(1),
		))
	} else {
		listContainer := gt.New(
			gt.WithDisplay(gt.DisplayFlex),
			gt.WithDirection(gt.Column),
			gt.WithFlexGrow(1),
			gt.WithBorder(gt.BorderSingle),
			gt.WithBorderStyle(theme.BorderStyle()),
			gt.WithPadding(1),
		)
		listEl := app.MountPersistent(l, 0, func() gt.Component {
			return l.list
		})
		listContainer.AddChild(listEl)
		root.AddChild(listContainer)
	}

	root.AddChild(gt.New(
		gt.WithText("↑↓: navigate  ·  Enter: resume  ·  Esc: close"),
		gt.WithTextStyle(theme.DescriptionStyle()),
		gt.WithFlexShrink(0),
	))

	return root
}

func (l *SessionListView) buildItems() []components.ListItem {
	sessions := l.sessions.Get()
	items := make([]components.ListItem, 0, len(sessions)+1)
	for _, session := range sessions {
		items = append(items, components.ListItem{
			ID:    session.ID,
			Label: fmt.Sprintf("%s  ·  %s  ·  %d msgs", session.ID, session.ModelID, session.MessageCount),
			Description: fmt.Sprintf("%s  ·  %s",
				formatDate(session.CreatedAt),
				formatTokens(session.TotalTokens),
			),
		})
	}
	if l.hasMore.Get() && l.onLoadMore != nil {
		items = append(items, components.ListItem{
			ID:    "_load_more",
			Label: "Load more sessions…",
		})
	}
	return items
}

func (l *SessionListView) handleListSelect(item components.ListItem) {
	if item.ID == "_load_more" {
		if l.onLoadMore != nil {
			l.onLoadMore()
		}
		return
	}
	for _, session := range l.sessions.Get() {
		if session.ID == item.ID {
			if l.onSelect != nil {
				l.onSelect(session)
			}
			return
		}
	}
}

var _ gt.KeyListener = (*SessionListView)(nil)
