package views

import (
	"fmt"
	"sort"
	"strings"

	gt "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
)

// SessionTreeFetchLimit is the number of sessions fetched for tree building.
// It's large enough to build a useful tree without pagination for typical
// session counts.
const SessionTreeFetchLimit = 200

// treeNode is an internal node in the session hierarchy.
type treeNode struct {
	Summary  tauchat.SessionSummary
	Depth    int
	Expanded bool
	Children []*treeNode
}

// SessionTreeView renders sessions as a collapsible tree with expand/collapse,
// status icons, and a filter toggle. It is a plain component — the caller
// (ChatPanel) manages alternate-screen entry/exit and renders it fullscreen.
type SessionTreeView struct {
	sessions   *gt.State[[]tauchat.SessionSummary]
	selected   *gt.State[int]
	filter     *gt.State[string]
	showDetail *gt.State[bool]
	detailInfo *gt.State[tauchat.SessionSummary]
	detailView *SessionInfoView

	onSelect   func(tauchat.SessionSummary)
	onClose    func()
	onFetchAll func()

	// Internal tree state — mutated only from key handlers inside the go-tui
	// event loop. computeVisible is called after every mutation.
	expanded map[string]bool
	visible  []*treeNode
}

// NewSessionTreeView creates the tree view component.
func NewSessionTreeView(
	sessions *gt.State[[]tauchat.SessionSummary],
	onSelect func(tauchat.SessionSummary),
	onClose func(),
	onFetchAll func(),
) *SessionTreeView {
	detailShow := gt.NewState(false)
	detailInfo := gt.NewState(tauchat.SessionSummary{})
	v := &SessionTreeView{
		sessions:   sessions,
		selected:   gt.NewState(0),
		filter:     gt.NewState("all"),
		showDetail: detailShow,
		detailInfo: detailInfo,
		detailView: NewSessionInfoView(detailShow, detailInfo),
		expanded:   make(map[string]bool),
		onSelect:   onSelect,
		onClose:    onClose,
		onFetchAll: onFetchAll,
	}
	return v
}

// BindApp wires reactive state and the detail view.
func (v *SessionTreeView) BindApp(app *gt.App) {
	v.sessions.BindApp(app)
	v.selected.BindApp(app)
	v.filter.BindApp(app)
	v.showDetail.BindApp(app)
	v.detailInfo.BindApp(app)
	v.detailView.BindApp(app)
}

// Render returns the fullscreen tree element. ChatPanel calls this when the
// tree view is active and the app is in alternate-screen mode.
func (v *SessionTreeView) Render(app *gt.App) *gt.Element {
	root := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithWidthPercent(100),
		gt.WithHeightPercent(100),
		gt.WithPadding(1),
		gt.WithGap(1),
	)

	// Header with title and filter indicator.
	header := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Row),
		gt.WithJustify(gt.JustifySpaceBetween),
	)
	header.AddChild(gt.New(
		gt.WithText("Session Tree"),
		gt.WithTextStyle(theme.BrandStyle()),
	))
	header.AddChild(gt.New(
		gt.WithText("["+v.filter.Get()+"]  f: cycle filter"),
		gt.WithTextStyle(theme.DimStyle()),
	))
	root.AddChild(header)

	// Build tree and compute visible nodes.
	v.computeVisible()

	if len(v.visible) == 0 {
		root.AddChild(gt.New(
			gt.WithText("No sessions match the current filter."),
			gt.WithTextStyle(theme.DimStyle().Italic()),
		))
	} else {
		body := gt.New(
			gt.WithDisplay(gt.DisplayFlex),
			gt.WithDirection(gt.Column),
			gt.WithGap(0),
			gt.WithScrollable(gt.ScrollVertical),
			gt.WithFlexGrow(1),
		)

		sel := clamp(v.selected.Get(), 0, len(v.visible)-1)
		for i, node := range v.visible {
			body.AddChild(v.renderNode(node, i == sel))
		}
		root.AddChild(body)
	}

	// Footer with keybinding hints.
	root.AddChild(gt.New(
		gt.WithText("↑↓: navigate  Enter: expand/select  ←→: collapse/expand  f: filter  Esc: close"),
		gt.WithTextStyle(theme.DimStyle().Italic()),
	))

	// Render the detail popup (SessionInfoView modal).
	detailEl := v.detailView.Render(app)

	wrapper := gt.New(gt.WithOverlay(true))
	wrapper.AddChild(root)
	wrapper.AddChild(detailEl)
	return wrapper
}

func (v *SessionTreeView) renderNode(node *treeNode, selected bool) *gt.Element {
	row := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Row),
		gt.WithGap(1),
		gt.WithPaddingTRBL(0, 0, 0, 0),
	)

	indent := strings.Repeat("  ", node.Depth)
	arrow := "  "
	if len(node.Children) > 0 {
		if node.Expanded {
			arrow = "▼ "
		} else {
			arrow = "▶ "
		}
	}

	prefix := indent + arrow
	statusIcon, statusLabel := statusDisplay(node.Summary.Status)
	id := truncateString(node.Summary.ID, 32)

	line := fmt.Sprintf("%s%s  %s %s",
		prefix,
		id,
		statusIcon,
		statusLabel,
	)

	style := theme.DimStyle()
	if selected {
		style = theme.BrandStyle()
	}

	row.AddChild(gt.New(
		gt.WithText(line),
		gt.WithTextStyle(style),
		gt.WithTruncate(true),
	))
	return row
}

// KeyMap for the tree view.
func (v *SessionTreeView) KeyMap() gt.KeyMap {
	return gt.KeyMap{
		gt.On(gt.KeyEscape, func(ke gt.KeyEvent) {
			if v.showDetail.Get() {
				v.showDetail.Set(false)
				return
			}
			if v.onClose != nil {
				v.onClose()
			}
		}),
		gt.On(gt.KeyUp, func(ke gt.KeyEvent) {
			if len(v.visible) == 0 {
				return
			}
			idx := v.selected.Get() - 1
			if idx < 0 {
				idx = len(v.visible) - 1
			}
			v.selected.Set(idx)
		}),
		gt.On(gt.KeyDown, func(ke gt.KeyEvent) {
			if len(v.visible) == 0 {
				return
			}
			idx := v.selected.Get() + 1
			if idx >= len(v.visible) {
				idx = 0
			}
			v.selected.Set(idx)
		}),
		gt.On(gt.KeyEnter, func(ke gt.KeyEvent) {
			node := v.selectedNode()
			if node == nil {
				return
			}
			if len(node.Children) > 0 {
				node.Expanded = !node.Expanded
				v.expanded[node.Summary.ID] = node.Expanded
				v.computeVisible()
				v.clampSelection()
			} else if v.onSelect != nil {
				v.onSelect(node.Summary)
			}
		}),
		gt.On(gt.KeyRight, func(ke gt.KeyEvent) {
			node := v.selectedNode()
			if node == nil {
				return
			}
			if len(node.Children) > 0 && !node.Expanded {
				node.Expanded = true
				v.expanded[node.Summary.ID] = true
				v.computeVisible()
			} else {
				v.detailInfo.Set(node.Summary)
				v.showDetail.Set(true)
			}
		}),
		gt.On(gt.KeyLeft, func(ke gt.KeyEvent) {
			node := v.selectedNode()
			if node == nil {
				return
			}
			if len(node.Children) > 0 && node.Expanded {
				node.Expanded = false
				v.expanded[node.Summary.ID] = false
				v.computeVisible()
				v.clampSelection()
			}
		}),
		gt.On(gt.Rune('f'), func(ke gt.KeyEvent) {
			v.cycleFilter()
		}),
	}
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

	v.visible = nil
	v.buildVisibleFrom(roots)
}

func (v *SessionTreeView) buildVisibleFrom(roots []*treeNode) {
	var walk func(nodes []*treeNode)
	walk = func(nodes []*treeNode) {
		for _, node := range nodes {
			if !v.matchesFilter(node) {
				continue
			}
			v.visible = append(v.visible, node)
			if node.Expanded && len(node.Children) > 0 {
				walk(node.Children)
			}
		}
	}
	walk(roots)
}

func (v *SessionTreeView) computeVisible() {
	if len(v.sessions.Get()) == 0 {
		v.visible = nil
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

func (v *SessionTreeView) selectedNode() *treeNode {
	if len(v.visible) == 0 {
		return nil
	}
	idx := clamp(v.selected.Get(), 0, len(v.visible)-1)
	return v.visible[idx]
}

func (v *SessionTreeView) clampSelection() {
	if len(v.visible) == 0 {
		v.selected.Set(0)
		return
	}
	v.selected.Set(clamp(v.selected.Get(), 0, len(v.visible)-1))
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
