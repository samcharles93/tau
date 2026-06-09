package views

import (
	"fmt"
	"sort"
	tui "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/internal/tui/components"
)

// SessionTreeFetchLimit is the number of sessions fetched for tree building.
const SessionTreeFetchLimit = 200

// treeNode is an internal node in the session hierarchy used for layout building.
type treeNode struct {
	Summary  tauchat.SessionSummary
	Depth    int
	Expanded bool
	Children []*treeNode
}

// SessionTreeView renders sessions as a collapsible tree by composing the reusable components.Tree.
type SessionTreeView struct {
	sessions      *tui.State[[]tauchat.SessionSummary]
	selected      *tui.State[int]
	filter        *tui.State[string]
	showDetail    *tui.State[bool]
	detailInfo    *tui.State[tauchat.SessionSummary]
	detailView    *SessionInfoView
	visibleItems  *tui.State[[]components.TreeItem]
	treeComponent *components.Tree

	onSelect   func(tauchat.SessionSummary)
	onClose    func()
	onFetchAll func()

	expanded map[string]bool
}

// NewSessionTreeView creates the tree view component.
func NewSessionTreeView(
	sessions *tui.State[[]tauchat.SessionSummary],
	onSelect func(tauchat.SessionSummary),
	onClose func(),
	onFetchAll func(),
) *SessionTreeView {
	detailShow := tui.NewState(false)
	detailInfo := tui.NewState(tauchat.SessionSummary{})
	selected := tui.NewState(0)
	filter := tui.NewState("all")
	visibleItems := tui.NewState([]components.TreeItem{})

	v := &SessionTreeView{
		sessions:     sessions,
		selected:     selected,
		filter:       filter,
		showDetail:   detailShow,
		detailInfo:   detailInfo,
		detailView:   NewSessionInfoView(detailShow, detailInfo),
		visibleItems: visibleItems,
		expanded:     make(map[string]bool),
		onSelect:     onSelect,
		onClose:      onClose,
		onFetchAll:   onFetchAll,
	}

	v.treeComponent = components.NewTree(
		visibleItems,
		selected,
		func(item components.TreeItem) {
			// OnSelect: resume the selected session
			for _, summary := range v.sessions.Get() {
				if summary.ID == item.ID {
					if v.onSelect != nil {
						v.onSelect(summary)
					}
					return
				}
			}
		},
		func(item components.TreeItem) {
			// OnToggle: expand or collapse parent nodes
			v.expanded[item.ID] = !v.expanded[item.ID]
			v.computeVisible()
			v.clampSelection()
		},
		func(item components.TreeItem) {
			// OnDetail: open detail modal
			for _, summary := range v.sessions.Get() {
				if summary.ID == item.ID {
					v.detailInfo.Set(summary)
					v.showDetail.Set(true)
					return
				}
			}
		},
	)

	return v
}

// BindApp wires reactive state.
func (v *SessionTreeView) BindApp(app *tui.App) {
	v.sessions.BindApp(app)
	v.selected.BindApp(app)
	v.filter.BindApp(app)
	v.showDetail.BindApp(app)
	v.detailInfo.BindApp(app)
	v.detailView.BindApp(app)
	v.visibleItems.BindApp(app)
	v.treeComponent.BindApp(app)
}

// KeyMap for the tree view.
func (v *SessionTreeView) KeyMap() tui.KeyMap {
	km := tui.KeyMap{
		tui.On(tui.KeyEscape, func(ke tui.KeyEvent) {
			if v.showDetail.Get() {
				v.showDetail.Set(false)
				return
			}
			if v.onClose != nil {
				v.onClose()
			}
		}),
		tui.On(tui.Rune('f'), func(ke tui.KeyEvent) {
			v.cycleFilter()
		}),
	}

	// Delegate arrow keys and Enter to the Tree component
	km = append(km, v.treeComponent.KeyMap()...)

	return km
}

// --- Tree building ---
func (v *SessionTreeView) buildTree() {
	flat := v.sessions.Get()
	if len(flat) == 0 {
		return
	}

	nodeMap := make(map[string]*treeNode, len(flat))
	for i := range flat {
		nodeMap[flat[i].ID] = &treeNode{
			Summary:  flat[i],
			Expanded: v.expanded[flat[i].ID],
		}
	}

	for _, node := range nodeMap {
		pid := node.Summary.ParentSessionID
		if pid != "" {
			if parent, ok := nodeMap[pid]; ok {
				parent.Children = append(parent.Children, node)
				continue
			}
		}
	}

	roots := make([]*treeNode, 0, len(nodeMap))
	for _, node := range nodeMap {
		if node.Summary.ParentSessionID == "" {
			roots = append(roots, node)
		} else if _, ok := nodeMap[node.Summary.ParentSessionID]; !ok {
			roots = append(roots, node)
		}
	}
	sort.Slice(roots, func(i, j int) bool {
		return roots[i].Summary.CreatedAt.After(roots[j].Summary.CreatedAt)
	})

	var assignDepth func(nodes []*treeNode, depth int)
	assignDepth = func(nodes []*treeNode, depth int) {
		for _, n := range nodes {
			n.Depth = depth
			sort.Slice(n.Children, func(i, j int) bool {
				return n.Children[i].Summary.CreatedAt.After(n.Children[j].Summary.CreatedAt)
			})
			assignDepth(n.Children, depth+1)
		}
	}
	assignDepth(roots, 0)

	var visible []*treeNode
	var walk func(nodes []*treeNode)
	walk = func(nodes []*treeNode) {
		for _, node := range nodes {
			if !v.matchesFilter(node) {
				continue
			}
			visible = append(visible, node)
			if node.Expanded && len(node.Children) > 0 {
				walk(node.Children)
			}
		}
	}
	walk(roots)

	// Convert visible internal nodes to components.TreeItem objects
	items := make([]components.TreeItem, 0, len(visible))
	for _, n := range visible {
		statusIcon, statusLabel := statusDisplay(n.Summary.Status)
		label := fmt.Sprintf("%s  %s %s", truncateString(n.Summary.ID, 32), statusIcon, statusLabel)

		items = append(items, components.TreeItem{
			ID:          n.Summary.ID,
			Label:       label,
			Depth:       n.Depth,
			Expanded:    n.Expanded,
			HasChildren: len(n.Children) > 0,
		})
	}
	v.visibleItems.Set(items)
}

func (v *SessionTreeView) computeVisible() {
	if len(v.sessions.Get()) == 0 {
		v.visibleItems.Set(nil)
		return
	}
	v.buildTree()
}

func (v *SessionTreeView) matchesFilter(node *treeNode) bool {
	switch v.filter.Get() {
	case "active":
		return node.Summary.Status == string(tauchat.ChatSessionIdle) ||
			node.Summary.Status == string(tauchat.ChatSessionStreaming) ||
			node.Summary.Status == string(tauchat.ChatSessionCancelling)
	case "closed":
		return node.Summary.Status == string(tauchat.ChatSessionClosed)
	default:
		return true
	}
}

func (v *SessionTreeView) cycleFilter() {
	switch v.filter.Get() {
	case "all":
		v.filter.Set("active")
	case "active":
		v.filter.Set("closed")
	default:
		v.filter.Set("all")
	}
	v.computeVisible()
	v.clampSelection()
}

// --- Helpers ---
func (v *SessionTreeView) clampSelection() {
	items := v.visibleItems.Get()
	if len(items) == 0 {
		v.selected.Set(0)
		return
	}
	v.selected.Set(clamp(v.selected.Get(), 0, len(items)-1))
}

func statusDisplay(status string) (icon, label string) {
	switch status {
	case string(tauchat.ChatSessionStreaming), string(tauchat.ChatSessionCancelling):
		return "●", status
	case string(tauchat.ChatSessionClosed):
		return "✓", status
	default:
		return "○", status
	}
}

templ (v *SessionTreeView) Render() {
	v.computeVisible()
	<div class="flex-col w-full h-full p-1 gap-1">
		<div class="flex-row justify-between">
			<span textStyle={theme.BrandStyle()}>{"Session Tree"}</span>
			<span textStyle={theme.DimStyle()}>{"[" + v.filter.Get() + "]  f: cycle filter"}</span>
		</div>

		if len(v.visibleItems.Get()) == 0 {
			<span textStyle={theme.DimStyle().Italic()}>{"No sessions match the current filter."}</span>
		} else {
			@v.treeComponent
		}

		<span textStyle={theme.DimStyle().Italic()}>
			{"↑↓: navigate  Enter: expand/select  ←→: collapse/expand  f: filter  Esc: close"}
		</span>
		@v.detailView
	</div>
}
