package tui2

import (
	"context"
	"fmt"
	"image/color"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"

	"github.com/google/uuid"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/metrics"
	"github.com/samcharles93/tau/internal/providerui"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/internal/tui/notify"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// --- model ------------------------------------------------------------------

// model is the root Bubbletea model for the chat TUI.
type model struct {
	ctx     context.Context
	runtime tauchat.ChatRuntime
	chatSub *eventbus.Subscriber[tauchat.ChatEvent]

	sessionID string
	modelName string
	provider  string

	// Terminal dimensions (updated on WindowSizeMsg).
	width  int
	height int

	// maxViewportHeight is the most the message viewport may grow to (leaving
	// room for the separator/input/status bar). tui2 owns the full terminal via
	// alt-screen, so the viewport fills whatever vertical space remains after
	// reserving fixed UI chrome.
	maxViewportHeight int

	// Conversation state — stored as raw content string fed to the viewport.
	viewport   viewport.Model
	streaming  string // current streaming text delta
	reasoning  string // current reasoning delta
	inResponse bool   // true while a response is in progress

	// Tool state.
	tools           []toolState           // active tool calls in display order
	committedGroups []*committedToolGroup // multi-call batches already in scrollback, still foldable — see committedToolGroup

	// Input state. input may contain embedded '\n' (Shift+Enter/Ctrl+J
	// inserts a newline rather than submitting) — inputCursor is a rune
	// index into it, 0..len([]rune(input)). Editing/navigation mirror
	// pkg/taui/lineinput.go so both frontends behave identically.
	input       string
	inputCursor int
	history     []string // submitted inputs for up/down recall
	historyIdx  int      // -1 = not navigating; 0..len(history) = navigating
	draftInput  string   // stashed input on first history recall; restored at idx == len(history)

	// focused reports whether the terminal window currently has focus,
	// tracked via tea.FocusMsg/tea.BlurMsg (requires View.ReportFocus).
	// Defaults to true so a terminal that never reports focus (no
	// ReportFocus support) doesn't spuriously suppress notifications.
	focused bool

	// Completion dropdown state. compToken is the last token the dropdown was
	// computed against — when it changes (the user typed/deleted a
	// character), compSelected resets to the top-ranked match rather than
	// pointing at whatever now sits at that index.
	compSelected int
	compToken    string

	// Viewport content — rendered lines, built incrementally.
	renderedLines []string

	// messageRanges records which span of renderedLines each ChatMessage's
	// rendered lines occupy, so a click can be resolved to "which message"
	// (see messageAtRow) rather than just "which line" — mirrors
	// committedToolGroup's lineIdx/lineCount, for whole messages instead of
	// tool boxes. Only messages with a real ID (see chat.ChatMessage.ID)
	// get an entry; entries must be kept in sync by anything that mutates
	// renderedLines in place (see spliceCommittedGroup).
	messageRanges []messageLineRange

	// lastAssistantText is the raw (unstyled) content of the most recent
	// assistant message, kept separately from renderedLines because those
	// are lipgloss-styled — the ANSI escape codes wrapping the content mean
	// a literal substring/prefix match against renderedLines can never
	// reliably succeed (this is what /copy needs; scanning styled output
	// doesn't work).
	lastAssistantText string

	// Status / transient.
	statusText        string       // one-line status bar (model @ provider only)
	notification      string       // transient notification banner, shown above the input area
	notificationLevel notify.Level // drives the banner's color (info/warn/error)
	notificationGen   int          // bumped every time notification is set; guards clear race

	// Model / provider state (populated by run.go).
	availableModels []tauchat.ChatModelRef
	refresh         func(context.Context) ([]tauchat.ChatModelRef, error)
	showReasoning   bool
	reasoningEffort string
	ctxWindow       int // context window size for % display

	// Extension commands (populated from ExtensionCommandsChangedEvent).
	extensionCommands map[string]tauchat.ExtensionCommand

	// Usage tracking.
	usage *metrics.UsageTracker

	// Configuration.
	webURL string
	debug  bool

	// Session management.
	sessionSummaries []tauchat.SessionSummary

	// Turn management.
	turnQueue    []string // queued prompts behind a running turn
	lastSubmit   time.Time
	spinnerFrame int // frame index for working indicator animation

	// turnStartedAt is when the current response began
	// (ChatResponseStartedEvent), driving the live elapsed clock in the
	// working indicator; turnSeed varies the opening thinking-verb per turn so
	// two consecutive turns don't repeat the same word.
	turnStartedAt time.Time
	turnSeed      int64

	// Markdown rendering (P3 enhancement) — reusable glamour term renderers
	// keyed by terminal width so resize doesn't allocate a new renderer for
	// the same width. Each converts assistant messages from markdown to
	// ANSI-styled output with syntax-highlighted code blocks. Only applied
	// on finalized messages (ChatResponseCompletedEvent), never during
	// streaming — mid-token markdown re-parsing is unsafe.
	mdCache map[int]*glamour.TermRenderer

	// pendingQuit is the time of the last unanswered Ctrl+C (idle, nothing to
	// cancel) — a second Ctrl+C within quitConfirmWindow confirms the quit,
	// mirroring internal/tui/inline_chat.go's double-tap guard.
	pendingQuit time.Time

	// Steering.
	steering bool

	// Interactive prompts — see formPrompt in prompt.go for the shared
	// widget both the agent-question flow and local UI flows (e.g.
	// providerLogin's Enterprise-domain prompt) build and present through
	// presentPrompt/presentLocalPrompt.
	activePrompt *formPrompt
	promptQueue  []*formPrompt

	// contextMenu is the currently open right-click menu, or nil if none is
	// open — mirrors activePrompt's nil-sentinel idiom.
	contextMenu *contextMenu

	// Plugin views.
	panels map[string]pluginPanel

	// Bash mode.
	bashRunning bool
	bashCallID  string

	// Phase 1: tool focus/expansion navigation (keyboard and mouse).
	focusedTool int    // index into m.tools for keyboard nav (-1 = none)
	expandedID  string // tool ID currently expanded ("" = none)

	// toolGroupCollapsed collapses the live tool-call group to a one-line
	// summary, saving vertical space when there are many calls. Toggled by
	// clicking on the live group header / border area.
	toolGroupCollapsed bool

	// toolCallsDefaultCollapsed is the initial value for toolGroupCollapsed
	// at the start of each turn, driven by UI.ToolCallsDefaultCollapsed in
	// the global/project config.
	toolCallsDefaultCollapsed bool

	// autoFollow keeps the viewport pinned to the bottom as new content
	// arrives. Cleared when the user scrolls up (they want to read
	// history), and restored once they scroll back to the bottom or submit
	// a new prompt.
	autoFollow bool

	// Mouse drag text selection (see handleMousePress/Drag/Release, and
	// selectionState below). Built from scratch on raw press/motion/release
	// events — bubbletea v2 and bubbles have no selection primitive to
	// reuse (verified against clipboard.go/mouse.go). Four regions each
	// get their own selectionState — viewport (whole logical lines), input
	// box (rune-precise), status bar (column-precise), and the live tool-call
	// box (whole lines) — but share one state machine and one finalize path
	// (finalizeSelection) rather than four hand-rolled ones. Copying is an
	// explicit right-click action after selection.
	viewportSel selectionState
	inputSel    selectionState
	statusSel   selectionState
	toolsSel    selectionState

	// dragRegion records which UI region a mouse gesture started in, so a
	// drag/release that moves outside that region (or outside the terminal
	// entirely, on emulators that clamp coordinates) still routes to the
	// right selection — press decides the region once; drag/release just
	// follow it.
	dragRegion dragRegion
}

// dragRegion identifies which UI region a mouse press/drag/release is
// operating on, so a single input event stream can drive three independent
// selectionStates (viewport, input box, status bar) without them
// interfering.
type dragRegion int

const (
	dragNone dragRegion = iota
	dragViewport
	dragInput
	dragStatus
	dragTools
)

// selectionState is a press→drag→release text-selection gesture over some
// region's content, addressed by a single ordered integer position — a
// line index, a rune index, a column, whatever that region's own
// coordinate space is. Any UI region gets full drag-to-select behavior (via
// finalizeSelection) just by driving one of these plus a small
// position-mapping function and a text-extraction function, instead of
// hand-rolling its own anchor/cursor/dragging fields and copy logic —
// see viewportSel/inputSel/statusSel for the three current regions.
type selectionState struct {
	anchor   int // -1 = no selection
	cursor   int
	dragging bool
}

func newSelectionState() selectionState {
	return selectionState{anchor: -1, cursor: -1}
}

// clear drops any in-progress or finalized selection.
func (s *selectionState) clear() {
	s.anchor, s.cursor, s.dragging = -1, -1, false
}

// armed reports whether a press has staked an anchor (regardless of
// whether a real drag has extended it yet).
func (s *selectionState) armed() bool {
	return s.anchor >= 0
}

// press arms the anchor at pos, ready to become a drag.
func (s *selectionState) press(pos int) {
	s.anchor, s.cursor, s.dragging = pos, pos, false
}

// drag extends the selection to pos and marks the gesture as a real drag
// (as opposed to a plain click with no movement).
func (s *selectionState) drag(pos int) {
	s.dragging = true
	s.cursor = pos
}

// bounds returns the ordered [lo,hi] range, and false if there's no
// selection at all. Callers interpret lo/hi in their own domain — the
// viewport treats them as inclusive line indices, input/status as a
// half-open position range — selectionState itself is domain-agnostic.
func (s *selectionState) bounds() (lo, hi int, ok bool) {
	if s.anchor < 0 || s.cursor < 0 {
		return 0, 0, false
	}
	lo, hi = s.anchor, s.cursor
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi, true
}

// contextMenuTarget identifies what kind of element a context menu was
// opened against — tool calls have two distinct on-screen forms (see
// contextMenuTargetTool vs contextMenuTargetToolRow), because a live,
// uncommitted tool box and a committed group's unfolded per-tool row are
// resolved through completely different hit-testing paths.
type contextMenuTarget int

const (
	contextMenuTargetNone    contextMenuTarget = iota
	contextMenuTargetTool                      // a live geom.toolBoxes entry, or a whole (folded or unfolded) committed group
	contextMenuTargetToolRow                   // one row inside an unfolded committed group
	contextMenuTargetMessage
)

// contextMenuAction identifies what a menu item does when activated.
type contextMenuAction int

const (
	contextMenuActionCopy contextMenuAction = iota
	contextMenuActionToggleExpand
)

// contextMenuItem is one selectable row in an open context menu. label is
// computed at build time (e.g. "Expand" vs "Collapse" reflects current
// state), not recomputed on every render, so the menu's contents stay
// stable while it's open even if something else changes state underneath.
type contextMenuItem struct {
	label  string
	action contextMenuAction
}

// contextMenu is the state of an open right-click menu. A nil *contextMenu
// on model means no menu is open — mirrors activePrompt's nil-sentinel
// idiom. x/y are the raw click position; positioning/clamping onto the
// screen happens at render/hit-test time (see clampContextMenuPosition),
// not here, so this struct stays purely "what was clicked and what can be
// done about it."
type contextMenu struct {
	target   contextMenuTarget
	targetID string
	x, y     int
	items    []contextMenuItem
	selected int
}

type toolState struct {
	id     string
	name   string
	args   string
	result string
	status string // "pending", "running", "done", "error"

	// startedAt is when the call first appeared, and elapsed is frozen at
	// completion so a settled row keeps showing how long it took rather than
	// ticking on (or dropping to zero) after the turn ends.
	startedAt time.Time
	elapsed   time.Duration

	// Phase 1: live output streaming, per-tool spinner, and expand/collapse
	// interaction.
	tailLines  []string // live output streaming, max tailCap lines
	tailCap    int      // max tail lines to show (default 6)
	spinnerIdx int      // per-tool spinner animation frame (bumped on tickMsg)
	expanded   bool     // user has clicked Enter to expand this tool
}

// committedToolGroup is a multi-tool-call batch already committed to
// permanent scrollback (see commitToolGroup) that can still be
// unfolded/refolded and drilled into afterward, mirroring the live group's
// own two-level accordion (group -> per-tool rows -> one row's full output)
// instead of freezing forever into a single flat summary line the moment it
// scrolls into history. A lone committed tool call doesn't need one of
// these — it's already a full, permanently-detailed box (see
// commitToolGroup) with nothing left to toggle.
type committedToolGroup struct {
	tools      []toolState
	expanded   bool   // group unfolded into per-tool rows
	expandedID string // which row, if any, is further expanded to full output

	// lineIdx/lineCount record where in m.renderedLines this group's current
	// rendering lives, so a click can find it (toggleCommittedToolAtLine)
	// and a toggle can splice its replacement in without disturbing any
	// other content beyond a fixed line-count shift (spliceCommittedGroup).
	lineIdx   int
	lineCount int
}

// committedGroupKey identifies a committed tool-call group by its ordered
// tool-call IDs. Real tool calls keep their persisted call ID across an
// applySnapshot rebuild; bash-history-reconstructed entries key off the
// message's index instead (see applySnapshot) since they have no real call
// ID — either way the key is stable across rebuilds, which is what lets
// fold/expand state survive a snapshot instead of resetting to folded every
// time the user submits another prompt.
func committedGroupKey(tools []toolState) string {
	ids := make([]string, len(tools))
	for i, t := range tools {
		ids[i] = t.id
	}
	return strings.Join(ids, "\x00")
}

type pluginPanel struct {
	id      string
	title   string
	content string
}

// layoutGeometry records where interactive regions land in the final
// rendered view. computeLayout is the single source of truth for both
// View()'s rendering and handleMousePress/Drag's hit-testing, so the two
// can never drift apart.
type layoutGeometry struct {
	chromeStr   string
	panelSepStr string
	topPadding  int

	viewportStartY int
	viewportEndY   int
	toolBoxes      []toolBoxGeometry
	toolsStartY    int
	toolsEndY      int

	inputStartY int
	inputEndY   int
	statusY     int
}

// toolBoxGeometry is the inclusive [startY, endY] row range (0-based, in
// final rendered-view coordinates) that a tool box occupies.
type toolBoxGeometry struct {
	id     string
	startY int
	endY   int
}

// messageLineRange records the [startLine, endLine) span (half-open,
// indices into m.renderedLines at the time of recording) that one
// ChatMessage's rendered lines occupy. Unlike toolBoxGeometry, this indexes
// m.renderedLines directly rather than final screen rows — logicalLineAtRow
// already maps a screen row to a renderedLines index, so no separate
// box-relative-to-absolute translation is needed in computeLayout.
type messageLineRange struct {
	id        string
	content   string // raw (unstyled) message content, for Copy — renderedLines is lipgloss-styled, same reason lastAssistantText is kept separately
	startLine int
	endLine   int
}

// --- constructor -----------------------------------------------------------

func newModel(
	ctx context.Context,
	runtime tauchat.ChatRuntime,
	chatSub *eventbus.Subscriber[tauchat.ChatEvent],
	sessionID, modelName, provider string,
	availableModels []tauchat.ChatModelRef,
	refresh func(context.Context) ([]tauchat.ChatModelRef, error),
	showReasoning bool,
	reasoningEffort string,
	toolCallsDefaultCollapsed bool,
	usage *metrics.UsageTracker,
	webURL string,
	debug bool,
) *model {
	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	vp.SoftWrap = true

	// Glamour markdown renderer cache, keyed by terminal width.
	mdCache := map[int]*glamour.TermRenderer{}
	// Pre-populate the default width so the first finalized message can
	// render immediately without a WindowSizeMsg having arrived yet.
	ensureMDRenderer(mdCache, 80)

	return &model{
		ctx:                       ctx,
		runtime:                   runtime,
		chatSub:                   chatSub,
		sessionID:                 sessionID,
		modelName:                 modelName,
		provider:                  provider,
		viewport:                  vp,
		historyIdx:                -1,
		focused:                   true,
		focusedTool:               -1,
		autoFollow:                true,
		viewportSel:               newSelectionState(),
		inputSel:                  newSelectionState(),
		statusSel:                 newSelectionState(),
		toolsSel:                  newSelectionState(),
		availableModels:           availableModels,
		refresh:                   refresh,
		showReasoning:             showReasoning,
		reasoningEffort:           reasoningEffort,
		toolCallsDefaultCollapsed: toolCallsDefaultCollapsed,
		toolGroupCollapsed:        toolCallsDefaultCollapsed,
		usage:                     usage,
		webURL:                    webURL,
		debug:                     debug,
		extensionCommands:         make(map[string]tauchat.ExtensionCommand),
		panels:                    make(map[string]pluginPanel),
		mdCache:                   mdCache,
	}
}

// --- spinner ---------------------------------------------------------------

// spinnerFrames are the Unicode braille dots cycled through at 80ms —
// mirrors the legacy TUI's spinnerLoop (internal/tui/inline_chat.go).
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinTick returns a tea.Cmd that fires a tickMsg after 80ms.
func spinTick() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg{t: t}
	})
}

// --- Bubbletea model interface ---------------------------------------------

func (m *model) Init() tea.Cmd {
	return tea.Batch(
		readNextEvent(m.chatSub),
		func() tea.Msg { return startupMsg{m.sessionID, m.modelName, m.provider} },
	)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle quit request from the program (Ctrl+C signal, etc.).
	if _, ok := msg.(tea.QuitMsg); ok {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.SetWidth(msg.Width)
		// Alt-screen layout: the viewport shares the terminal with the fixed
		// chrome (separator, padded input box, status bar). The actual space
		// left for the viewport is recomputed every frame in View(); here we
		// just store an upper-bound estimate for tests and sanity checks.
		inputLines := max(strings.Count(m.input, "\n")+1, 1)
		reserved := 1 + inputLines + 4 + 1 // separator + input box + status
		m.maxViewportHeight = max(msg.Height-reserved, 4)
		m.viewport.SetHeight(m.maxViewportHeight)
		// Rebuild the glamour renderer with the new terminal width so
		// subsequent finalized messages don't wrap at a stale column.
		ensureMDRenderer(m.mdCache, msg.Width)
		return m, nil

	case tea.MouseMsg:
		mev := msg.Mouse()
		switch mev.Button {
		case tea.MouseWheelUp:
			m.viewport.ScrollUp(3)
			m.autoFollow = false
		case tea.MouseWheelDown:
			m.viewport.ScrollDown(3)
			if m.viewport.AtBottom() {
				m.autoFollow = true
			}
		case tea.MouseLeft:
			switch msg.(type) {
			case tea.MouseClickMsg:
				return m, m.handleMousePress(mev.X, mev.Y)
			case tea.MouseMotionMsg:
				m.handleMouseDrag(mev.X, mev.Y)
			case tea.MouseReleaseMsg:
				return m, m.handleMouseRelease()
			}
		case tea.MouseRight:
			if _, ok := msg.(tea.MouseClickMsg); ok {
				// Right-click keeps its existing "copy my selection"
				// behavior; only when nothing is selected does it fall
				// through to opening a context menu at the click.
				if cmd := m.copyActiveSelection(); cmd != nil {
					return m, cmd
				}
				m.openContextMenuAt(mev.X, mev.Y)
			}
		}
		return m, nil

	case tea.PasteMsg:
		m.insertAtCursor(msg.Content)
		return m, nil

	case tea.FocusMsg:
		m.focused = true
		return m, nil

	case tea.BlurMsg:
		m.focused = false
		return m, nil

	case tea.KeyPressMsg:
		return m, m.handleKey(msg)

	case tea.KeyReleaseMsg:
		return m, noopCmd

	case tea.KeyMsg:
		// Dispatch known key types from the KeyMsg interface.
		if kp, ok := msg.(tea.KeyPressMsg); ok {
			return m, m.handleKey(kp)
		}
		return m, nil

	case chatEventMsg:
		var cmds []tea.Cmd
		cmds = append(cmds, readNextEvent(m.chatSub)) // re-arm
		if eventCmd := m.handleChatEvent(msg.event); eventCmd != nil {
			cmds = append(cmds, eventCmd)
		}
		return m, tea.Batch(cmds...)

	case chatEventsClosedMsg:
		m.notification = "event stream closed — exiting"
		return m, tea.Quit

	case clearNotificationMsg:
		if msg.gen == m.notificationGen {
			m.notification = ""
			m.notificationLevel = notify.LevelInfo
		}
		return m, nil

	case sendResultMsg:
		if msg.err != nil {
			m.inResponse = false
			m.notification = fmt.Sprintf("send failed: %v", msg.err)
		}
		return m, nil

	case bashSendResultMsg:
		if msg.err != nil {
			m.bashRunning = false
			m.bashCallID = ""
			m.notification = fmt.Sprintf("bash command failed: %v", msg.err)
		}
		return m, nil

	case refreshResultMsg:
		if msg.err != nil {
			return m, m.setNotification("refresh failed: " + msg.err.Error())
		}
		m.availableModels = msg.models
		return m, m.setNotification(fmt.Sprintf("refreshed: %d models available", len(msg.models)))

	case providerToggleResultMsg:
		var line string
		if msg.err != nil {
			line = fmt.Sprintf("%s %s, but model refresh failed: %s", msg.displayName, msg.action, msg.err.Error())
		} else {
			m.availableModels = msg.models
			line = fmt.Sprintf("%s %s, models available: %d", msg.displayName, msg.action, len(msg.models))
			if msg.warning != "" {
				line += " (" + msg.warning + ")"
			}
		}
		m.appendMessage("system", line)
		return m, nil

	case providerLoginStartedMsg:
		if msg.err != nil {
			m.appendMessage("system", providerui.FailureMessage(msg.displayName, msg.err))
			return m, nil
		}
		code := msg.session.DeviceCode
		codeText := strings.TrimSpace(code.UserCode)
		codeCopied := false
		var copyCmd tea.Cmd
		if codeText != "" {
			if _, ok := termkit.OSC52Copy(codeText); ok {
				codeCopied = true
				copyCmd = tea.SetClipboard(codeText)
			}
		}
		m.appendMessage("system", providerui.DeviceCodeMessage(msg.displayName, code, msg.browserOpened, codeCopied))
		return m, tea.Batch(copyCmd, m.providerLoginPoll(msg.providerID, msg.displayName, msg.session))

	case providerLoginResultMsg:
		if msg.err != nil {
			m.appendMessage("system", providerui.FailureMessage(msg.displayName, msg.err))
			return m, nil
		}
		if len(msg.models) > 0 {
			m.availableModels = msg.models
			m.appendMessage("system", fmt.Sprintf("%s enabled, models available: %d", msg.displayName, len(msg.models)))
		} else {
			m.appendMessage("system", fmt.Sprintf("%s enabled", msg.displayName))
		}
		return m, nil

	case tickMsg:
		m.spinnerFrame++
		// Phase 1: per-tool spinner animation — bump spinnerIdx for every
		// running tool so each tool row animates independently.
		for i := range m.tools {
			if m.tools[i].status == "running" {
				m.tools[i].spinnerIdx++
			}
		}
		if m.inResponse {
			return m, spinTick()
		}
		return m, nil

	case startupMsg:
		m.sessionID = msg.sessionID
		m.modelName = msg.modelName
		m.provider = msg.provider
		return m, nil

	default:
		return m, nil
	}
}

func (m *model) View() tea.View {
	geom := m.computeLayout()

	var sb strings.Builder
	if geom.panelSepStr != "" {
		sb.WriteString(geom.panelSepStr)
	}
	if geom.topPadding > 0 {
		sb.WriteString(strings.Repeat("\n", geom.topPadding))
	}
	sb.WriteString(m.viewport.View())
	sb.WriteString("\n")
	sb.WriteString(geom.chromeStr)

	base := sb.String()
	if m.contextMenu != nil {
		base = m.compositeContextMenu(base)
	}

	v := tea.NewView(base)
	// AltScreen owns the full terminal so we can use guaranteed screen real
	// estate for tool boxes, expansion panels, and flicker-free output.
	v.AltScreen = true
	// Requests terminal focus reporting so we only fire a desktop
	// notification (see handleChatEvent's ChatResponseCompletedEvent case)
	// when the user has actually looked away — matches the legacy
	// engine.Focused() gate in internal/tui/inline_events.go.
	v.ReportFocus = true
	// CellMotion enables click, release, and wheel events (plus drag) without
	// the constant hover-motion stream AllMotion would add, and is better
	// supported across terminals — all we need for scroll + click-to-expand.
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// compositeContextMenu overlays the open context menu on top of base using a
// lipgloss Compositor — tui2's only (x,y)-stamping compositing path, used
// nowhere else, since every other piece of chrome is flow-laid-out below
// the viewport rather than floating on top of arbitrary content. Compositing
// happens at the decoded-cell level (via lipgloss.Layer's underlying
// uv.StyledString), so this never corrupts either base's or menuStr's ANSI
// regardless of what's already styled underneath.
//
// This must go through a Compositor, not bare Canvas.Compose(layer) calls —
// Canvas.Compose(drawer) always calls drawer.Draw(canvas, canvas.Bounds()),
// and a bare Layer.Draw ignores its own X/Y and just stamps its content
// starting at whatever area it's handed, filling that entire area (blanking
// anything beyond its own content). A Layer's X/Y/Z only get honored once
// it's inside a Compositor, which precomputes each layer's true absolute
// bounds before drawing it.
func (m *model) compositeContextMenu(base string) string {
	menuStr := renderContextMenu(m.contextMenu.items, m.contextMenu.selected)
	mx, my := m.clampContextMenuPosition(menuStr)

	// Compositor.Render() would auto-size the canvas to the union of layer
	// bounds, which could come up short of the real terminal size if base's
	// widest line is narrower than m.width — pin it explicitly instead, so
	// the composited frame always matches what bubbletea expects.
	compositor := lipgloss.NewCompositor(
		lipgloss.NewLayer(base).X(0).Y(0),
		lipgloss.NewLayer(menuStr).X(mx).Y(my).Z(1),
	)
	canvas := lipgloss.NewCanvas(m.width, m.height)
	canvas.Compose(compositor)
	return canvas.Render()
}

// clampContextMenuPosition returns where the open menu should actually be
// drawn: anchored at its raw click position (m.contextMenu.x/y), flipped to
// the opposite corner when it would overflow the right or bottom edge, then
// hard-clamped into the terminal bounds as a safety net for a menu wider or
// taller than the terminal itself (which the flip alone doesn't guarantee).
// Shared by compositeContextMenu (render) and handleContextMenuClick
// (click-away hit-test) so the two can never disagree about where the menu
// actually is — same rationale as computeLayout being the single source of
// truth for the rest of the screen.
func (m *model) clampContextMenuPosition(menuStr string) (x, y int) {
	mw := lipgloss.Width(menuStr)
	mh := lipgloss.Height(menuStr)
	x, y = m.contextMenu.x, m.contextMenu.y

	if x+mw > m.width {
		x = m.contextMenu.x - mw
	}
	if y+mh > m.height {
		y = m.contextMenu.y - mh + 1
	}
	x = max(0, min(x, max(0, m.width-mw)))
	y = max(0, min(y, max(0, m.height-mh)))
	return x, y
}

// renderContextMenu renders an open context menu's items as a bordered box,
// mirroring renderPrompt/renderCompletions' signature shape. The selected
// row gets the same "▶ " chevron + bold-foreground convention as the
// completions dropdown, for a consistent selection idiom across tui2's
// keyboard-navigable lists.
func renderContextMenu(items []contextMenuItem, selected int) string {
	lines := make([]string, len(items))
	for i, item := range items {
		if i == selected {
			lines[i] = "▶ " + contextMenuSelectedStyle.Render(item.label)
		} else {
			lines[i] = "  " + item.label
		}
	}
	return contextMenuStyle.Render(strings.Join(lines, "\n"))
}

// computeLayout renders every non-viewport UI region, decides how much
// vertical space the viewport gets, and records the on-screen row range of
// each tool box. It is called once by View() (for rendering) and again by
// handleMousePress/handleMouseDrag (for hit-testing) — keeping both derived
// from the same function is what guarantees they never disagree.
func (m *model) computeLayout() layoutGeometry {
	var g layoutGeometry

	// 1. Plugin panel.
	var activePanelStr string
	if p := m.activePanel(); p != nil {
		var psb strings.Builder
		psb.WriteString(panelStyle.Render("┌─ " + p.title + " ─┐"))
		psb.WriteString("\n")
		psb.WriteString(p.content)
		psb.WriteString("\n")
		psb.WriteString(panelStyle.Render("└" + strings.Repeat("─", min(m.width, 40)) + "┘"))
		activePanelStr = psb.String()
	}

	// 2. Live response content is rendered inside the viewport so it occupies
	// the same bottom-aligned chat pane as finalized messages.
	visibleReasoning := m.reasoning != "" && m.showReasoning
	viewportLines := m.viewportLinesForView(visibleReasoning)
	m.viewport.SetContentLines(viewportLines)

	// 3. Live tool-call group (uncommitted — see flushToolGroup). Rendered as
	// one box: a lone tool keeps its full box, multiple concurrent/sequential
	// calls render as a single group so a long tool-calling burst doesn't
	// grow unbounded on screen while it's still running.
	toolsStr, toolRows := m.renderToolGroup()

	// 4. Interactive prompt (modal).
	var promptStr string
	if m.activePrompt != nil {
		promptStr = renderPrompt(m.activePrompt, m.width)
	}

	// 5. Completion dropdown.
	var compStr string
	if rows, _ := m.completionRows(); len(rows) > 0 {
		selected := m.compSelected
		if selected < 0 || selected >= len(rows) {
			selected = 0
		}
		compStr = renderCompletions(rows, selected, m.width)
	}

	// 6. Notification banner — a fixed notifyReservedLines-tall area is
	// always reserved directly above the separator/input, even when
	// there's nothing to show, matching how Claude Code's own status area
	// never resizes. Previously this only occupied space while
	// m.notification was non-empty, so the viewport visibly grew and
	// shrank by that height every time a notification appeared or cleared
	// ("pushing text up and dropping it back down"). Width-wrapped via
	// lipgloss (so a message that needs it still gets multiple lines, up
	// to the reserved height — see notifyStyleForLevel's caller), then
	// padded/clipped to exactly notifyReservedLines so the reserved height
	// truly never varies.
	notifyWidth := m.width
	if notifyWidth <= 0 {
		notifyWidth = 80
	}
	var notifyRendered string
	switch {
	case m.notification != "":
		notifyRendered = notifyStyleForLevel(m.notificationLevel).Width(notifyWidth).Render(m.notification)
	case m.inResponse:
		// No active notification — use the reserved area for the steer hint
		// instead of growing the input box by a row for it.
		notifyRendered = lipgloss.NewStyle().Width(notifyWidth).Render(steerHint())
	}
	h := max(visualLineCount(notifyRendered), notifyReservedLines)
	notifyStr := padOrClipLines(notifyRendered, h)

	// 7. Divider and input area.
	sepWidth := m.width
	if sepWidth <= 0 {
		sepWidth = 80
	}
	sepStr := separatorStyle.Render(strings.Repeat("─", sepWidth))
	inputStr := m.renderInputArea()

	// 8. Status bar.
	var statusStr string
	if m.width > 0 {
		statusStr = m.computeStatusBar()
	}

	// 9. Assemble the chrome (everything below the viewport) as a single string
	// with no leading newline.  The boundary newline between the viewport and
	// the chrome is added during final assembly, so its height is not double
	// counted by visualLineCount.
	chromeParts := []string{}
	if toolsStr != "" {
		chromeParts = append(chromeParts, toolsStr)
	}
	if promptStr != "" {
		chromeParts = append(chromeParts, promptStr)
	}
	if compStr != "" {
		chromeParts = append(chromeParts, compStr)
	}
	// notifyStr is always present (padOrClipLines guarantees
	// notifyReservedLines rows even when there's no message) — see its
	// comment above for why that fixed reservation matters.
	chromeParts = append(chromeParts, notifyStr, sepStr, inputStr, statusStr)

	chromeStr := strings.Join(chromeParts, "\n")
	chromeHeight := visualLineCount(chromeStr)

	// 10. Calculate total visual height occupied by all non-viewport elements.
	reservedHeight := chromeHeight
	panelSepStr := ""
	if activePanelStr != "" {
		panelSepStr = activePanelStr + "\n\n"
		reservedHeight += visualLineCount(panelSepStr)
	}

	// 11. Decide how much vertical space the viewport region may use.
	availableHeight := max(m.height-reservedHeight, 4)
	contentHeight := max(m.viewport.TotalLineCount(), 1)
	viewportHeight := min(contentHeight, availableHeight)
	topPadding := availableHeight - viewportHeight
	m.viewport.SetHeight(viewportHeight)
	if m.autoFollow {
		m.viewport.GotoBottom()
	}

	// 12. Record geometry in final rendered-view coordinates.
	g.chromeStr = chromeStr
	g.panelSepStr = panelSepStr
	g.topPadding = topPadding

	row := 0
	if panelSepStr != "" {
		row += visualLineCount(panelSepStr)
	}
	if topPadding > 0 {
		row += topPadding
	}
	g.viewportStartY = row
	g.viewportEndY = g.viewportStartY + viewportHeight - 1

	row = g.viewportEndY + 1 // "\n" boundary between viewport and chrome
	if toolsStr != "" {
		g.toolsStartY = row
		g.toolsEndY = row + visualLineCount(toolsStr) - 1
		g.toolBoxes = make([]toolBoxGeometry, len(toolRows))
		for i, tr := range toolRows {
			g.toolBoxes[i] = toolBoxGeometry{id: tr.id, startY: row + tr.startY, endY: row + tr.endY}
		}
		row += visualLineCount(toolsStr) + 1
	}
	if promptStr != "" {
		row += visualLineCount(promptStr) + 1
	}
	if compStr != "" {
		row += visualLineCount(compStr) + 1
	}
	row += visualLineCount(notifyStr) + 1 // notifyStr is always present
	row += visualLineCount(sepStr) + 1    // separator is always present
	g.inputStartY = row
	inputHeight := visualLineCount(inputStr)
	g.inputEndY = row + inputHeight - 1
	g.statusY = g.inputEndY + 1

	return g
}

// --- key handling ----------------------------------------------------------

// handleKey dispatches a keypress and then re-syncs the completions
// selection against whatever the keystroke just did to m.input. The sync
// can't happen only inside handleCompletionKey's own pre-dispatch check —
// that check runs BEFORE a character insertion/deletion below it in the same
// keystroke, so it always compares against the token as of the START of this
// call. Without the post-dispatch sync, a query-narrowing keystroke leaves
// compSelected pointing at a stale index for one extra render frame instead
// of resetting immediately.
func (m *model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	cmd := m.dispatchKey(msg)
	m.syncCompletionSelection()
	return cmd
}

// syncCompletionSelection resets compSelected to the top match whenever the
// token being completed has changed since it was last set.
func (m *model) syncCompletionSelection() {
	_, token := m.completionRows()
	if token != m.compToken {
		m.compToken = token
		m.compSelected = 0
	}
}

func (m *model) dispatchKey(msg tea.KeyPressMsg) tea.Cmd {
	// Interactive prompt active: route keys to prompt handler.
	if m.activePrompt != nil {
		return m.handlePromptKey(msg)
	}

	// Context menu open: route keys to the menu, above the completions
	// dropdown (both use up/down/enter/esc and completions can legitimately
	// be visible at the same time — input still has a "/slash" token while
	// right-clicking a tool box — so completions would otherwise eat every
	// menu keystroke), below activePrompt (a blocking host-service
	// round-trip must win over a UI affordance the user can re-open).
	if m.contextMenu != nil {
		return m.handleContextMenuKey(msg)
	}

	// Pure cursor-movement keys don't edit the buffer, so they'd otherwise
	// leave a stale selection highlighted while the cursor visibly moves
	// away from it. Insert/backspace/delete consume the selection
	// themselves (deleteInputSelection) and must NOT be cleared here first.
	if m.inputSel.armed() {
		switch msg.String() {
		case "up", "down", "left", "right", "home", "end",
			"ctrl+a", "ctrl+e", "ctrl+left", "ctrl+right", "alt+left", "alt+right":
			m.inputSel.clear()
		}
	}

	// The completions dropdown gets first refusal on every keystroke while
	// visible — matches taui's OverlayStack precedence (a "soft" overlay
	// that consumes only the keys it recognizes; everything else falls
	// through unchanged to the bindings below).
	if cmd, handled := m.handleCompletionKey(msg); handled {
		return cmd
	}

	switch msg.String() {
	case "ctrl+c":
		return m.handleCtrlC()

	case "ctrl+d":
		if m.input == "" {
			return tea.Quit
		}
		m.clearInput()
		return nil

	case "ctrl+s":
		return m.handleSteer()

	case "ctrl+shift+g":
		return m.cmdCopy("")

	case "pgup":
		m.viewport.HalfPageUp()
		m.autoFollow = false
		return nil

	case "pgdown":
		m.viewport.HalfPageDown()
		if m.viewport.AtBottom() {
			m.autoFollow = true
		}
		return nil

	case "ctrl+l":
		m.clearScreen()
		return nil

	case "esc":
		if m.bashRunning {
			return m.cancelBash()
		}
		if m.viewportSel.armed() || m.inputSel.armed() || m.statusSel.armed() {
			m.clearAllSelections()
			return nil
		}
		// Phase 1: collapse expanded tool or clear tool focus before
		// clearing input, so Esc steps out of tool interaction first.
		if m.expandedID != "" {
			m.expandedID = ""
			return nil
		}
		if m.focusedTool >= 0 {
			m.focusedTool = -1
			return nil
		}
		if m.input != "" {
			m.clearInput()
			return nil
		}
		return nil

	// Up/Down recall history from the first/last logical line, and move the
	// cursor vertically within a multi-line buffer otherwise — matching
	// pkg/taui/lineinput.go's atFirstLineStart/atLastLineEnd gate.
	// Phase 1: when input is empty and history is exhausted, Up/Down navigate
	// tool focus among completed tools.
	case "up":
		if m.atFirstLineStart() {
			m.recallHistory(-1)
			// No history to recall — navigate tool focus.
			if m.shouldNavigateTools() {
				m.focusNextTool(-1)
			}
			return nil
		}
		m.moveCursorVert(-1)
		return nil
	case "down":
		if m.atLastLineEnd() {
			m.recallHistory(1)
			if m.shouldNavigateTools() {
				m.focusNextTool(1)
			}
			return nil
		}
		m.moveCursorVert(1)
		return nil

	case "left":
		m.moveCursorLeft()
		return nil
	case "right":
		m.moveCursorRight()
		return nil
	case "ctrl+left", "alt+left":
		m.inputCursor = m.wordLeft()
		return nil
	case "ctrl+right", "alt+right":
		m.inputCursor = m.wordRight()
		return nil
	case "home", "ctrl+a":
		m.inputCursor = 0
		return nil
	case "end", "ctrl+e":
		m.inputCursor = utf8.RuneCountInString(m.input)
		return nil

	case "ctrl+u":
		m.killToLineStart()
		return nil
	case "ctrl+k":
		m.killToLineEnd()
		return nil
	case "ctrl+w", "ctrl+backspace":
		m.deleteWordBeforeCursor()
		return nil

	// Shift+Enter/Ctrl+J inserts a newline; bare Enter submits.
	case "shift+enter", "ctrl+j":
		m.insertAtCursor("\n")
		return nil

	case "tab":
		// Tab is only meaningful while the completions dropdown is visible
		// (handleCompletionKey, checked before this switch, handles that
		// case) — matches taui, where a bare Tab with no dropdown showing is
		// a no-op (LineInput has no binding for it).
		// Phase 1: when not in response, no prompt, and input empty, Tab
		// navigates tool focus among completed tools.
		if m.shouldNavigateTools() {
			m.focusNextTool(1)
		}
		return nil

	case "shift+tab":
		m.cycleInputMode()
		return nil

	case "enter":
		// Phase 1: toggle tool expansion when a tool is focused.
		if m.focusedTool >= 0 && m.input == "" && !m.inResponse && m.activePrompt == nil {
			return m.toggleToolExpansion()
		}
		return m.submitInput()

	case "space":
		// Space also toggles expansion on a focused tool; otherwise it's a
		// normal printable character handled by the default case below.
		if m.focusedTool >= 0 && m.input == "" && !m.inResponse && m.activePrompt == nil {
			return m.toggleToolExpansion()
		}
		m.insertAtCursor(" ")
		return nil

	case "backspace":
		m.backspaceAtCursor()
		return nil
	case "delete":
		m.deleteAtCursor()
		return nil

	default:
		// Append printable characters using rune-based check so multi-byte
		// UTF-8 (accented chars, emoji, CJK) is not silently dropped (N3).
		if text := msg.Key().Text; text != "" {
			r, _ := utf8.DecodeRuneInString(text)
			if r >= 32 && r != utf8.RuneError {
				m.insertAtCursor(text)
			}
		}
		return nil
	}
}

// clearInput resets the input buffer and cursor together — every reset site
// must clear both or the cursor can end up pointing past the end of a
// shorter (or empty) buffer.
func (m *model) clearInput() {
	m.input = ""
	m.inputCursor = 0
	m.inputSel.clear()
}

// clearScreen wipes the visible scrollback (Ctrl+L) without touching the
// underlying chat session — unlike /clear, which sends a
// ResetChatSessionCommand and actually starts a new session, this only
// clears what's rendered locally. The next ChatSessionSnapshotEvent (e.g.
// from /session, /resume) still rebuilds renderedLines from the real
// session history, so nothing is actually lost.
func (m *model) clearScreen() {
	m.renderedLines = m.renderedLines[:0]
	m.committedGroups = m.committedGroups[:0]
	m.viewport.SetContentLines(m.renderedLines)
	m.autoFollow = true
	m.viewportSel.clear()
	m.viewport.GotoBottom()
}

// --- cursor-aware input editing ---------------------------------------------
//
// These mirror pkg/taui/lineinput.go's editing primitives (insertLocked,
// deleteWordLocked, wordLeftLocked/wordRightLocked, moveVertLocked, and the
// split/cursorInLines/linePos line-math) so the two frontends behave
// identically. input is kept as a string and converted to []rune per call —
// fine at chat-input scale, and it keeps model.input's type simple for
// everything else (history, slash-command parsing, tab completion) that
// still treats it as a plain string.

// deleteInputSelection removes the currently selected input range (if any),
// moves the cursor to where the selection started, and clears it. Reports
// whether there was a selection to consume, so insertAtCursor/backspaceAt-
// Cursor/deleteAtCursor can fall back to their normal single-character
// behavior when there wasn't one — this is what makes typing/backspace/
// delete replace or remove a mouse-dragged selection, exactly like any
// normal text field.
func (m *model) deleteInputSelection() bool {
	lo, hi, ok := m.inputSel.bounds()
	if !ok {
		return false
	}
	rs := []rune(m.input)
	lo = max(min(lo, len(rs)), 0)
	hi = max(min(hi, len(rs)), 0)
	rs = append(rs[:lo], rs[hi:]...)
	m.input = string(rs)
	m.inputCursor = lo
	m.inputSel.clear()
	return true
}

func (m *model) insertAtCursor(s string) {
	m.deleteInputSelection()
	rs := []rune(m.input)
	ins := []rune(s)
	nr := make([]rune, 0, len(rs)+len(ins))
	nr = append(nr, rs[:m.inputCursor]...)
	nr = append(nr, ins...)
	nr = append(nr, rs[m.inputCursor:]...)
	m.input = string(nr)
	m.inputCursor += len(ins)
}

func (m *model) backspaceAtCursor() {
	if m.deleteInputSelection() {
		return
	}
	if m.inputCursor <= 0 {
		return
	}
	rs := []rune(m.input)
	rs = append(rs[:m.inputCursor-1], rs[m.inputCursor:]...)
	m.input = string(rs)
	m.inputCursor--
}

func (m *model) deleteAtCursor() {
	if m.deleteInputSelection() {
		return
	}
	rs := []rune(m.input)
	if m.inputCursor >= len(rs) {
		return
	}
	rs = append(rs[:m.inputCursor], rs[m.inputCursor+1:]...)
	m.input = string(rs)
}

func (m *model) moveCursorLeft() {
	if m.inputCursor > 0 {
		m.inputCursor--
	}
}

func (m *model) moveCursorRight() {
	if m.inputCursor < utf8.RuneCountInString(m.input) {
		m.inputCursor++
	}
}

func (m *model) killToLineStart() {
	rs := []rune(m.input)
	m.input = string(rs[m.inputCursor:])
	m.inputCursor = 0
}

func (m *model) killToLineEnd() {
	rs := []rune(m.input)
	if m.inputCursor < len(rs) {
		m.input = string(rs[:m.inputCursor])
	}
}

func (m *model) wordLeft() int {
	rs := []rune(m.input)
	i := m.inputCursor
	for i > 0 && rs[i-1] == ' ' {
		i--
	}
	for i > 0 && rs[i-1] != ' ' && rs[i-1] != '\n' {
		i--
	}
	return i
}

func (m *model) wordRight() int {
	rs := []rune(m.input)
	i := m.inputCursor
	for i < len(rs) && rs[i] != ' ' && rs[i] != '\n' {
		i++
	}
	for i < len(rs) && rs[i] == ' ' {
		i++
	}
	return i
}

func (m *model) deleteWordBeforeCursor() {
	i := m.wordLeft()
	rs := []rune(m.input)
	rs = append(rs[:i], rs[m.inputCursor:]...)
	m.input = string(rs)
	m.inputCursor = i
}

// atFirstLineStart/atLastLineEnd gate Up/Down between history recall and
// intra-buffer cursor movement (see the "up"/"down" cases in handleKey).
func (m *model) atFirstLineStart() bool { return m.inputCursor == 0 }

func (m *model) atLastLineEnd() bool {
	return m.inputCursor >= utf8.RuneCountInString(m.input)
}

// splitInputLines returns the logical lines (split on '\n') of m.input.
func (m *model) splitInputLines() [][]rune {
	rs := []rune(m.input)
	var out [][]rune
	start := 0
	for i, r := range rs {
		if r == '\n' {
			out = append(out, rs[start:i])
			start = i + 1
		}
	}
	return append(out, rs[start:])
}

// cursorLineCol returns (line index, column) for the current cursor.
func (m *model) cursorLineCol(lines [][]rune) (int, int) {
	pos := 0
	for idx, ln := range lines {
		end := pos + len(ln)
		if m.inputCursor <= end {
			return idx, m.inputCursor - pos
		}
		pos = end + 1 // +1 for the newline
	}
	return len(lines) - 1, len(lines[len(lines)-1])
}

// linePos returns the absolute cursor index for (line, col) within lines.
func linePos(lines [][]rune, targetLine, col int) int {
	pos := 0
	for i := range targetLine {
		pos += len(lines[i]) + 1
	}
	return pos + min(col, len(lines[targetLine]))
}

// moveCursorVert moves the cursor up (-1) or down (+1) one logical line,
// preserving column where possible.
func (m *model) moveCursorVert(dir int) {
	lines := m.splitInputLines()
	curLine, curCol := m.cursorLineCol(lines)
	target := curLine + dir
	if target < 0 || target >= len(lines) {
		return
	}
	col := min(curCol, len(lines[target]))
	m.inputCursor = linePos(lines, target, col)
}

// --- input rendering ---------------------------------------------------------

// renderInputArea draws the (possibly multi-line) input buffer with a
// highlighted block at the caret position — mirrors pkg/taui/lineinput.go's
// visual cursor (a coloured background over the character under the cursor)
// rather than the terminal's native cursor, so it composes as plain content
// alongside the rest of View()'s single string.
func (m *model) renderInputArea() string {
	lines := m.splitInputLines()
	curLine, curCol := m.cursorLineCol(lines)
	selLoAbs, selHiAbs, hasSel := m.inputSel.bounds()

	boxWidth := m.width
	if boxWidth <= 0 {
		boxWidth = 80
	}
	innerWidth := max(boxWidth-2, 1)
	body := make([]string, 0, len(lines))
	for i, ln := range lines {
		prefix := inputPromptStyle.Render("> ")
		if m.inResponse {
			prefix = inputSteerPromptStyle.Render("(steer) > ")
		}
		prefixWidth := visibleWidth(stripANSI(prefix))
		contentWidth := max(innerWidth-prefixWidth, 1)
		continuationPrefix := strings.Repeat(" ", prefixWidth)

		// Selection bounds are absolute rune positions into m.input; convert
		// to this line's own local rune range so per-chunk rendering below
		// can treat them the same way it already treats curCol.
		lineStartAbs := linePos(lines, i, 0)
		selLo, selHi, lineHasSel := 0, 0, false
		if hasSel {
			lo := max(selLoAbs-lineStartAbs, 0)
			hi := min(selHiAbs-lineStartAbs, len(ln))
			lineHasSel = lo < hi
			selLo, selHi = lo, hi
		}

		chunks := wrapInputLine(ln, contentWidth)
		if i == curLine && curCol == len(ln) && len(chunks) > 0 {
			last := chunks[len(chunks)-1]
			if visibleWidth(string(ln[last.start:last.end])) >= contentWidth {
				chunks = append(chunks, inputLineChunk{start: len(ln), end: len(ln)})
			}
		}
		for j, chunk := range chunks {
			rowPrefix := prefix
			if i > 0 || j > 0 {
				rowPrefix = continuationPrefix
			}

			hasCursor := i == curLine && curCol >= chunk.start && curCol < chunk.end
			if i == curLine && curCol == len(ln) && j == len(chunks)-1 {
				hasCursor = true
			}
			switch {
			case !hasCursor && !lineHasSel:
				body = append(body, rowPrefix+inputStyle.Render(string(ln[chunk.start:chunk.end])))
			default:
				body = append(body, rowPrefix+renderInputChunk(ln, chunk.start, chunk.end, hasCursor, curCol, lineHasSel, selLo, selHi))
			}
		}
	}
	if desc := m.inputModePlaceholder(); desc != "" {
		prefixWidth := visibleWidth(stripANSI(inputPromptStyle.Render("> ")))
		if m.inResponse {
			prefixWidth = visibleWidth(stripANSI(inputSteerPromptStyle.Render("(steer) > ")))
		}
		body = append(body, strings.Repeat(" ", prefixWidth)+inputPlaceholderStyle.Render(desc))
	}

	title := m.inputModeTitle()
	return renderInputBox(m.width, title, body, "")
}

// steerHint renders the "[Ctrl+C] stop | [Enter] steer" hint shown in the
// notification area (not the input box itself, which would otherwise grow
// by a row for it) while a response is in flight.
func steerHint() string {
	ctrlC := lipgloss.NewStyle().Foreground(themeHex(theme.ToneError)).Bold(true).Render("Ctrl+C")
	enter := lipgloss.NewStyle().Foreground(themeHex(theme.CommandFG)).Bold(true).Render("Enter")
	return lipgloss.NewStyle().Foreground(themeHex(theme.ToneMuted)).Italic(true).Render(
		fmt.Sprintf("[%s] stop | [%s] steer", ctrlC, enter),
	)
}

// inputPositionAt maps a (row, col) coordinate within the rendered input
// box's text area — row 0 is the box's first body row (i.e. right below
// its top border and hint row, see the bodyRowOffset constant below); col 0
// is the first column after the left border and the "> "/"(steer) > "
// prefix (or the matching blank indent on a continuation row) — to a rune
// index into m.input. This is the inverse of renderInputArea's own layout
// math (same wrapInputLine/linePos calls), so a click positions the cursor
// at exactly the character under the mouse.
//
// It does not replicate renderInputArea's one edge case where an extra
// trailing empty chunk is inserted when the *current* cursor sits exactly
// at a wrap boundary (that tweak depends on where the cursor already is,
// not on the click being mapped) — a click landing on that extra row in
// that narrow case can be off by one row. Acceptable: it only affects a
// click's target position, never what gets copied once selected, and it's
// a rare edge case (cursor exactly at a wrap boundary on a wrapped line).
func (m *model) inputPositionAt(row, col int) int {
	const bodyRowOffset = 2 // top border + hint row precede body rows

	lines := m.splitInputLines()
	bodyRow := row - bodyRowOffset
	if bodyRow < 0 {
		return 0
	}

	prefix := inputPromptStyle.Render("> ")
	if m.inResponse {
		prefix = inputSteerPromptStyle.Render("(steer) > ")
	}
	prefixWidth := visibleWidth(stripANSI(prefix))
	boxWidth := m.width
	if boxWidth <= 0 {
		boxWidth = 80
	}
	innerWidth := max(boxWidth-2, 1)
	contentWidth := max(innerWidth-prefixWidth, 1)
	textCol := max(col-1-prefixWidth, 0) // -1 for the left border column

	renderedRow := 0
	for i, ln := range lines {
		for _, chunk := range wrapInputLine(ln, contentWidth) {
			if renderedRow == bodyRow {
				rel := min(textCol, chunk.end-chunk.start)
				return linePos(lines, i, chunk.start+rel)
			}
			renderedRow++
		}
	}
	// Past all rendered lines (e.g. the placeholder row, or below the
	// content) — snap to the very end of the buffer.
	return utf8.RuneCountInString(m.input)
}

// inputSelectionText returns the substring of m.input between rune
// positions lo and hi (half-open — these are cursor positions, not
// inclusive line indices like the viewport's), for finalizeSelection.
func (m *model) inputSelectionText(lo, hi int) string {
	runes := []rune(m.input)
	lo = max(min(lo, len(runes)), 0)
	hi = max(min(hi, len(runes)), 0)
	if lo >= hi {
		return ""
	}
	return string(runes[lo:hi])
}

// statusSelectionText returns the substring of the status bar's plain
// (ANSI-stripped) text between columns lo and hi (half-open), for
// finalizeSelection. The status bar has no left border/prefix, so a screen
// column maps directly to a column in its plain text.
func (m *model) statusSelectionText(lo, hi int) string {
	runes := []rune(stripANSI(m.computeStatusBar()))
	lo = max(min(lo, len(runes)), 0)
	hi = max(min(hi, len(runes)), 0)
	if lo >= hi {
		return ""
	}
	return string(runes[lo:hi])
}

type inputLineChunk struct {
	start int
	end   int
}

func wrapInputLine(ln []rune, maxWidth int) []inputLineChunk {
	if maxWidth < 1 {
		maxWidth = 1
	}
	if len(ln) == 0 {
		return []inputLineChunk{{start: 0, end: 0}}
	}

	var chunks []inputLineChunk
	start, width := 0, 0
	for i, r := range ln {
		rw := max(visibleWidth(string(r)), 1)
		if width > 0 && width+rw > maxWidth {
			chunks = append(chunks, inputLineChunk{start: start, end: i})
			start = i
			width = 0
		}
		width += rw
	}
	chunks = append(chunks, inputLineChunk{start: start, end: len(ln)})
	return chunks
}

func (m *model) inputModePlaceholder() string {
	text := m.input
	if !strings.HasPrefix(text, "/") || !strings.HasSuffix(text, " ") {
		return ""
	}
	name, args := slashNameAndArgs(text)
	if args != "" {
		return ""
	}
	entry, ok := slashIndex[name]
	if !ok || !entry.isAgent {
		return ""
	}
	return entry.description
}

func (m *model) inputModeTitle() string {
	if m.inResponse {
		return "steer"
	}
	text := strings.TrimSpace(m.input)
	switch {
	case strings.HasPrefix(text, "!!"):
		return "shell local"
	case strings.HasPrefix(text, "!"):
		return "shell"
	case strings.HasPrefix(text, "/"):
		name, _ := slashNameAndArgs(text)
		if entry, ok := slashIndex[name]; ok {
			if entry.isAgent {
				return entry.modeLabel()
			}
			return "command"
		}
		return "command"
	default:
		return "chat"
	}
}

func (m *model) cycleInputMode() {
	if m.inResponse || m.activePrompt != nil || m.bashRunning {
		return
	}
	m.inputSel.clear()
	modes := inputModes()
	if len(modes) == 0 {
		return
	}

	current, args := m.currentInputModeIndex(modes)
	next := (current + 1) % len(modes)
	mode := modes[next]

	if mode.command == "" {
		m.input = args
	} else if args == "" {
		m.input = "/" + mode.command + " "
	} else {
		m.input = "/" + mode.command + " " + args
	}
	m.inputCursor = utf8.RuneCountInString(m.input)
}

func (m *model) currentInputModeIndex(modes []inputMode) (int, string) {
	text := strings.TrimSpace(m.input)
	if strings.HasPrefix(text, "/") {
		name, args := slashNameAndArgs(text)
		for i, mode := range modes {
			if mode.command == name {
				return i, args
			}
		}
		return 0, strings.TrimSpace(strings.TrimPrefix(text, "/"))
	}
	return 0, text
}

func (m *model) viewportLinesForView(visibleReasoning bool) []string {
	lines := make([]string, 0, len(m.renderedLines)+4)
	lines = append(lines, m.renderedLines...)
	m.highlightSelection(lines)

	if m.inResponse && len(m.tools) == 0 && m.streaming == "" && !visibleReasoning {
		lines = append(lines, m.workingIndicator())
		return lines
	}
	if visibleReasoning {
		for line := range strings.SplitSeq(m.reasoning, "\n") {
			lines = append(lines, reasoningStyle.Render("Thinking: "+line))
		}
	}
	if m.streaming != "" {
		for line := range strings.SplitSeq(m.streaming, "\n") {
			lines = append(lines, streamStyle.Render(line))
		}
	}
	return lines
}

func renderInputBox(width int, title string, lines []string, hint string) string {
	if width <= 0 {
		width = 80
	}
	if width < 8 {
		return strings.Join(lines, "\n")
	}

	innerWidth := max(width-2, 1)
	label := " " + title + " "
	topRuleWidth := max(width-2-lipgloss.Width(label), 0)
	top := inputBoxStyle.Render("╭" + label + strings.Repeat("─", topRuleWidth) + "╮")
	bottom := inputBoxStyle.Render("╰" + strings.Repeat("─", width-2) + "╯")

	out := make([]string, 0, len(lines)+2)
	out = append(out, top)
	out = append(out, renderInputBoxLine(innerWidth, hint))
	for _, line := range lines {
		out = append(out, renderInputBoxLine(innerWidth, line))
	}
	out = append(out, renderInputBoxLine(innerWidth, ""))
	out = append(out, bottom)
	return strings.Join(out, "\n")
}

func renderInputBoxLine(innerWidth int, line string) string {
	plainWidth := visibleWidth(stripANSI(line))
	if plainWidth > innerWidth {
		line = truncateANSIToWidth(line, innerWidth, "…")
		plainWidth = visibleWidth(stripANSI(line))
	}
	padding := strings.Repeat(" ", max(innerWidth-plainWidth, 0))
	return inputBoxStyle.Render("│") + line + padding + inputBoxStyle.Render("│")
}

// renderInputChunk renders ln[chunkStart:chunkEnd], applying the block
// cursor (if hasCursor and cursorCol falls within this chunk, or sits
// exactly at chunkEnd — a cursor past the chunk's last rune still
// highlights a trailing blank cell so it's never invisible) and/or a
// reverse-video selection highlight for [selLo,selHi) — both absolute rune
// indices into the full ln, matching curCol's own convention. The cursor's
// own highlight takes visual priority over selection for the one rune it
// lands on.
func renderInputChunk(ln []rune, chunkStart, chunkEnd int, hasCursor bool, cursorCol int, hasSel bool, selLo, selHi int) string {
	var b strings.Builder
	for p := chunkStart; p < chunkEnd; p++ {
		switch {
		case hasCursor && p == cursorCol:
			b.WriteString(inputCursorStyle.Render(string(ln[p])))
		case hasSel && p >= selLo && p < selHi:
			b.WriteString("\x1b[7m")
			b.WriteString(inputStyle.Render(string(ln[p])))
			b.WriteString("\x1b[27m")
		default:
			b.WriteString(inputStyle.Render(string(ln[p])))
		}
	}
	if hasCursor && cursorCol == chunkEnd && cursorCol >= chunkStart {
		b.WriteString(inputCursorStyle.Render(" "))
	}
	return b.String()
}

func (m *model) recallHistory(delta int) tea.Cmd {
	if len(m.history) == 0 {
		return nil
	}
	if m.historyIdx == -1 {
		// Start navigating: stash current input as draft before overwriting.
		m.draftInput = m.input
		if delta < 0 {
			m.historyIdx = len(m.history) - 1
		} else {
			m.historyIdx = 0
		}
	} else {
		m.historyIdx += delta
		if m.historyIdx < 0 {
			m.historyIdx = 0
		}
		if m.historyIdx > len(m.history) {
			m.historyIdx = len(m.history)
		}
	}
	if m.historyIdx == len(m.history) {
		m.input = m.draftInput
	} else {
		m.input = m.history[m.historyIdx]
	}
	m.inputCursor = utf8.RuneCountInString(m.input)
	return nil
}

// seedHistoryFromMessages seeds the input history (Up/Down recall) from a
// loaded session's user messages — mirrors
// internal/tui/inline_chat.go's function of the same name. Leaves the
// current history untouched when the session had no user messages, rather
// than clearing it.
func (m *model) seedHistoryFromMessages(messages []tauchat.ChatMessage) {
	var prompts []string
	for _, msg := range messages {
		if msg.Role == tauchat.ChatRoleUser && strings.TrimSpace(msg.Content) != "" {
			prompts = append(prompts, msg.Content)
		}
	}
	if len(prompts) > 0 {
		m.history = prompts
		m.historyIdx = -1
	}
}

func (m *model) submitInput() tea.Cmd {
	// Interactive prompt active: handle prompt input.
	if m.activePrompt != nil {
		return m.resolvePrompt()
	}

	// If a response is in-flight, any Enter press acts as a steering command!
	if m.inResponse {
		if strings.TrimSpace(m.input) == "" {
			return nil
		}
		return m.handleSteer()
	}

	if m.bashRunning {
		m.clearInput()
		return m.setNotification("still waiting for a response…")
	}

	text := strings.TrimSpace(m.input)
	m.clearInput()
	m.historyIdx = -1  // reset history navigation
	m.compSelected = 0 // reset completion dropdown selection
	m.compToken = ""
	if text == "" {
		return nil
	}

	// Slash commands.
	if strings.HasPrefix(text, "/") {
		return m.handleSlashCommand(text)
	}

	// Bash mode: !command (or !!command, excluded from what the model sees)
	// runs outside the LLM turn loop. handleBashCommand does its own
	// bang-stripping on the full text, not just a single "!".
	if strings.HasPrefix(text, "!") {
		return m.handleBashCommand(text)
	}

	// Debounce guard: 300ms between submits (P2 #27).
	if elapsed := time.Since(m.lastSubmit); elapsed < 300*time.Millisecond {
		return m.setNotification("slow down — submit debounced")
	}
	m.lastSubmit = time.Now()

	// Record in history.
	m.history = append(m.history, text)

	// Queue or start a turn.
	return m.startOrQueueTurn(text)
}

// startOrQueueTurn sends a prompt, queueing it behind a running turn.
func (m *model) startOrQueueTurn(text string) tea.Cmd {
	if m.inResponse {
		m.turnQueue = append(m.turnQueue, text)
		return m.setNotification("queued — will send after current response")
	}

	// Record the user message locally for immediate display. Submitting a
	// prompt always means "show me what happens next" — resume following.
	m.autoFollow = true
	m.appendMessage("user", text)
	m.inResponse = true
	m.steering = false

	return sendCommand(m.runtime, tauchat.SubmitChatPromptCommand{
		SessionID:   m.sessionID,
		RequestID:   newRequestID(),
		Prompt:      text,
		SubmittedAt: time.Now().UTC(),
	})
}

// drainTurnQueue sends the next queued prompt, if any.
func (m *model) drainTurnQueue() tea.Cmd {
	if len(m.turnQueue) == 0 {
		return nil
	}
	next := m.turnQueue[0]
	m.turnQueue = m.turnQueue[1:]
	return m.startOrQueueTurn(next)
}

// noopCmd performs no action. Used where a handler must always return a
// non-nil Cmd even when there's nothing to schedule.
var noopCmd tea.Cmd = func() tea.Msg { return nil }

// handleSteer sends a steering command mid-turn, or — while idle — falls
// through to a normal submit rather than rejecting the keystroke, so
// whatever the user typed is never silently lost. Mirrors
// internal/tui/inline_chat.go's onSteer.
func (m *model) handleSteer() tea.Cmd {
	text := strings.TrimSpace(m.input)

	if !m.inResponse {
		if text == "" {
			return nil
		}
		m.clearInput()
		return m.startOrQueueTurn(text)
	}

	m.clearInput()
	if text == "" {
		// No visible feedback needed here — the status bar already shows a
		// "steering…" segment (see computeStatusBar) whenever m.steering is true.
		m.steering = !m.steering
		return nil
	}
	m.steering = true
	return sendCommand(m.runtime, tauchat.SteerChatPromptCommand{
		SessionID:   m.sessionID,
		RequestID:   newRequestID(),
		Prompt:      text,
		SubmittedAt: time.Now().UTC(),
	})
}

// handleBashCommand runs a "!" (or "!!") bash-mode command. trimmed is the
// full submitted text, bang(s) included — "!!" (or "!!!", "!!!!", ...) marks
// the command as Exclude: true, meaning it's hidden from what the model
// sees in the conversation history. Every leading "!" is stripped, not just
// one or two, so "!!!ls" doesn't leave a literal "!" glued onto the front
// of the command. The CallID is generated here (not by the coordinator) and
// recorded in m.bashCallID before the command is sent, so the matching
// ChatToolExecutionCompletedEvent can be recognised as "ours" and clear
// bashRunning. Mirrors internal/tui/inline_chat.go's handleBashCommand.
func (m *model) handleBashCommand(trimmed string) tea.Cmd {
	exclude := strings.HasPrefix(trimmed, "!!")
	command := strings.TrimSpace(strings.TrimLeft(trimmed, "!"))
	if command == "" {
		return nil
	}

	callID := "bash-" + newRequestID()
	m.bashRunning = true
	m.bashCallID = callID
	m.autoFollow = true
	m.appendMessage("user", trimmed)
	return sendBashCommand(m.runtime, tauchat.RunBashCommand{
		SessionID:   m.sessionID,
		CallID:      callID,
		Command:     command,
		Exclude:     exclude,
		RequestedAt: time.Now().UTC(),
	})
}

func (m *model) cancelBash() tea.Cmd {
	if !m.bashRunning {
		return nil
	}
	m.bashRunning = false
	m.bashCallID = ""
	return sendCommand(m.runtime, tauchat.CancelBashCommand{
		SessionID:   m.sessionID,
		RequestedAt: time.Now().UTC(),
	})
}

// quitConfirmWindow is how long a second Ctrl+C is honored as "confirm
// quit" — matches internal/tui/inline_chat.go's quitConfirmWindow.
const quitConfirmWindow = 800 * time.Millisecond

// handleCtrlC triages a Ctrl+C press exactly like inline_chat.go's
// inlineCtrl.HandleInput: cancel an in-flight turn or bash command first, and
// only treat Ctrl+C as "quit" (with a double-tap confirmation) when there's
// nothing running to cancel — so an accidental Ctrl+C during generation never
// silently kills the program.
func (m *model) handleCtrlC() tea.Cmd {
	if m.inResponse {
		return m.cancelTurn()
	}
	if m.bashRunning {
		return m.cancelBash()
	}
	now := time.Now()
	if now.Sub(m.pendingQuit) < quitConfirmWindow {
		return tea.Quit
	}
	m.pendingQuit = now
	return m.setNotification("quit: press Ctrl+C again")
}

// cancelTurn sends a CancelChatRequestCommand to stop the current
// generation. m.inResponse is cleared asynchronously by the resulting
// ChatResponseCancelledEvent, not here.
func (m *model) cancelTurn() tea.Cmd {
	m.steering = false
	return sendCommand(m.runtime, tauchat.CancelChatRequestCommand{
		SessionID:   m.sessionID,
		RequestedAt: time.Now().UTC(),
	})
}

// newRequestID generates a UUIDv7 request/call ID, falling back to a
// timestamp if the platform's random source is unavailable.
func newRequestID() string {
	if id, err := uuid.NewV7(); err == nil {
		return id.String()
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// --- chat event handling ---------------------------------------------------

// handleChatEvent mutates model state and returns an optional Cmd (e.g. for
// notification-clear timers or auto-responses). It MUST be called from within
// Update so the returned Cmd is properly composed.
func (m *model) handleChatEvent(evt tauchat.ChatEvent) tea.Cmd {
	switch e := evt.(type) {
	case tauchat.ChatSessionSnapshotEvent:
		m.applySnapshot(e)

	case tauchat.ChatResponseStartedEvent:
		m.streaming = ""
		m.reasoning = ""
		m.tools = nil
		m.toolGroupCollapsed = m.toolCallsDefaultCollapsed
		m.inResponse = true
		m.spinnerFrame = 0
		m.turnStartedAt = time.Now()
		// Seed the verb rotation from the turn's start so each turn opens on a
		// different word; the low bits of the nanosecond clock are plenty of
		// spread for a cosmetic choice.
		m.turnSeed = m.turnStartedAt.UnixNano()
		return spinTick()

	case tauchat.ChatResponseDeltaEvent:
		// New text after a tool-calling batch means that batch is over —
		// commit it now so it lands in scrollback before (not after) this
		// text, and so the live tool-call box doesn't linger below text that
		// chronologically comes after it.
		if len(m.tools) > 0 {
			m.flushToolGroup()
		}
		m.streaming += e.Delta

	case tauchat.ChatReasoningDeltaEvent:
		m.reasoning += e.Delta

	case tauchat.ChatToolCallDeltaEvent:
		m.upsertToolCall(e.CallID, e.ToolName, e.ArgumentsSummary)

	case tauchat.ChatToolExecutionStartedEvent:
		m.upsertToolCall(e.CallID, e.ToolName, e.ArgumentsSummary)
		m.setToolStatus(e.CallID, "running")

	case tauchat.ChatToolExecutionCompletedEvent:
		status := "done"
		if e.IsError {
			status = "error"
		}
		m.setToolStatus(e.CallID, status)
		if e.ResultSummary != "" {
			m.finalizeToolResult(e.CallID, e.ResultSummary)
		}
		if m.bashCallID != "" && e.CallID == m.bashCallID {
			m.bashRunning = false
			m.bashCallID = ""
		}

	case tauchat.ChatToolOutputEvent:
		m.setToolResult(e.CallID, e.Chunk)
		m.appendToolTail(e.CallID, e.Chunk)

	case tauchat.ChatResponseCompletedEvent:
		content := m.finalizeResponse(lastAssistantMessageID(e.State))
		// noopCmd guarantees a non-nil Batch even when there's no queued turn
		// to drain and no desktop notification to fire.
		cmds := []tea.Cmd{noopCmd, m.drainTurnQueue()}
		// Only nudge the user via desktop notification when they've
		// actually looked away — while focused, the response already
		// streamed onto their screen. Matches internal/tui/inline_events.go.
		if content != "" && !m.focused {
			cmds = append(cmds, tea.Raw(termkit.Notify("tau", content)))
		}
		return tea.Batch(cmds...)

	case tauchat.ChatResponseCancelledEvent:
		m.streaming = ""
		m.reasoning = ""
		m.tools = nil
		m.inResponse = false
		m.steering = false
		return m.drainTurnQueue()

	case tauchat.ChatRuntimeErrorEvent:
		// Abandons the in-flight turn's UI state entirely (matches legacy's
		// clearTurnLocked) — an error means there's nothing left to stream
		// into, so the streaming/reasoning/tools buffers would otherwise
		// linger as stale leftovers from the failed turn.
		m.steering = false
		m.inResponse = false
		m.streaming = ""
		m.reasoning = ""
		m.tools = nil
		// Shown via the notification banner (above the input area) at error
		// level with duration 0 (persists until dismissed rather than
		// silently expiring), and also printed to the scrollback so it
		// isn't lost if overtaken by a later notice.
		cmd := m.setNotificationWithLevel(e.Message, notify.LevelError, 0)
		m.appendMessage("system", "✗ "+e.Message)
		return cmd

	case tauchat.ChatNotificationEvent:
		cmd := m.setNotificationWithLevel(e.Message, notifyLevelFromChat(e.Level), notifyDurationFromChat(e.Level))
		if e.Level == tauchat.ChatNotificationError {
			m.appendMessage("system", e.Message)
		}
		return cmd

	case tauchat.InteractivePromptRequestedEvent:
		return m.enqueuePrompt(e)

	// Session events.
	case tauchat.SessionsListedEvent:
		m.sessionSummaries = e.Sessions
		m.appendMessage("system", sessionSummariesText(e.Sessions, e.NextCursor))
		return nil

	case tauchat.SessionLoadedEvent:
		// Reuses the same state-sync + message-replay logic as an initial
		// snapshot — mirrors internal/tui/inline_chat.go's syncState +
		// printMessage-per-message replay, so a loaded session's history is
		// actually visible instead of just a notification.
		m.applySnapshot(tauchat.ChatSessionSnapshotEvent(e))
		m.seedHistoryFromMessages(e.State.Messages)
		return m.setNotification(fmt.Sprintf("Session %s loaded (%d messages)", e.State.SessionID, len(e.State.Messages)))

	case tauchat.SessionDeletedEvent:
		return m.setNotification("session deleted: " + e.SessionID)

	case tauchat.SessionExportedEvent:
		return m.setNotification("session exported: " + e.Path)

	// Extension / plugin events.
	case tauchat.ExtensionsReloadedEvent:
		return m.setNotification(fmt.Sprintf("extensions: %d loaded, %d diagnostics", e.Result.ExtensionCount, len(e.Result.Diagnostics)))

	case tauchat.ExtensionCommandsChangedEvent:
		m.extensionCommands = make(map[string]tauchat.ExtensionCommand, len(e.Commands))
		for _, ext := range e.Commands {
			m.extensionCommands[ext.Name] = ext
		}
		return nil

	case tauchat.CommandsChangedEvent:
		// Registry commands changed — no-op for now (completion picks them up
		// from slashTable which is static).
		return nil

	case tauchat.ExtensionCommandResultEvent:
		if e.Output != "" {
			m.appendMessage("system", e.Output)
		}
		if e.View != nil {
			text := renderPluginView(*e.View)
			if e.View.Title != "" {
				text = e.View.Title + "\n" + text
			}
			m.appendMessage("system", text)
		}
		return nil

	case tauchat.ExtensionViewRenderedEvent:
		m.panels[e.ViewID] = pluginPanel{
			id:      e.ViewID,
			title:   e.View.Title,
			content: renderPluginView(e.View),
		}
		return nil

	case tauchat.ExtensionViewClosedEvent:
		delete(m.panels, e.ViewID)
		return nil

	// Skills events.
	case tauchat.SkillsChangedEvent:
		m.appendMessage("system", skillsChangedText(e.Skills))
		return nil

	default:
		return nil
	}
	return nil
}

func (m *model) applySnapshot(e tauchat.ChatSessionSnapshotEvent) {
	state := e.State
	if state.SessionID != "" {
		m.sessionID = state.SessionID
	}
	if state.Model.ID != "" {
		m.modelName = state.Model.ID
	}
	if state.ProviderName != "" {
		m.provider = state.ProviderName
	}
	// N15: statusText consistently means "model @ provider"; never carry
	// error text here — errors go through m.notification.
	m.statusText = fmt.Sprintf("%s @ %s", m.modelName, m.provider)

	// Rebuild viewport content from session history. Snapshots arrive
	// routinely (not just on session load/resume) — e.g. after every
	// submitted prompt — so this must reproduce finalizeResponse's/
	// flushToolGroup's rendering exactly, or an already-committed message
	// reverts to raw/unbatched form the next time a snapshot rebuilds the
	// viewport.
	//
	// toolCallsByID tracks each assistant tool_call's name/arguments so a
	// later ChatRoleTool message (which only carries the call's ID and
	// result) can be rendered as a real tool call instead of being dropped —
	// history has no per-call elapsed time or error flag, so replayed calls
	// always show status "done" with no timing. pendingTools batches a run of
	// consecutive tool results (uninterrupted by user/assistant text) so a
	// turn with many tool calls renders as one compact group instead of
	// burying the surrounding conversation.
	// oldGroups lets a fold/row-expand the user made on a committed group
	// survive this rebuild — without it, every submitted prompt (which
	// triggers a fresh snapshot) would silently refold anything they'd
	// opened. Keyed by committedGroupKey, which stays stable across
	// rebuilds (see its doc comment).
	oldGroups := make(map[string]*committedToolGroup, len(m.committedGroups))
	for _, g := range m.committedGroups {
		oldGroups[committedGroupKey(g.tools)] = g
	}
	m.committedGroups = m.committedGroups[:0]

	toolCallsByID := map[string]tauchat.ChatToolCall{}
	var pendingTools []toolState
	flushPendingTools := func() {
		if len(pendingTools) == 0 {
			return
		}
		m.commitToolGroup(pendingTools, oldGroups)
		pendingTools = nil
	}

	m.renderedLines = m.renderedLines[:0]
	m.messageRanges = m.messageRanges[:0]
	m.viewportSel.clear()
	for idx, msg := range state.Messages {
		switch msg.Role {
		case tauchat.ChatRoleUser:
			if cmd, output, ok := parseBashHistoryMessage(msg.Content); ok {
				pendingTools = append(pendingTools, toolState{
					// A stable, index-derived ID (not uuid.NewString()) —
					// this entry has no real call ID, and committedGroupKey
					// needs the same ID back on every rebuild for fold
					// state to carry over instead of resetting.
					id:     fmt.Sprintf("bash-%d", idx),
					name:   "shell",
					args:   cmd,
					result: output,
					status: "done",
				})
				continue
			}
			flushPendingTools()
			start := len(m.renderedLines)
			lines := strings.Split(msg.Content, "\n")
			for i, l := range lines {
				if i == 0 {
					m.renderedLines = append(m.renderedLines, renderLine("user", l))
				} else {
					m.renderedLines = append(m.renderedLines, renderContinuationLine("user", l))
				}
			}
			if msg.ID != "" {
				m.messageRanges = append(m.messageRanges, messageLineRange{id: msg.ID, content: msg.Content, startLine: start, endLine: len(m.renderedLines)})
			}
		case tauchat.ChatRoleAssistant:
			if msg.Content != "" {
				flushPendingTools()
				start := len(m.renderedLines)
				m.lastAssistantText = msg.Content
				rendered := m.renderMarkdown(msg.Content)
				m.renderedLines = append(m.renderedLines, strings.Split(rendered, "\n")...)
				if msg.ID != "" {
					m.messageRanges = append(m.messageRanges, messageLineRange{id: msg.ID, content: msg.Content, startLine: start, endLine: len(m.renderedLines)})
				}
			}
			for _, tc := range msg.ToolCalls {
				toolCallsByID[tc.ID] = tc
			}
		case tauchat.ChatRoleTool:
			tc := toolCallsByID[msg.ToolCallID]
			pendingTools = append(pendingTools, toolState{
				id:     msg.ToolCallID,
				name:   tc.Function.Name,
				args:   tc.Function.Arguments,
				result: msg.Content,
				status: "done",
			})
		default:
			continue
		}
	}
	flushPendingTools()
	m.viewport.SetContentLines(m.renderedLines)
}

// finalizeResponse returns the finalized assistant text (empty if none) so
// the caller can decide whether to fire a desktop notification. id, if
// non-empty, is the just-completed assistant ChatMessage's ID (see
// lastAssistantMessageID) — recorded into messageRanges the same way
// applySnapshot records every other message's range, so this message is
// immediately right-clickable without waiting for the next snapshot rebuild.
func (m *model) finalizeResponse(id string) string {
	// A turn that ends purely on tool calls (no trailing text) still needs
	// its tools committed to scrollback — flushToolGroup renders the real
	// group instead of a synthetic placeholder, so the user sees exactly
	// what ran.
	if len(m.tools) > 0 {
		m.flushToolGroup()
	}
	content := m.streaming
	if m.reasoning != "" && content == "" {
		content = "[reasoning only]"
	}
	if content != "" {
		// Store raw markdown for /copy before glamour renders it.
		m.lastAssistantText = content
		// Render through glamour (markdown → ANSI) and append to viewport.
		// Glamour output is already fully styled, so each line goes
		// directly into renderedLines without passing through renderLine().
		start := len(m.renderedLines)
		rendered := m.renderMarkdown(content)
		m.renderedLines = append(m.renderedLines, strings.Split(rendered, "\n")...)
		if id != "" {
			m.messageRanges = append(m.messageRanges, messageLineRange{id: id, content: content, startLine: start, endLine: len(m.renderedLines)})
		}
		m.viewport.SetContentLines(m.renderedLines)
	}
	m.streaming = ""
	m.reasoning = ""
	m.inResponse = false
	return content
}

// lastAssistantMessageID returns the ID of state's final message if it's the
// assistant turn CompleteTurnWithReasoning just appended, or "" if that turn
// ended purely on tool calls (no trailing assistant text message).
func lastAssistantMessageID(state tauchat.ChatSessionState) string {
	if n := len(state.Messages); n > 0 && state.Messages[n-1].Role == tauchat.ChatRoleAssistant {
		return state.Messages[n-1].ID
	}
	return ""
}

// ensureMDRenderer creates a glamour TermRenderer for the given width in
// the cache if one doesn't already exist. Uses the "dark" bundled style;
// a custom glamour theme (glamour.WithStyles) would give full visual
// control over code blocks and headings, matching tau's theme palette.
func ensureMDRenderer(cache map[int]*glamour.TermRenderer, width int) {
	if width < 20 {
		width = 20
	}
	if _, ok := cache[width]; ok {
		return
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithWordWrap(width),
		glamour.WithStylePath("dark"),
	)
	if err != nil {
		return
	}
	cache[width] = r
}

// renderMarkdown converts raw markdown to ANSI-styled terminal output using
// a glamour renderer memoized for the current terminal width. Falls back
// to plain text when no renderer exists for the current width.
func (m *model) renderMarkdown(content string) string {
	r, ok := m.mdCache[m.width]
	if !ok || r == nil {
		return content
	}
	out, err := r.Render(content)
	if err != nil {
		return content
	}
	return out
}

// --- tool state helpers ----------------------------------------------------

func (m *model) upsertToolCall(callID, toolName, argumentsSummary string) {
	for i := range m.tools {
		if m.tools[i].id == callID {
			if toolName != "" {
				m.tools[i].name = toolName
			}
			if argumentsSummary != "" {
				m.tools[i].args += argumentsSummary
			}
			return
		}
	}
	// This tool call is new — any streaming/reasoning text accumulated so
	// far was authored before it started, so commit it now (see
	// flushStreamingText) rather than let it render after the tool group
	// that chronologically follows it.
	m.flushStreamingText()
	m.tools = append(m.tools, toolState{
		id:        callID,
		name:      toolName,
		args:      argumentsSummary,
		status:    "pending",
		startedAt: time.Now(),
	})
}

func (m *model) setToolStatus(id, status string) {
	for i := range m.tools {
		if m.tools[i].id == id {
			m.tools[i].status = status
			// Freeze the elapsed clock the moment a row settles so it reports
			// how long the call actually took, rather than continuing to tick
			// (or resetting) once the turn ends and the frame stops advancing.
			if status == "done" || status == "error" {
				m.tools[i].elapsed = time.Since(m.tools[i].startedAt)
			}
			return
		}
	}
}

// setToolResult appends a streamed output chunk (ChatToolOutputEvent) to a
// tool's live-peek result.
func (m *model) setToolResult(id, result string) {
	for i := range m.tools {
		if m.tools[i].id == id {
			m.tools[i].result += result
			return
		}
	}
}

// finalizeToolResult replaces a tool's result with its final summary
// (ChatToolExecutionCompletedEvent.ResultSummary) rather than appending —
// the summary is the authoritative final output, not an increment on top of
// whatever ChatToolOutputEvent chunks streamed in beforehand. Mirrors
// internal/tui/inline_events.go discarding the streamed tail before setting
// the resolved row's label from ResultSummary alone.
func (m *model) finalizeToolResult(id, result string) {
	for i := range m.tools {
		if m.tools[i].id == id {
			m.tools[i].result = result
			return
		}
	}
}

// appendToolTail appends a streamed output chunk to a tool's live tail buffer
// (Phase 1). Lines are split by newline and capped at tailCap (default 6)
// using a ring-buffer approach that discards the oldest lines.
func (m *model) appendToolTail(id, chunk string) {
	for i := range m.tools {
		if m.tools[i].id == id {
			t := &m.tools[i]
			if t.tailCap <= 0 {
				t.tailCap = 6
			}
			for line := range strings.SplitSeq(chunk, "\n") {
				if line == "" {
					continue
				}
				t.tailLines = append(t.tailLines, line)
				// Ring-buffer: discard oldest when over cap.
				if len(t.tailLines) > t.tailCap {
					t.tailLines = t.tailLines[len(t.tailLines)-t.tailCap:]
				}
			}
			return
		}
	}
}

// flushStreamingText commits any accumulated streaming text to scrollback
// without ending the turn. Called right before a new tool call starts: that
// text was authored before the call, so it must land in history now —
// otherwise the tool call/group that commits later (flushToolGroup) would
// render ahead of text that chronologically came first. Reasoning is
// discarded rather than persisted, matching finalizeResponse.
func (m *model) flushStreamingText() {
	if m.streaming != "" {
		rendered := m.renderMarkdown(m.streaming)
		m.renderedLines = append(m.renderedLines, strings.Split(rendered, "\n")...)
		m.viewport.SetContentLines(m.renderedLines)
	}
	m.streaming = ""
	m.reasoning = ""
}

// flushToolGroup commits the current live tool-call batch to scrollback as
// one group (see commitToolGroup) and clears it from the live/chrome list.
// Called when new streaming text resumes after a batch of tool calls, and
// at turn end for a trailing batch with no following text — both mean the
// batch is chronologically over.
func (m *model) flushToolGroup() {
	if len(m.tools) == 0 {
		return
	}
	m.commitToolGroup(m.tools, nil)
	m.viewport.SetContentLines(m.renderedLines)
	m.tools = nil
	m.focusedTool = -1
	m.expandedID = ""
}

// bashHistoryPrefix/bashHistoryInfix delimit the synthetic message format
// runBashCommand (internal/agent/coordinator.go) persists for "!" bash-mode
// commands: `fmt.Sprintf("Ran `+"`"+`%s`+"`"+`\n\n```\n%s\n```", cmd, output)`.
// Those commands run outside the normal tool-call loop, so the backend
// records them as plain ChatRoleUser text rather than a tool_call/tool
// result pair — parseBashHistoryMessage reverses that back into a command
// and output so a replayed session still shows the tool box instead of raw
// markdown text.
const (
	bashHistoryPrefix = "Ran `"
	bashHistoryInfix  = "`\n\n```\n"
	bashHistorySuffix = "\n```"
)

func parseBashHistoryMessage(content string) (cmd, output string, ok bool) {
	rest, ok := strings.CutPrefix(content, bashHistoryPrefix)
	if !ok {
		return "", "", false
	}
	cmd, body, ok := strings.Cut(rest, bashHistoryInfix)
	if !ok {
		return "", "", false
	}
	output, ok = strings.CutSuffix(body, bashHistorySuffix)
	if !ok {
		return "", "", false
	}
	return cmd, output, true
}

// renderCommittedToolsSummary renders a multi-tool-call group's folded
// (collapsed) one-line form for permanent scrollback — otherwise a turn
// with many tool calls would bury the assistant text around it (real bug: a
// 100+ tool-call turn made the preceding/following message unreachable in
// the scrollback). Tool names are deliberately omitted — with the group now
// re-openable (see committedToolGroup), the count plus a click is enough,
// and a long, deduped name list just added noise.
func renderCommittedToolsSummary(tools []toolState) string {
	errored := 0
	for _, t := range tools {
		if t.status == "error" {
			errored++
		}
	}
	glyph := "✓"
	if errored > 0 {
		glyph = "✗"
	}
	summary := fmt.Sprintf("%s %d tool calls", glyph, len(tools))
	if errored > 0 {
		summary += fmt.Sprintf(" (%d error)", errored)
	}
	return toolMetaStyle.Render(summary)
}

// renderCommittedGroup renders a committedToolGroup as it currently stands.
// Single tools use the ordinary tool box collapsed/expanded states; multi-tool
// groups fold to a one-line summary or unfold into the same per-tool-row
// accordion the live group uses (see renderToolGroupBox).
func (m *model) renderCommittedGroup(g *committedToolGroup) string {
	if len(g.tools) == 1 {
		return m.renderToolBox(g.tools[0], g.expanded, 1)
	}
	if !g.expanded {
		return renderCommittedToolsSummary(g.tools)
	}
	box, _ := m.renderToolGroupBox(g.tools, g.expandedID, -1)
	return box
}

// commitToolGroup appends tools to scrollback as permanent content and
// registers a committedToolGroup so it can still be unfolded after commit (see
// toggleCommittedToolAtLine).
// restore carries over fold/row-expand state from before a full
// applySnapshot rebuild, keyed by committedGroupKey (nil for a brand-new
// live-turn commit via flushToolGroup, which always starts folded).
func (m *model) commitToolGroup(tools []toolState, restore map[string]*committedToolGroup) {
	if len(tools) == 0 {
		return
	}
	lineIdx := len(m.renderedLines)

	g := &committedToolGroup{tools: append([]toolState(nil), tools...)}
	if old, ok := restore[committedGroupKey(tools)]; ok {
		g.expanded, g.expandedID = old.expanded, old.expandedID
	}
	rendered := m.renderCommittedGroup(g)
	g.lineIdx = lineIdx
	g.lineCount = visualLineCount(rendered)
	m.committedGroups = append(m.committedGroups, g)
	m.renderedLines = append(m.renderedLines, strings.Split(rendered, "\n")...)
}

// shouldNavigateTools returns true when tool focus navigation is appropriate:
// not in a running response, no active prompt, and input is empty (Phase 1).
func (m *model) shouldNavigateTools() bool {
	return !m.inResponse && m.activePrompt == nil && m.input == ""
}

// focusNextTool moves focusedTool to the next (delta=1) or previous (delta=-1)
// completed tool (status "done" or "error"). Wraps around when at the edges.
func (m *model) focusNextTool(delta int) {
	// Collect indices of eligible tools.
	var eligible []int
	for i, t := range m.tools {
		if t.status == "done" || t.status == "error" {
			eligible = append(eligible, i)
		}
	}
	if len(eligible) == 0 {
		m.focusedTool = -1
		return
	}

	// Find current position in eligible list and advance by delta.
	cur := -1
	for ei, ti := range eligible {
		if ti == m.focusedTool {
			cur = ei
			break
		}
	}
	newIdx := (cur + delta) % len(eligible)
	if newIdx < 0 {
		newIdx += len(eligible)
	}
	m.focusedTool = eligible[newIdx]
}

// handleMousePress maps a left-button-down terminal position to a UI
// action: pressing on the live tool-call box arms its text selection anchor
// (and focuses whichever box, if any, sits under the cursor) — the actual
// focus/expand-toggle click action fires on release, once handleMouseRelease
// knows whether the gesture turned into a drag (see toggleToolBoxAtY);
// pressing in the viewport, input box, or status bar arms that region's
// text selection anchor the same way, in case the press turns into a drag
// (see dragRegion). Recomputing the layout here (rather than caching
// View()'s geometry) keeps hit-testing correct even if the event arrives
// before the next render — cheap enough since it only runs per mouse event,
// not per frame.
func (m *model) handleMousePress(x, y int) tea.Cmd {
	// A left-click while a context menu is open resolves against the menu
	// itself — inside its bounds activates whatever's under the click,
	// outside closes it — and never also fires the region's own press
	// action underneath (e.g. re-focusing a different tool box).
	if m.contextMenu != nil {
		return m.handleContextMenuClick(x, y)
	}

	geom := m.computeLayout()

	if y >= geom.toolsStartY && y <= geom.toolsEndY {
		m.clearAllSelections()
		m.dragRegion = dragTools
		m.toolsSel.press(y)
		for _, tb := range geom.toolBoxes {
			if y >= tb.startY && y <= tb.endY {
				m.focusedTool = m.findLiveTool(tb.id)
				break
			}
		}
		return nil
	}

	switch {
	case y >= geom.viewportStartY && y <= geom.viewportEndY:
		m.clearAllSelections()
		m.focusedTool = -1
		m.expandedID = ""
		m.dragRegion = dragViewport
		row := m.viewport.YOffset() + (y - geom.viewportStartY)
		if idx, ok := m.logicalLineAtRow(row); ok {
			m.viewportSel.press(idx)
		}

	case y >= geom.inputStartY && y <= geom.inputEndY:
		m.clearAllSelections()
		m.dragRegion = dragInput
		pos := m.inputPositionAt(y-geom.inputStartY, x)
		m.inputCursor = pos
		m.inputSel.press(pos)

	case y == geom.statusY:
		m.clearAllSelections()
		m.dragRegion = dragStatus
		m.statusSel.press(x)

	default:
		m.clearAllSelections()
	}
	// The region's selectionState.dragging stays false until
	// handleMouseDrag actually sees motion — a plain click (press+release,
	// no movement) must not leave a stray highlight or copy anything.
	return nil
}

// handleContextMenuClick resolves a left-click while a context menu is open
// against the menu's clamped on-screen bounds (see clampContextMenuPosition
// — the same function compositeContextMenu uses to render it, so hit-test
// and render can never disagree about where the menu actually is). A click
// inside the menu activates the item under it; a click anywhere else closes
// the menu without performing the click's underlying region action.
func (m *model) handleContextMenuClick(x, y int) tea.Cmd {
	menuStr := renderContextMenu(m.contextMenu.items, m.contextMenu.selected)
	mx, my := m.clampContextMenuPosition(menuStr)
	mw, mh := lipgloss.Width(menuStr), lipgloss.Height(menuStr)

	if x < mx || x >= mx+mw || y < my || y >= my+mh {
		m.contextMenu = nil
		return nil
	}

	// contextMenuStyle draws a 1-cell border on every side, so the first
	// content row (item index 0) sits at my+1, not my.
	itemIdx := y - my - 1
	if itemIdx < 0 || itemIdx >= len(m.contextMenu.items) {
		m.contextMenu = nil
		return nil
	}
	return m.activateContextMenuItem(m.contextMenu.items[itemIdx])
}

// handleMouseDrag extends whichever selection dragRegion says is active to
// the position under the mouse. For the viewport, it also auto-scrolls when
// the drag reaches the top/bottom edge — the whole point of building
// selection ourselves rather than relying on the terminal's native
// (single-screen, can't-scroll) selection: a selection can now extend
// across content beyond one screen. For the input box, the drag is clamped
// to its own rows even if the mouse leaves them, matching ordinary GUI
// text-field behavior (you can't drag-select out of the field you're in).
func (m *model) handleMouseDrag(x, y int) {
	// A context menu is never a drag target — defensive guard, since a
	// menu never sets m.dragRegion itself.
	if m.contextMenu != nil {
		return
	}
	switch m.dragRegion {
	case dragViewport:
		if !m.viewportSel.armed() {
			return
		}
		geom := m.computeLayout()
		switch {
		case y <= geom.viewportStartY:
			m.viewport.ScrollUp(1)
			m.autoFollow = false
		case y >= geom.viewportEndY:
			m.viewport.ScrollDown(1)
			if m.viewport.AtBottom() {
				m.autoFollow = true
			}
		}
		rowInViewport := max(min(y-geom.viewportStartY, geom.viewportEndY-geom.viewportStartY), 0)
		row := m.viewport.YOffset() + rowInViewport
		if idx, ok := m.logicalLineAtRow(row); ok {
			m.viewportSel.drag(idx)
		}

	case dragInput:
		if !m.inputSel.armed() {
			return
		}
		geom := m.computeLayout()
		row := max(min(y, geom.inputEndY), geom.inputStartY) - geom.inputStartY
		pos := m.inputPositionAt(row, x)
		m.inputSel.drag(pos)
		m.inputCursor = pos

	case dragStatus:
		if !m.statusSel.armed() {
			return
		}
		m.statusSel.drag(x)

	case dragTools:
		if !m.toolsSel.armed() {
			return
		}
		m.toolsSel.drag(y)
	}
}

// handleMouseRelease finalizes whichever selection dragRegion says was active
// via the shared finalizeSelection path. A real drag leaves the highlight in
// place; copying is an explicit right-click action handled by
// copyActiveSelection.
func (m *model) handleMouseRelease() tea.Cmd {
	defer func() { m.dragRegion = dragNone }()

	switch m.dragRegion {
	case dragViewport:
		// A plain click (no drag) landing on a committed tool-call group is
		// the fold/unfold/row-expand action (see toggleCommittedToolAtLine),
		// not a selection — finalizeSelection's generic "no-op on click"
		// doesn't know about that, so it's checked here first. A click on
		// ordinary text still just clears the selection, same as before.
		if !m.viewportSel.dragging {
			m.toggleCommittedToolAtLine(m.viewportSel.anchor)
			m.viewportSel.clear()
			return nil
		}
		return m.finalizeSelection(&m.viewportSel, m.viewportSelectionText, "line")
	case dragInput:
		return m.finalizeSelection(&m.inputSel, m.inputSelectionText, "")
	case dragStatus:
		return m.finalizeSelection(&m.statusSel, m.statusSelectionText, "")
	case dragTools:
		// A plain click (no drag) is the pre-existing focus/expand-toggle
		// action, not a selection — finalizeSelection's generic "no-op on
		// click" doesn't know about that domain-specific behavior, so it's
		// handled here before falling back to the shared copy path for an
		// actual drag.
		if !m.toolsSel.dragging {
			m.toggleToolBoxAtY(m.toolsSel.anchor)
			m.toolsSel.clear()
			return nil
		}
		return m.finalizeSelection(&m.toolsSel, m.toolsSelectionText, "line")
	}
	return nil
}

// toggleToolBoxAtY toggles expand/collapse for whichever live tool box (from
// a fresh layout computation) contains absolute view row y — the
// click-to-expand behavior for a plain click (no drag) on the tool group.
// When the live group is collapsed, any click inside the tools area expands
// it. When expanded, a click on a tool row toggles per-tool expansion
// (existing behavior), while a click on the header/border/padding area
// collapses the whole group back to its one-line summary.
func (m *model) toggleToolBoxAtY(y int) {
	geom := m.computeLayout()

	// A click inside the tools area when the group is collapsed: expand it.
	if m.toolGroupCollapsed && y >= geom.toolsStartY && y <= geom.toolsEndY {
		m.toolGroupCollapsed = false
		return
	}

	for _, tb := range geom.toolBoxes {
		if y < tb.startY || y > tb.endY {
			continue
		}
		m.focusedTool = m.findLiveTool(tb.id)
		if m.expandedID == tb.id {
			m.expandedID = ""
		} else {
			m.expandedID = tb.id
			if realIdx := m.findLiveTool(tb.id); realIdx >= 0 {
				m.tools[realIdx].expanded = true
			}
		}
		return
	}

	// Click landed inside the tools area but on the header/border/padding —
	// collapse the whole live group to its one-line summary.
	if y >= geom.toolsStartY && y <= geom.toolsEndY {
		m.toolGroupCollapsed = true
	}
}

// toggleCommittedToolAtLine handles a plain click (no drag) landing on a
// committed tool-call group in scrollback (idx is the absolute logical line
// index from m.viewportSel's anchor — see logicalLineAtRow): folds/unfolds
// the group, or, if it's already unfolded, expands/collapses whichever tool
// row the click landed on — restoring the same accordion interaction the
// group had live, which would otherwise be lost for good the moment it
// scrolls into history (see committedToolGroup). Returns whether a group
// actually handled the click, so the caller can tell that apart from an
// ordinary click on plain text (which should just clear the selection).
func (m *model) toggleCommittedToolAtLine(idx int) bool {
	for _, g := range m.committedGroups {
		if idx < g.lineIdx || idx >= g.lineIdx+g.lineCount {
			continue
		}
		if !g.expanded {
			g.expanded = true
			m.spliceCommittedGroup(g)
			return true
		}
		if len(g.tools) == 1 {
			g.expanded = false
			m.spliceCommittedGroup(g)
			return true
		}

		// Unfolded: a click on a specific tool row toggles just that row's
		// full-detail expansion. Anything else — the header text, the
		// group's top/bottom border, the padding around a row — folds the
		// whole group back down. rows only covers the per-tool lines (see
		// renderToolGroupBox: row 0 is the border, row 1 is the header
		// text), so a click landing outside every row's range is exactly
		// "not on a tool row", which is deliberately the fold trigger
		// rather than a dead click — a header/border click is the obvious
		// place a user would click to close what they just opened.
		rel := idx - g.lineIdx
		toggledRow := false
		if _, rows := m.renderToolGroupBox(g.tools, g.expandedID, -1); rows != nil {
			for _, tb := range rows {
				if rel < tb.startY || rel > tb.endY {
					continue
				}
				if g.expandedID == tb.id {
					g.expandedID = ""
				} else {
					g.expandedID = tb.id
				}
				toggledRow = true
				break
			}
		}
		if !toggledRow {
			g.expanded = false
			g.expandedID = ""
		}
		m.spliceCommittedGroup(g)
		return true
	}
	return false
}

// spliceCommittedGroup re-renders g after toggleCommittedToolAtLine folded,
// unfolded, or expanded/collapsed one of its rows, and splices the result
// into m.renderedLines in place of its previous lines — shifting every
// other committed group AND every recorded messageRange that comes after it
// by the resulting line-count delta so their recorded positions stay
// accurate for the next click. This is the only place that mutates
// renderedLines in place after initial construction (every other write is a
// pure trailing append) — anything else added to renderedLines' bookkeeping
// in the future needs the same shift treatment here.
func (m *model) spliceCommittedGroup(g *committedToolGroup) {
	rendered := m.renderCommittedGroup(g)
	newLines := strings.Split(rendered, "\n")
	delta := len(newLines) - g.lineCount

	tail := append([]string(nil), m.renderedLines[g.lineIdx+g.lineCount:]...)
	m.renderedLines = append(m.renderedLines[:g.lineIdx], newLines...)
	m.renderedLines = append(m.renderedLines, tail...)

	g.lineCount = len(newLines)
	for _, other := range m.committedGroups {
		if other != g && other.lineIdx > g.lineIdx {
			other.lineIdx += delta
		}
	}
	for i := range m.messageRanges {
		if m.messageRanges[i].startLine > g.lineIdx {
			m.messageRanges[i].startLine += delta
			m.messageRanges[i].endLine += delta
		}
	}
	m.viewport.SetContentLines(m.renderedLines)
}

// clearAllSelections drops every region's selection — used whenever an
// interaction (a new press, a tool-box click) makes all of them stale.
func (m *model) clearAllSelections() {
	m.viewportSel.clear()
	m.inputSel.clear()
	m.statusSel.clear()
	m.toolsSel.clear()
}

// finalizeSelection is the shared "mouse released" behavior for any
// selectionState: a real drag freezes the selected range so it stays
// highlighted for an explicit right-click copy; a plain click (no drag)
// clears the selection instead of leaving a stray highlight behind.
func (m *model) finalizeSelection(s *selectionState, extract func(lo, hi int) string, _ string) tea.Cmd {
	dragging := s.dragging
	s.dragging = false
	if !dragging {
		s.clear()
		return nil
	}
	lo, hi, ok := s.bounds()
	if !ok || extract(lo, hi) == "" {
		s.clear()
	}
	return nil
}

// copyActiveSelection copies whichever region currently has a finalized
// selection. Only one selection is normally armed at a time because every
// left-button press clears all other regions first.
func (m *model) copyActiveSelection() tea.Cmd {
	if cmd := m.copySelection(&m.viewportSel, m.viewportSelectionText, "line"); cmd != nil {
		return cmd
	}
	if cmd := m.copySelection(&m.inputSel, m.inputSelectionText, ""); cmd != nil {
		return cmd
	}
	if cmd := m.copySelection(&m.statusSel, m.statusSelectionText, ""); cmd != nil {
		return cmd
	}
	return m.copySelection(&m.toolsSel, m.toolsSelectionText, "line")
}

// The OSC 52 size guard mirrors legacy taui's /copy — tea.SetClipboard
// itself has no such guard, and many terminals silently drop or corrupt
// oversized payloads rather than truncating. unit names what was selected
// for the notification ("line" -> "copied 3 lines"); pass "" for a region
// where a count isn't meaningful ("copied selection").
func (m *model) copySelection(s *selectionState, extract func(lo, hi int) string, unit string) tea.Cmd {
	lo, hi, ok := s.bounds()
	if !ok {
		return nil
	}
	text := extract(lo, hi)
	if text == "" {
		return nil
	}

	if _, ok := termkit.OSC52Copy(text); !ok {
		return m.setNotification(fmt.Sprintf("selection too large to copy (over %d chars)", termkit.OSC52MaxBytes))
	}
	notice := "copied selection"
	if unit != "" {
		n := hi - lo + 1
		u := unit
		if n != 1 {
			u += "s"
		}
		notice = fmt.Sprintf("copied %d %s", n, u)
	}
	return tea.Batch(tea.SetClipboard(text), m.setNotification(notice))
}

// highlightSelection wraps the selected range of lines (indices 0..
// len(m.renderedLines)-1 of lines, which viewportLinesForView builds from
// m.renderedLines first and unconditionally) in reverse-video SGR codes, in
// place. Reverse video composes with whatever foreground/background colors
// are already set in the styled line rather than requiring us to parse and
// re-inject them, so the line's normal styling still shows through,
// visually inverted. This only affects rendering — viewportSelectionText
// reads the unmodified m.renderedLines, so highlight quirks on complex
// multi-segment lines (rare — see comment there) never affect what gets
// copied.
func (m *model) highlightSelection(lines []string) {
	lo, hi, ok := m.viewportSel.bounds()
	if !ok {
		return
	}
	for i := lo; i <= hi && i < len(lines); i++ {
		lines[i] = reverseVideo(lines[i])
	}
}

// reverseVideoReset re-asserts reverse video after any SGR reset the line
// already contains internally — glamour ends each styled span (e.g. an
// inline-code span's colors) with a bare reset ("\x1b[m", equivalent to
// "\x1b[0m"), and a reset clears every active attribute, not just the one
// that span set. Without re-asserting, wrapping the whole line in
// "\x1b[7m...\x1b[27m" only highlights up to the line's first embedded
// reset — everything after it silently loses the reverse attribute, even
// though the whole line is genuinely selected and copies correctly (copy
// reads the unmodified, un-highlighted line via stripANSI).
var reverseVideoReset = strings.NewReplacer(
	"\x1b[m", "\x1b[m\x1b[7m",
	"\x1b[0m", "\x1b[0m\x1b[7m",
)

func reverseVideo(line string) string {
	return "\x1b[7m" + reverseVideoReset.Replace(line) + "\x1b[27m"
}

// viewportSelectionText returns the plain (ANSI-stripped) text of
// m.renderedLines[lo..hi] inclusive, for finalizeSelection.
func (m *model) viewportSelectionText(lo, hi int) string {
	if lo < 0 || hi >= len(m.renderedLines) {
		return ""
	}
	lines := make([]string, 0, hi-lo+1)
	for i := lo; i <= hi; i++ {
		lines = append(lines, stripANSI(m.renderedLines[i]))
	}
	return strings.Join(lines, "\n")
}

// toolsSelectionText returns the plain (ANSI-stripped) text of the live
// tool-call box's own lines between absolute view rows lo and hi inclusive
// (toolsSel presses/drags in that same coordinate space — see
// handleMousePress). The box isn't cached between frames like
// m.renderedLines, so this recomputes it fresh; cheap enough at
// mouse-release frequency.
func (m *model) toolsSelectionText(lo, hi int) string {
	geom := m.computeLayout()
	toolsStr, _ := m.renderToolGroup()
	if toolsStr == "" {
		return ""
	}
	boxLines := strings.Split(toolsStr, "\n")
	loRow, hiRow := lo-geom.toolsStartY, hi-geom.toolsStartY
	if loRow < 0 {
		loRow = 0
	}
	if hiRow >= len(boxLines) {
		hiRow = len(boxLines) - 1
	}
	if loRow > hiRow {
		return ""
	}
	lines := make([]string, 0, hiRow-loRow+1)
	for i := loRow; i <= hiRow; i++ {
		lines = append(lines, stripANSI(boxLines[i]))
	}
	return strings.Join(lines, "\n")
}

// wrappedRowCount estimates how many visual rows a styled line occupies
// once soft-wrapped to width, matching (approximately) how bubbles/viewport
// wraps it. This is character-width-based, not word-boundary-based like the
// viewport's actual wrap, so it can be off by a row for a long line that
// wraps near a word boundary. That only affects which whole line a
// click/drag lands on (logicalLineAtRow), never what gets copied once a
// line is selected, and most renderedLines entries are already pre-wrapped
// to width (glamour output, tool boxes) — the risk is concentrated in a
// long single-line, unwrapped user/system message.
func wrappedRowCount(line string, width int) int {
	if width < 1 {
		return 1
	}
	w := visibleWidth(stripANSI(line))
	if w == 0 {
		return 1
	}
	return (w + width - 1) / width
}

// logicalLineAtRow maps an absolute wrapped-row offset (0 = the viewport's
// first content row — the same coordinate space as m.viewport.YOffset()) to
// an index into m.renderedLines, accounting for lines that soft-wrap across
// more than one visual row (see wrappedRowCount's caveat).
func (m *model) logicalLineAtRow(targetRow int) (int, bool) {
	if targetRow < 0 {
		return 0, false
	}
	width := m.width
	if width <= 0 {
		width = 80
	}
	row := 0
	for i, line := range m.renderedLines {
		h := wrappedRowCount(line, width)
		if targetRow < row+h {
			return i, true
		}
		row += h
	}
	return 0, false
}

// messageAtRow maps an absolute wrapped-row offset (see logicalLineAtRow) to
// the id of the message whose recorded range contains it, if any. Used to
// resolve a right-click in the viewport to "which message" rather than just
// "which renderedLines index."
func (m *model) messageAtRow(row int) (string, bool) {
	idx, ok := m.logicalLineAtRow(row)
	if !ok {
		return "", false
	}
	for _, r := range m.messageRanges {
		if idx >= r.startLine && idx < r.endLine {
			return r.id, true
		}
	}
	return "", false
}

// toggleToolExpansion toggles the expanded state for the currently focused
// tool. Returns a tea.Cmd (always nil for now) to match dispatchKey's
// return signature.
func (m *model) toggleToolExpansion() tea.Cmd {
	if m.focusedTool < 0 || m.focusedTool >= len(m.tools) {
		return nil
	}
	t := &m.tools[m.focusedTool]
	if m.expandedID == t.id {
		m.expandedID = ""
	} else {
		m.expandedID = t.id
		t.expanded = true
	}
	return nil
}

// --- notification helper ---------------------------------------------------

// notificationClearDelay is the auto-dismiss duration for transient
// notifications set via setNotification. Exported as a package variable
// so tests can set it to time.Millisecond (via TestMain) and avoid a
// real 4-second sleep per test through drainCmd.
var notificationClearDelay = 0 * time.Second

// setNotification sets an info-level notification that does not auto-clear.
// For a level (drives its color) or a custom duration — e.g. an error that
// should persist until superseded rather than silently vanish — use
// setNotificationWithLevel.
func (m *model) setNotification(text string) tea.Cmd {
	return m.setNotificationWithLevel(text, notify.LevelInfo, notificationClearDelay)
}

// setNotificationWithLevel sets the notification banner (rendered above the
// input area, wrapping to as many lines as it needs — see computeLayout)
// with an explicit level and auto-clear duration, using a generation counter
// so a newer notification is not clobbered by an older timer that fires
// late (N1). duration <= 0 means the notification persists until replaced,
// rather than auto-clearing — used for errors.
func (m *model) setNotificationWithLevel(text string, level notify.Level, duration time.Duration) tea.Cmd {
	m.notificationGen++
	m.notification = text
	m.notificationLevel = level
	if duration <= 0 {
		return func() tea.Msg { return nil }
	}
	gen := m.notificationGen
	return tea.Tick(duration, func(t time.Time) tea.Msg {
		return clearNotificationMsg{gen: gen}
	})
}

// notifyLevelFromChat maps a chat notification level to the notify package
// level. Matches internal/tui/inline_events.go's notifyLevelFromChat.
func notifyLevelFromChat(level tauchat.ChatNotificationLevel) notify.Level {
	switch level {
	case tauchat.ChatNotificationError:
		return notify.LevelError
	case tauchat.ChatNotificationWarn:
		return notify.LevelWarn
	default:
		return notify.LevelInfo
	}
}

// notifyDurationFromChat returns the auto-dismiss duration for a level.
// Errors persist (0); warnings get 8s; info gets 5s. Matches
// internal/tui/inline_events.go's notifyDurationFromChat.
// notifyWarnDuration/notifyInfoDuration are package variables (not literals
// inline in notifyDurationFromChat) so tests can shrink them the same way
// TestMain shrinks notificationClearDelay — otherwise a test that drives a
// ChatNotificationEvent through Update actually waits out the real
// tea.Tick duration now that it schedules a genuine auto-clear Cmd instead
// of being handed to the old (never-rendered) notifyQ.
var (
	notifyWarnDuration = 0 * time.Second
	notifyInfoDuration = 0 * time.Second
)

func notifyDurationFromChat(level tauchat.ChatNotificationLevel) time.Duration {
	switch level {
	case tauchat.ChatNotificationError:
		return 0
	case tauchat.ChatNotificationWarn:
		return notifyWarnDuration
	default:
		return notifyInfoDuration
	}
}

// notifyReservedLines is how many rows the notification banner always
// occupies (see padOrClipLines) — enough for most messages to show in
// full without wrapping past it.
const notifyReservedLines = 2

// padOrClipLines pads or clips s to exactly n lines, so a fixed-height
// chrome region's reserved space never changes based on whether there's
// currently anything to show in it — that's what stops the viewport from
// visibly growing/shrinking every time a notification appears or clears.
// A message that doesn't fit within n lines is clipped, not wrapped
// further; that's rare (most notifications are one line) and preferable to
// letting the reserved height vary. Padding lines are a single space rather
// than empty, since visualLineCount trims trailing blank lines when
// measuring height and a single space is visually indistinguishable from
// truly empty in a terminal.
func padOrClipLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	for len(lines) < n {
		lines = append(lines, " ")
	}
	return strings.Join(lines, "\n")
}

// notifyStyleForLevel returns the notification banner's color for a level.
func notifyStyleForLevel(level notify.Level) lipgloss.Style {
	switch level {
	case notify.LevelError:
		return lipgloss.NewStyle().Foreground(themeHex(theme.ToneError)).Bold(true)
	case notify.LevelWarn:
		return lipgloss.NewStyle().Foreground(themeHex(theme.ToneWarn)).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(themeHex(theme.ToneInfo)).Bold(true)
	}
}

// --- viewport helpers ------------------------------------------------------

// appendMessage writes a styled message line to the viewport and scrolls to
// the bottom. Multi-line content is split so each visual line gets its own
// style wrapping; only the first line carries the role prefix.
func (m *model) appendMessage(role, content string) {
	if role == "assistant" {
		m.lastAssistantText = content
	}
	lines := strings.Split(content, "\n")
	if role == "tool" {
		m.renderedLines = append(m.renderedLines, lines...)
		m.viewport.SetContentLines(m.renderedLines)
		return
	}
	for i, l := range lines {
		if i == 0 {
			m.renderedLines = append(m.renderedLines, renderLine(role, l))
		} else {
			m.renderedLines = append(m.renderedLines, renderContinuationLine(role, l))
		}
	}
	m.viewport.SetContentLines(m.renderedLines)
}

// --- message cap -----------------------------------------------------------
// Removed — replaced by bubbles/viewport. Content is bounded by the terminal
// session, not an arbitrary in-memory cap. See commit history for N17 fix.

// --- rendering helpers -----------------------------------------------------

// renderLine styles a scrollback line by role. Matches taui's convention
// (internal/tui/inline_chat.go's submit echo, PrintAbove("%s %s",
// c.bold("⏎"), prompt)): a user message gets a bold return-glyph prefix, an
// assistant message gets none — neither ever gets a literal "You:"/"tau:"
// name label, which is legacy behaviour from an earlier renderer.
func renderLine(role, content string) string {
	switch role {
	case "user":
		return userStyle.Render("⏎ " + content)
	case "assistant":
		return assistantStyle.Render(content)
	default:
		return content
	}
}

func renderContinuationLine(role, content string) string {
	switch role {
	case "user":
		return userContinuationStyle.Render(content)
	case "assistant":
		return assistantContinuationStyle.Render(content)
	default:
		return continuationStyle.Render(content)
	}
}

// renderTool renders a single-line tool call summary for the inline tool
// list shown during a running turn. Full markdown-rendered tool output
// (including glamour-rendered code blocks and diffs) is handled by
// renderToolBox, which has access to the model's glamour renderer cache.
func renderTool(t toolState, frame int) string {
	style := toolStyleForStatus(t.name, t.status)

	// Build the label: tool name, or for skill tool, parse JSON args for
	// the human-readable skill name.
	label := t.name
	if t.name == "skill" {
		label = skillLabelFromArgs(t.args)
	}

	// Lead with a lifecycle glyph — use per-tool spinnerIdx (Phase 1) when
	// available, falling back to the shared frame for backward compatibility.
	spIdx := t.spinnerIdx
	if spIdx == 0 {
		spIdx = frame
	}
	line := toolGlyph(t.status, spIdx) + " " + label

	switch t.status {
	case "pending", "running":
		// A running row shows how long it's been going; a frozen number would
		// read as stuck, so this ticks with the frame.
		if !t.startedAt.IsZero() {
			line += toolMetaStyle.Render("  (" + formatElapsed(time.Since(t.startedAt)) + ")")
		}
	default:
		// N11: render a short result summary when available.
		const resultLimit = 60
		if t.result != "" {
			summary := strings.ReplaceAll(t.result, "\n", " ")
			if len(summary) > resultLimit {
				summary = summary[:resultLimit] + "…"
			}
			line += " — " + summary
		}
		if t.elapsed > 0 {
			line += toolMetaStyle.Render("  (" + formatElapsed(t.elapsed) + ")")
		}
	}
	return style.Render(line)
}

// renderToolGroup renders the current live (uncommitted) tool-call batch —
// see flushToolGroup for when it moves to permanent scrollback.
func (m *model) renderToolGroup() (string, []toolBoxGeometry) {
	if len(m.tools) == 0 {
		return "", nil
	}
	if m.toolGroupCollapsed && len(m.tools) > 1 {
		return renderLiveToolsSummary(m.tools, m.spinnerFrame), nil
	}
	return m.renderToolGroupBox(m.tools, m.expandedID, m.focusedTool)
}

// renderLiveToolsSummary renders a collapsed one-line summary for the live
// tool-call group — mirrors renderCommittedToolsSummary but keeps the live
// spinner and running/pending/error counts up-to-date.
func renderLiveToolsSummary(tools []toolState, frame int) string {
	running, pending, errored := 0, 0, 0
	for _, t := range tools {
		switch t.status {
		case "running":
			running++
		case "pending":
			pending++
		case "error":
			errored++
		}
	}
	glyph := toolGlyph("running", frame)
	if running+pending == 0 {
		if errored > 0 {
			glyph = "✗"
		} else {
			glyph = "✓"
		}
	}
	summary := fmt.Sprintf("%s %d tool calls", glyph, len(tools))
	total := running + pending
	if total > 0 {
		if pending > 0 {
			summary += fmt.Sprintf(" · %d pending", pending)
		}
		if running > 0 {
			summary += fmt.Sprintf(" · %d running", running)
		}
	}
	if errored > 0 {
		summary += fmt.Sprintf(" · %d error", errored)
	}
	return toolGroupSummaryStyle.Render(summary)
}

// maxToolRowsVisible is the most per-tool rows renderToolGroupBox will show
// inside a group before windowing — prevents a turn with 40+ tool calls from
// pushing everything off-screen. Overflowed rows get an "↑ N more" indicator.
const maxToolRowsVisible = 8

// renderToolGroupBox renders any batch of tool calls — live or a committed
// group reopened from scrollback (see committedToolGroup) — as one unit. A
// lone tool keeps its full interactive box (matches the pre-grouping look);
// multiple tools render as one bordered group — a header summary line, then
// each tool as a compact one-line row, except the tool whose id matches
// expandedID, which renders its full box in place of its row. focusedIdx
// draws the keyboard-focus marker on that row (pass -1 for a committed
// group, which has no live keyboard focus). Returns the rendered string
// plus each *visible* tool's row range (relative to the string's own first
// line, i.e. row 0 is the box's top border) for mouse hit-testing.
//
// When there are more tools than maxToolRowsVisible, the box windows to show
// the last N rows (most-recent calls at the bottom) and prepends an
// "↑ N more" overflow indicator so the user knows there's more above.
func (m *model) renderToolGroupBox(tools []toolState, expandedID string, focusedIdx int) (string, []toolBoxGeometry) {
	if len(tools) == 1 {
		t := tools[0]
		expanded := expandedID != "" && expandedID == t.id
		box := m.renderToolBox(t, expanded, 1)
		return box, []toolBoxGeometry{{id: t.id, startY: 0, endY: visualLineCount(box) - 1}}
	}

	running, errored, pending := 0, 0, 0
	for _, t := range tools {
		switch t.status {
		case "pending":
			pending++
		case "running":
			running++
		case "error":
			errored++
		}
	}
	header := fmt.Sprintf("%d tool calls", len(tools))
	if running > 0 || pending > 0 {
		if pending > 0 {
			header += fmt.Sprintf(" · %d pending", pending)
		}
		if running > 0 {
			header += fmt.Sprintf(" · %d running", running)
		}
	}
	if errored > 0 {
		header += fmt.Sprintf(" · %d error", errored)
	}

	n := len(tools)
	visibleCount := n
	start := 0
	if visibleCount > maxToolRowsVisible {
		visibleCount = maxToolRowsVisible
		start = n - visibleCount
	}

	lines := []string{toolGroupHeaderStyle.Render(header)}
	rows := make([]toolBoxGeometry, 0, visibleCount)
	row := 2 // top border row (0) + header row (1)

	// Overflow indicator for windowed groups.
	if start > 0 {
		overflow := toolGroupOverflowStyle.Render(fmt.Sprintf("  ↑ %d more", start))
		lines = append(lines, overflow)
		row++ // account for the overflow line
	}

	for i := start; i < n; i++ {
		t := tools[i]
		upperI := i // relative to the full slice, for focusedIdx comparison
		expanded := expandedID != "" && expandedID == t.id
		var line string
		if expanded {
			line = m.renderToolBox(t, true, len(tools))
		} else {
			marker := "  "
			if focusedIdx == upperI {
				marker = toolFocusMarkerStyle.Render("› ")
			}
			line = marker + renderTool(t, m.spinnerFrame)
		}
		h := visualLineCount(line)
		rows = append(rows, toolBoxGeometry{id: t.id, startY: row, endY: row + h - 1})
		row += h
		lines = append(lines, line)
	}

	width := m.width
	if width < 20 {
		width = 80
	}
	box := toolGroupBoxStyle.Width(width).Padding(0, 1).Render(strings.Join(lines, "\n"))
	return box, rows
}

// renderToolBox renders a complete background-colored tool box with borders,
// glyph, status, elapsed clock, live tail output, and full result when
// expanded (Phase 1).
func (m *model) renderToolBox(t toolState, expanded bool, _ int) string {
	label := t.name
	if t.name == "skill" {
		label = skillLabelFromArgs(t.args)
	}

	glyph := toolGlyph(t.status, t.spinnerIdx)
	var elapsed time.Duration
	if t.status == "pending" || t.status == "running" {
		elapsed = time.Since(t.startedAt)
	} else {
		elapsed = t.elapsed
	}

	elapsedStr := ""
	if !t.startedAt.IsZero() || elapsed > 0 {
		elapsedStr = " (" + formatElapsed(elapsed) + ")"
	}

	// Title line content: glyph + name + elapsed. For pending/running, the
	// status word ("pending" vs "running") is real information the shared
	// spinner glyph alone doesn't distinguish. For done/error, the glyph
	// and box color (green/red) already say everything — repeating "done"
	// or "error" as text is redundant log-speak, not state.
	statusWord := ""
	if t.status == "pending" || t.status == "running" {
		statusWord = " " + t.status
	}
	title := glyph + " " + label + statusWord + elapsedStr

	// Pick the style based on status.
	boxStyle := toolBoxStyleForStatus(t.name, t.status)
	if expanded {
		boxStyle = toolBoxExpandedStyle
	}
	focused := m.focusedTool >= 0 && m.focusedTool < len(m.tools) && m.tools[m.focusedTool].id == t.id
	if focused {
		boxStyle = boxStyle.BorderForeground(themeHex(theme.CommandFG)).Bold(true)
	}

	width := m.width
	if width < 20 {
		width = 80
	}
	boxStyle = boxStyle.Width(width).Padding(0, 1)

	// Build body lines.
	var bodyLines []string

	if expanded {
		// Expanded mode: show full result content in an inner box.
		// When the output looks like markdown (code blocks, lists,
		// tables), render it through glamour for syntax highlight.
		bodyLines = append(bodyLines, "")
		innerWidth := max(
			// inner box narrower than outer
			width-8, 10,
		)
		innerStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(themeHex(theme.ToneMuted)).
			Width(innerWidth).
			Padding(0, 1)
		var innerContent strings.Builder
		if len(t.result) == 0 {
			innerContent.WriteString("No output\n")
		}

		if looksLikeMarkdown(t.result) {
			// Wrap as a fenced markdown block and render through
			// the width-memoized glamour cache for syntax highlight.
			var mdBuilder strings.Builder
			mdBuilder.Grow(len(t.result) + len("```result.md\n\n```"))
			mdBuilder.WriteString("```result.md\n")
			mdBuilder.WriteString(t.result)
			mdBuilder.WriteString("\n```")
			md := mdBuilder.String()
			if r, ok := m.mdCache[innerWidth]; ok && r != nil {
				if out, err := r.Render(md); err == nil {
					for line := range strings.SplitSeq(out, "\n") {
						innerContent.WriteString(line)
						innerContent.WriteByte('\n')
					}
					innerRendered := innerStyle.Render(strings.TrimRight(innerContent.String(), "\n"))
					for line := range strings.SplitSeq(innerRendered, "\n") {
						bodyLines = append(bodyLines, line)
					}
					bodyLines = append(bodyLines, toolMetaStyle.Render("Press Enter to collapse"))
					content := title + "\n" + strings.Join(bodyLines, "\n")
					return boxStyle.Render(content)
				}
			}
			// Fall through to plain-text rendering if glamour failed.
		}

		resultLines := strings.SplitSeq(t.result, "\n")
		for line := range resultLines {
			innerContent.WriteString(line)
			innerContent.WriteByte('\n')
		}
		// Render inner box and append each line to body.
		innerRendered := innerStyle.Render(strings.TrimRight(innerContent.String(), "\n"))
		for line := range strings.SplitSeq(innerRendered, "\n") {
			bodyLines = append(bodyLines, line)
		}
		bodyLines = append(bodyLines, toolMetaStyle.Render("Press Enter to collapse"))
	} else {
		// Compact mode: show first tail line if any, otherwise truncated result summary.
		detail := ""
		if len(t.tailLines) > 0 && (t.status == "pending" || t.status == "running") {
			detail = t.tailLines[len(t.tailLines)-1]
		} else if t.result != "" {
			const resultLimit = 60
			summary := strings.ReplaceAll(t.result, "\n", " ")
			if len(summary) > resultLimit {
				summary = summary[:resultLimit] + "…"
			}
			detail = summary
		}
		if detail != "" {
			bodyLines = append(bodyLines, detail)
		}
	}

	// Build content: title on first line, body on subsequent lines.
	content := title
	if len(bodyLines) > 0 {
		content += "\n" + strings.Join(bodyLines, "\n")
	}
	return boxStyle.Render(content)
}

// looksLikeMarkdown reports whether content contains markdown syntax markers
// that glamour would meaningfully render. A lightweight heuristic — false
// positives are harmless (empty glamour render), false negatives leave
// plain text which is already the default.
func looksLikeMarkdown(content string) bool {
	patterns := []string{
		"# ", "## ", "**", "```", // headings, bold, code fences
		"- ", "1. ", "> ", // lists, blockquotes
		"---", "***", // horizontal rules
	}
	for _, p := range patterns {
		if strings.Contains(content, p) {
			return true
		}
	}
	return false
}

// toolStyleForStatus returns the lipgloss style for a tool's current lifecycle
// state — warm peach for pending/running, green for done, red for error —
// with lilac variants for the Skill tool to keep it visually distinct.
func toolStyleForStatus(toolName, status string) lipgloss.Style {
	skill := toolName == "skill"
	switch status {
	case "done":
		if skill {
			return skillSuccessStyle
		}
		return toolSuccessStyle
	case "error":
		if skill {
			return skillFailedStyle
		}
		return toolErrorStyle
	default: // pending, running
		if skill {
			return skillRunningStyle
		}
		return toolRunningStyle
	}
}

// toolBoxStyleForStatus returns the lipgloss style for a tool box (Phase 1)
// with background color, rounded border, and per-status coloring.
func toolBoxStyleForStatus(toolName, status string) lipgloss.Style {
	skill := toolName == "skill"
	switch status {
	case "done":
		if skill {
			return toolBoxSkillSuccessStyle
		}
		return toolBoxSuccessStyle
	case "error":
		if skill {
			return toolBoxSkillFailedStyle
		}
		return toolBoxErrorStyle
	default: // pending, running
		if skill {
			return toolBoxSkillRunningStyle
		}
		return toolBoxRunningStyle
	}
}

// skillLabelFromArgs extracts a human-readable skill name from tool arguments
// (JSON object with a "name": key). Falls back to "skill" when unparseable.
func skillLabelFromArgs(args string) string {
	// Look for "name": followed by a JSON string value and extract it.
	// Search for the exact key delimiter to avoid matching "no-name" etc.
	key := `","name":"`
	// Also try at the start of the object, right after the opening brace.
	for _, prefix := range []string{`{"name":"`, key} {
		if _, after, ok := strings.Cut(args, prefix); ok {
			rest := after
			if before, _, ok := strings.Cut(rest, "\""); ok {
				return "skill: " + before
			}
		}
	}
	return "skill"
}

// sessionSummariesText renders the /session list output — mirrors
// internal/tui/inline_events.go's printSessionSummaries.
func sessionSummariesText(summaries []tauchat.SessionSummary, nextCursor string) string {
	if len(summaries) == 0 {
		return "Sessions: no saved sessions"
	}
	var b strings.Builder
	b.WriteString("Sessions:")
	for _, s := range summaries {
		fmt.Fprintf(&b, "\n- %s · %d messages · %s", s.ID, s.MessageCount, s.ModelID)
		if line := sessionSummaryMetricsLine(s); line != "" {
			fmt.Fprintf(&b, "\n  %s", line)
		}
	}
	if nextCursor != "" {
		b.WriteString("\nMore sessions available.")
	}
	return b.String()
}

// sessionInfoText renders full detail for a single session (/session info
// <id>) — mirrors internal/tui/inline_events.go's printSessionInfo.
func sessionInfoText(s tauchat.SessionSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Session %s\n", s.ID)
	fmt.Fprintf(&b, "Model: %s\n", s.ModelID)
	fmt.Fprintf(&b, "Provider: %s\n", s.Provider)
	fmt.Fprintf(&b, "Messages: %d", s.MessageCount)
	if s.TotalTokens > 0 {
		fmt.Fprintf(&b, "\nTokens: ↑%s ↓%s (total %s)",
			humanizeTokens(s.InputTokens), humanizeTokens(s.OutputTokens), humanizeTokens(s.TotalTokens))
	}
	if s.Cost > 0 {
		fmt.Fprintf(&b, "\nCost: %s", formatCost(s.Cost))
	}
	if s.DurationMs > 0 {
		fmt.Fprintf(&b, "\nDuration: %s", formatDurationCompact(s.DurationMs))
	}
	if s.ToolCalls > 0 {
		fmt.Fprintf(&b, "\nTool calls: %d", s.ToolCalls)
		if s.ToolErrors > 0 {
			fmt.Fprintf(&b, " (%d errors)", s.ToolErrors)
		}
	}
	fmt.Fprintf(&b, "\nCreated: %s", s.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "\nUpdated: %s", s.UpdatedAt.Format(time.RFC3339))
	return b.String()
}

// skillsChangedText renders the formatted skill catalog (name, description,
// scope) shown on SkillsChangedEvent — mirrors
// internal/tui/inline_events.go's handleSkillsChanged.
func skillsChangedText(skills []tauchat.SkillInfo) string {
	if len(skills) == 0 {
		return "no skills available"
	}
	var b strings.Builder
	b.WriteString("Available Skills:")
	for _, skill := range skills {
		fmt.Fprintf(&b, "\n  %-20s %s", skill.Name, skill.Description)
		if skill.Scope != "" {
			fmt.Fprintf(&b, " (%s)", skill.Scope)
		}
	}
	return b.String()
}

// sessionSummaryMetricsLine builds a compact single-line metrics summary for
// a session entry when at least one metric field is non-zero. Mirrors
// internal/tui/inline_events.go's function of the same name.
func sessionSummaryMetricsLine(s tauchat.SessionSummary) string {
	var parts []string
	if s.InputTokens > 0 || s.OutputTokens > 0 {
		parts = append(parts, "↑"+humanizeTokens(s.InputTokens)+" ↓"+humanizeTokens(s.OutputTokens))
	}
	if s.Cost > 0 {
		parts = append(parts, formatCost(s.Cost))
	}
	if s.ToolCalls > 0 {
		toolStr := fmt.Sprintf("%d tools", s.ToolCalls)
		if s.ToolErrors > 0 {
			toolStr += fmt.Sprintf(" (%d err)", s.ToolErrors)
		}
		parts = append(parts, toolStr)
	}
	if s.DurationMs > 0 {
		parts = append(parts, formatDurationCompact(s.DurationMs))
	}
	return strings.Join(parts, " · ")
}

// formatDurationCompact renders a millisecond duration compactly (e.g.
// "450ms", "3s", "2m", "1h 5m"). Mirrors internal/tui/inline_events.go's
// function of the same name.
func formatDurationCompact(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", ms)
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh %dm", h, m)
	}
}

// --- tea.Msg types ---------------------------------------------------------

// chatEventMsg wraps a ChatEvent for delivery to the Bubbletea update loop.
type chatEventMsg struct {
	event tauchat.ChatEvent
}

// tickMsg is delivered by tea.Tick to drive timed animations (spinner,
// steering dots). Each tick bumps the spinner frame and returns another
// tick while the model is inResponse.
type tickMsg struct {
	t time.Time
}

// chatEventsClosedMsg is delivered when the subscriber channel closes, either
// from subscriber.Done() or from the events channel itself closing (N12).
type chatEventsClosedMsg struct{}

// clearNotificationMsg clears the notification only if the generation counter
// matches, preventing a stale timer from clearing a newer notification (N1).
type clearNotificationMsg struct {
	gen int
}

// sendResultMsg carries the result of a runtime.Send call (N5).
type sendResultMsg struct {
	err error
}

// bashSendResultMsg carries the result of sending a RunBashCommand.
type bashSendResultMsg struct {
	err error
}

// startupMsg is sent from Init to set initial display state.
type startupMsg struct {
	sessionID string
	modelName string
	provider  string
}

// --- tea.Cmd constructors --------------------------------------------------

// readNextEvent returns a tea.Cmd that blocks on the next event from the bus
// subscriber, delivering it as a chatEventMsg. Uses select against sub.Done()
// so the goroutine does not leak when the subscriber is closed (N4, N12).
func readNextEvent(sub *eventbus.Subscriber[tauchat.ChatEvent]) tea.Cmd {
	return func() tea.Msg {
		select {
		case evt, ok := <-sub.Events():
			if !ok {
				return chatEventsClosedMsg{}
			}
			return chatEventMsg{event: evt}
		case <-sub.Done():
			return chatEventsClosedMsg{}
		}
	}
}

// sendCommand returns a tea.Cmd that sends a ChatCommand to the coordinator
// and delivers the result (including any error) as a sendResultMsg (N5).
func sendCommand(runtime tauchat.ChatRuntime, cmd tauchat.ChatCommand) tea.Cmd {
	return func() tea.Msg {
		return sendResultMsg{err: runtime.Send(cmd)}
	}
}

// sendBashCommand sends a RunBashCommand and delivers the result as a
// bashSendResultMsg — kept distinct from sendResultMsg so a failed send can
// clear bashRunning/bashCallID specifically, without misreading an unrelated
// in-flight chat turn as failed (or vice versa).
func sendBashCommand(runtime tauchat.ChatRuntime, cmd tauchat.RunBashCommand) tea.Cmd {
	return func() tea.Msg {
		return bashSendResultMsg{err: runtime.Send(cmd)}
	}
}

// --- context menu -----------------------------------------------------------

// openContextMenuAt resolves the element under (x, y) — a live tool box or a
// committed tool group/row (chat messages are added in a later change via
// messageAtRow) — and opens a context menu for it. Mirrors handleMousePress's
// region dispatch but queries state rather than mutating it. A click on
// input/status/empty space, or on nothing resolvable, leaves the menu
// closed (silent no-op), matching how right-click on those regions is
// already a no-op today.
func (m *model) openContextMenuAt(x, y int) {
	geom := m.computeLayout()

	if y >= geom.toolsStartY && y <= geom.toolsEndY {
		for _, tb := range geom.toolBoxes {
			if y < tb.startY || y > tb.endY {
				continue
			}
			m.contextMenu = m.buildLiveToolContextMenu(tb.id, x, y)
			return
		}
		return
	}

	if y >= geom.viewportStartY && y <= geom.viewportEndY {
		row := m.viewport.YOffset() + (y - geom.viewportStartY)
		idx, ok := m.logicalLineAtRow(row)
		if !ok {
			return
		}
		if cm := m.buildCommittedToolContextMenu(idx, x, y); cm != nil {
			m.contextMenu = cm
			return
		}
		if id, ok := m.messageAtRow(row); ok {
			m.contextMenu = m.buildMessageContextMenu(id, x, y)
			return
		}
		return
	}
	// input/status/empty space: no target, menu stays closed.
}

// buildMessageContextMenu builds a menu for a right-click on a chat message.
func (m *model) buildMessageContextMenu(id string, x, y int) *contextMenu {
	return &contextMenu{
		target:   contextMenuTargetMessage,
		targetID: id,
		x:        x,
		y:        y,
		items: []contextMenuItem{
			{label: "Copy", action: contextMenuActionCopy},
		},
	}
}

// buildLiveToolContextMenu builds a menu for a right-click on the live
// (uncommitted) tool batch's tool with the given id.
func (m *model) buildLiveToolContextMenu(id string, x, y int) *contextMenu {
	if i := m.findLiveTool(id); i < 0 {
		return nil
	}
	expandLabel := "Expand"
	if m.expandedID == id {
		expandLabel = "Collapse"
	}
	return &contextMenu{
		target:   contextMenuTargetTool,
		targetID: id,
		x:        x,
		y:        y,
		items: []contextMenuItem{
			{label: "Copy output", action: contextMenuActionCopy},
			{label: expandLabel, action: contextMenuActionToggleExpand},
		},
	}
}

// buildCommittedToolContextMenu resolves renderedLines index idx against
// m.committedGroups and builds the appropriate menu — a single row's menu if
// idx lands on a specific tool row within an unfolded multi-tool group, or
// the whole group's menu otherwise. Mirrors toggleCommittedToolAtLine's same
// fold/row-expand precedence exactly, so right-click and left-click always
// agree on what's "under" a given line. Returns nil if idx isn't inside any
// committed group.
func (m *model) buildCommittedToolContextMenu(idx, x, y int) *contextMenu {
	for _, g := range m.committedGroups {
		if idx < g.lineIdx || idx >= g.lineIdx+g.lineCount {
			continue
		}
		if g.expanded && len(g.tools) > 1 {
			rel := idx - g.lineIdx
			if _, rows := m.renderToolGroupBox(g.tools, g.expandedID, -1); rows != nil {
				for _, tb := range rows {
					if rel < tb.startY || rel > tb.endY {
						continue
					}
					return m.buildToolRowContextMenu(g, tb.id, x, y)
				}
			}
		}
		return m.buildCommittedGroupContextMenu(g, x, y)
	}
	return nil
}

// buildToolRowContextMenu builds a menu for one tool row inside an unfolded
// committed group.
func (m *model) buildToolRowContextMenu(g *committedToolGroup, toolID string, x, y int) *contextMenu {
	expandLabel := "Expand"
	if g.expandedID == toolID {
		expandLabel = "Collapse"
	}
	return &contextMenu{
		target:   contextMenuTargetToolRow,
		targetID: toolID,
		x:        x,
		y:        y,
		items: []contextMenuItem{
			{label: "Copy output", action: contextMenuActionCopy},
			{label: expandLabel, action: contextMenuActionToggleExpand},
		},
	}
}

// buildCommittedGroupContextMenu builds a menu for a whole committed group
// (folded, or a click that landed outside every row of an unfolded group —
// the header/border, same as the fold trigger). Uses the group's first
// tool's id as the menu's stable identity — findCommittedGroupWithTool
// resolves it back to this same group at activation time. Groups always
// have at least one tool.
func (m *model) buildCommittedGroupContextMenu(g *committedToolGroup, x, y int) *contextMenu {
	expandLabel := "Expand"
	if g.expanded {
		expandLabel = "Collapse"
	}
	return &contextMenu{
		target:   contextMenuTargetTool,
		targetID: g.tools[0].id,
		x:        x,
		y:        y,
		items: []contextMenuItem{
			{label: "Copy output", action: contextMenuActionCopy},
			{label: expandLabel, action: contextMenuActionToggleExpand},
		},
	}
}

// findLiveTool returns the index of the live (uncommitted) tool with the
// given id, or -1.
func (m *model) findLiveTool(id string) int {
	for i, t := range m.tools {
		if t.id == id {
			return i
		}
	}
	return -1
}

// findCommittedGroupWithTool returns the committed group containing a tool
// with the given id, or nil. A tool id is never simultaneously live and
// committed — flushToolGroup/commitToolGroup always clear m.tools at the
// same time a batch is committed — so live and committed lookups never
// collide.
func (m *model) findCommittedGroupWithTool(id string) *committedToolGroup {
	for _, g := range m.committedGroups {
		for _, t := range g.tools {
			if t.id == id {
				return g
			}
		}
	}
	return nil
}

// handleContextMenuKey handles keyboard input while a context menu is open.
// Up/Down wrap (matches focusNextTool's wraparound — a menu conventionally
// wraps, unlike the completions dropdown's clamped window). Enter activates
// the selected item and closes the menu. Esc performs a real, unconditional
// dismiss — unlike the completions dropdown's swallow-without-closing, this
// is an explicit modal the user deliberately opened. Every other key is
// swallowed while the menu is open rather than falling through to input
// editing.
func (m *model) handleContextMenuKey(msg tea.KeyPressMsg) tea.Cmd {
	n := len(m.contextMenu.items)
	if n == 0 {
		m.contextMenu = nil
		return nil
	}
	switch msg.String() {
	case "up":
		m.contextMenu.selected = (m.contextMenu.selected - 1 + n) % n
	case "down":
		m.contextMenu.selected = (m.contextMenu.selected + 1) % n
	case "enter":
		return m.activateContextMenuItem(m.contextMenu.items[m.contextMenu.selected])
	case "esc":
		m.contextMenu = nil
	}
	return nil
}

// activateContextMenuItem performs item's action against the menu's current
// target and closes the menu. Re-resolves the target by id rather than
// caching a pointer/index at open time, so an action taken a while after
// the menu opened (or from a click, once click-to-activate lands) still
// acts on live state.
func (m *model) activateContextMenuItem(item contextMenuItem) tea.Cmd {
	cm := m.contextMenu
	m.contextMenu = nil
	if cm == nil {
		return nil
	}
	switch cm.target {
	case contextMenuTargetTool:
		return m.activateToolContextAction(cm.targetID, item.action)
	case contextMenuTargetToolRow:
		return m.activateToolRowContextAction(cm.targetID, item.action)
	case contextMenuTargetMessage:
		return m.activateMessageContextAction(cm.targetID, item.action)
	}
	return nil
}

// activateMessageContextAction performs action against the message
// identified by id, looked up in messageRanges for its raw (unstyled)
// content — Copy is currently the only message action.
func (m *model) activateMessageContextAction(id string, action contextMenuAction) tea.Cmd {
	if action != contextMenuActionCopy {
		return nil
	}
	for _, r := range m.messageRanges {
		if r.id == id {
			return m.copyText(r.content)
		}
	}
	return nil
}

// activateToolContextAction performs action against either the live tool or
// the whole committed group identified by id — whichever collection
// currently contains it (see findLiveTool/findCommittedGroupWithTool).
func (m *model) activateToolContextAction(id string, action contextMenuAction) tea.Cmd {
	if i := m.findLiveTool(id); i >= 0 {
		switch action {
		case contextMenuActionCopy:
			return m.copyText(m.tools[i].result)
		case contextMenuActionToggleExpand:
			if m.expandedID == id {
				m.expandedID = ""
			} else {
				m.expandedID = id
				m.tools[i].expanded = true
			}
		}
		return nil
	}
	if g := m.findCommittedGroupWithTool(id); g != nil {
		switch action {
		case contextMenuActionCopy:
			results := make([]string, len(g.tools))
			for i, t := range g.tools {
				results[i] = t.result
			}
			return m.copyText(strings.Join(results, "\n"))
		case contextMenuActionToggleExpand:
			g.expanded = !g.expanded
			if !g.expanded {
				g.expandedID = ""
			}
			m.spliceCommittedGroup(g)
		}
	}
	return nil
}

// activateToolRowContextAction performs action against one tool row within
// its committed group, identified by the row's own tool id.
func (m *model) activateToolRowContextAction(id string, action contextMenuAction) tea.Cmd {
	g := m.findCommittedGroupWithTool(id)
	if g == nil {
		return nil
	}
	switch action {
	case contextMenuActionCopy:
		for _, t := range g.tools {
			if t.id == id {
				return m.copyText(t.result)
			}
		}
	case contextMenuActionToggleExpand:
		if g.expandedID == id {
			g.expandedID = ""
		} else {
			g.expandedID = id
		}
		m.spliceCommittedGroup(g)
	}
	return nil
}

// copyText copies text to the clipboard via OSC 52, matching copySelection's
// size-guard and notification pattern — context-menu Copy actions have no
// selectionState/bounds of their own to route through copySelection itself.
func (m *model) copyText(text string) tea.Cmd {
	if text == "" {
		return m.setNotification("nothing to copy")
	}
	if _, ok := termkit.OSC52Copy(text); !ok {
		return m.setNotification(fmt.Sprintf("selection too large to copy (over %d chars)", termkit.OSC52MaxBytes))
	}
	return tea.Batch(tea.SetClipboard(text), m.setNotification("copied to clipboard"))
}

// activePanel returns the first active plugin panel (if any).
func (m *model) activePanel() *pluginPanel {
	for _, p := range m.panels {
		return &p
	}
	return nil
}

// --- rendering helpers -----------------------------------------------------

// renderCompletions draws the dropdown: a scrolling window (so a selection
// past the visible window is never invisible/unreachable), group headers,
// a chevron + bold-highlighted matched characters on the selected row, and
// a description column aligned across the visible rows. Mirrors
// pkg/taui/completions.go's Render.
func renderCompletions(rows []compRow, selected, width int) string {
	const window = 10
	n := len(rows)
	size := min(n, window)
	start := max(selected-size/2, 0)
	if start+size > n {
		start = n - size
	}
	end := start + size

	descCol := completionDescColumn(rows)

	var out []string
	lastGroup := ""
	for i := start; i < end; i++ {
		row := rows[i]
		if row.group != lastGroup {
			out = append(out, compTitleStyle.Render(row.group))
			lastGroup = row.group
		}
		out = append(out, renderCompletionRow(row, i == selected, descCol))
	}
	if start > 0 {
		out = append([]string{compMoreStyle.Render(fmt.Sprintf("  ↑ %d more", start))}, out...)
	}
	if remaining := n - end; remaining > 0 {
		out = append(out, compMoreStyle.Render(fmt.Sprintf("  ↓ %d more", remaining)))
	}

	if width > 0 {
		for i := range out {
			out[i] = truncateANSIToWidth(out[i], width, "…")
		}
	}
	return strings.Join(out, "\n")
}

// completionDescColumn returns the width to pad words to so descriptions
// line up in a column — the widest word among visible rows that carry a
// description, capped so an outlier doesn't push the column off-screen.
func completionDescColumn(rows []compRow) int {
	const maxCol = 44
	col := 0
	for _, r := range rows {
		if r.Description == "" {
			continue
		}
		if w := visibleWidth(r.Word); w > col {
			col = w
		}
	}
	return min(col, maxCol)
}

func renderCompletionRow(row compRow, selected bool, descCol int) string {
	chevron := "  "
	wordStyle := compItemStyle
	if selected {
		chevron = "▶ "
		wordStyle = compSelectedStyle
	}

	word := renderHighlightedWord(row.Word, row.highlight, wordStyle)
	line := chevron + word
	if row.Description != "" {
		pad := descCol - visibleWidth(row.Word)
		if pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		line += compDescStyle.Render(" " + row.Description)
	}
	return line
}

// renderHighlightedWord renders word with base applied throughout and
// compHighlightStyle layered on top of the matched rune spans (bold, so a
// fuzzy match's relevance is visible at a glance, same as taui's dropdown).
func renderHighlightedWord(word string, spans [][2]int, base lipgloss.Style) string {
	if len(spans) == 0 {
		return base.Render(word)
	}
	runes := []rune(word)
	var sb strings.Builder
	si := 0
	for i := 0; i < len(runes); {
		if si < len(spans) && i == spans[si][0] {
			end := min(spans[si][1], len(runes))
			sb.WriteString(compHighlightStyle.Render(string(runes[i:end])))
			i = end
			si++
			continue
		}
		next := len(runes)
		if si < len(spans) {
			next = spans[si][0]
		}
		sb.WriteString(base.Render(string(runes[i:next])))
		i = next
	}
	return sb.String()
}

// --- styles (N8: sourced from internal/theme colors, converted to hex) -----

// themeHex converts a termkit.Color RGB triple to a lipgloss-compatible color.
func themeHex(c termkit.Color) color.Color {
	return lipgloss.Color(fmt.Sprintf("#%02X%02X%02X", c[0], c[1], c[2]))
}

var (
	// Semantic colors sourced from internal/theme.
	userColor      = themeHex(theme.CommandFG)
	assistantColor = themeHex(theme.ToolSuccess.FG)
	reasoningColor = themeHex(theme.ToneWarn)
	streamColor    = themeHex(theme.TonePrimary)
	inputColor     = themeHex(theme.ShimmerHighlight)

	userStyle      = lipgloss.NewStyle().Foreground(userColor)
	assistantStyle = lipgloss.NewStyle().Foreground(assistantColor)
	reasoningStyle = lipgloss.NewStyle().Foreground(reasoningColor).Italic(true)
	streamStyle    = lipgloss.NewStyle().Foreground(streamColor)
	inputStyle     = lipgloss.NewStyle().Foreground(inputColor)

	userContinuationStyle      = lipgloss.NewStyle().Foreground(userColor).PaddingLeft(6)
	assistantContinuationStyle = lipgloss.NewStyle().Foreground(assistantColor).PaddingLeft(6)
	continuationStyle          = lipgloss.NewStyle().Foreground(themeHex(theme.ToneMuted)).PaddingLeft(6)

	// inputCursorStyle is the block-cursor background — matches the default
	// mid-grey pkg/taui/lineinput.go uses (\x1b[48;2;128;134;150m).
	// #808696 = R=128 G=134 B=150 = theme.ToneMuted.
	inputCursorStyle = lipgloss.NewStyle().Background(themeHex(theme.ToneMuted))

	// Prompt / completion styles.
	promptTitleStyle     = lipgloss.NewStyle().Bold(true).Foreground(themeHex(theme.ShimmerHighlight))
	promptTextStyle      = lipgloss.NewStyle().Foreground(themeHex(theme.TonePrimary))
	promptHintStyle      = lipgloss.NewStyle().Foreground(themeHex(theme.ToneMuted)).Italic(true)
	promptHighlightStyle = lipgloss.NewStyle().Bold(true).Underline(true)
	compTitleStyle       = lipgloss.NewStyle().Foreground(themeHex(theme.ToneMuted)).Bold(true)
	compItemStyle        = lipgloss.NewStyle().Foreground(themeHex(theme.ToneBody))
	compSelectedStyle    = lipgloss.NewStyle().Foreground(themeHex(theme.CommandFG)).Bold(true)
	compHighlightStyle   = lipgloss.NewStyle().Foreground(themeHex(theme.TonePrimary)).Bold(true).Underline(true)
	compDescStyle        = lipgloss.NewStyle().Foreground(themeHex(theme.ToneMuted))
	compMoreStyle        = lipgloss.NewStyle().Foreground(themeHex(theme.ToneMuted)).Italic(true)
	panelStyle           = lipgloss.NewStyle().Foreground(themeHex(theme.CommandFG))

	// Muted trailing metadata — the elapsed clock and interrupt hint on the
	// working indicator, and the per-tool elapsed suffix. Kept dim so the
	// verb / tool name stays the focus and the timing reads as ambient.
	workingMetaStyle = lipgloss.NewStyle().Foreground(themeHex(theme.ToneMuted))
	toolMetaStyle    = lipgloss.NewStyle().Foreground(themeHex(theme.ToneMuted))

	// Tool status styles — per-state foreground colors for tool call rows.
	// Use theme colors matching legacy's inline_chat.go tool box backgrounds.
	toolRunningStyle = lipgloss.NewStyle().Foreground(themeHex(theme.ToolRunning.FG))
	toolSuccessStyle = lipgloss.NewStyle().Foreground(themeHex(theme.ToolSuccess.FG))
	toolErrorStyle   = lipgloss.NewStyle().Foreground(themeHex(theme.ToolFailed.FG))
	// Skill tool gets lilac variants — same as theme.SkillRunning/SkillSuccess/SkillFailed.
	skillRunningStyle = lipgloss.NewStyle().Foreground(themeHex(theme.SkillRunning.FG))
	skillSuccessStyle = lipgloss.NewStyle().Foreground(themeHex(theme.SkillSuccess.FG))
	skillFailedStyle  = lipgloss.NewStyle().Foreground(themeHex(theme.SkillFailed.FG))

	// Styled input prompt and divider styles.
	inputPromptStyle      = lipgloss.NewStyle().Foreground(themeHex(theme.CommandFG)).Bold(true)
	inputSteerPromptStyle = lipgloss.NewStyle().Foreground(themeHex(theme.ToneWarn)).Bold(true)
	inputPlaceholderStyle = lipgloss.NewStyle().Foreground(themeHex(theme.ToneMuted)).Italic(true)
	inputBoxStyle         = lipgloss.NewStyle().Foreground(themeHex(theme.ToneMuted))
	separatorStyle        = lipgloss.NewStyle().Foreground(themeHex(theme.ToneMuted))

	// Phase 1: tool box styles — background-colored bordered boxes for each
	// lifecycle state. Width is set dynamically at render time via .Width().
	toolBoxRunningStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(themeHex(theme.ToolRunning.FG)).
				Background(themeHex(theme.ToolRunning.BG)).
				Foreground(themeHex(theme.ToolRunning.FG)).
				Padding(0, 1)
	toolBoxSuccessStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(themeHex(theme.ToolSuccess.FG)).
				Background(themeHex(theme.ToolSuccess.BG)).
				Foreground(themeHex(theme.ToolSuccess.FG)).
				Padding(0, 1)
	toolBoxErrorStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(themeHex(theme.ToolFailed.FG)).
				Background(themeHex(theme.ToolFailed.BG)).
				Foreground(themeHex(theme.ToolFailed.FG)).
				Padding(0, 1)
	// Skill tool gets lilac variants.
	toolBoxSkillRunningStyle = lipgloss.NewStyle().
					Border(lipgloss.RoundedBorder()).
					BorderForeground(themeHex(theme.SkillRunning.FG)).
					Background(themeHex(theme.SkillRunning.BG)).
					Foreground(themeHex(theme.SkillRunning.FG)).
					Padding(0, 1)
	toolBoxSkillSuccessStyle = lipgloss.NewStyle().
					Border(lipgloss.RoundedBorder()).
					BorderForeground(themeHex(theme.SkillSuccess.FG)).
					Background(themeHex(theme.SkillSuccess.BG)).
					Foreground(themeHex(theme.SkillSuccess.FG)).
					Padding(0, 1)
	toolBoxSkillFailedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(themeHex(theme.SkillFailed.FG)).
				Background(themeHex(theme.SkillFailed.BG)).
				Foreground(themeHex(theme.SkillFailed.FG)).
				Padding(0, 1)

	// Context menu — a floating overlay composited on top of arbitrary
	// already-rendered content (see compositeContextMenu). No Background():
	// a terminal's "rounded" border is only rounded in the corner glyphs —
	// the cell grid underneath is always a hard rectangle, so an explicit
	// fill just makes that rectangle's edge obvious as a harsh square
	// around the border. Foreground-only (like the prompt/completions
	// family) reads as a cleaner floating outline instead.
	contextMenuStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(themeHex(theme.ToneMuted)).
				Foreground(themeHex(theme.ToneBody)).
				Padding(0, 1)
	// Selected-item highlight follows the completions dropdown's convention:
	// foreground color + bold, no background swap.
	contextMenuSelectedStyle = lipgloss.NewStyle().Foreground(themeHex(theme.CommandFG)).Bold(true)

	// toolBoxExpandedStyle is the style for an expanded tool box (subtle border).
	toolBoxExpandedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder(), false, false, false, true).
				BorderForeground(themeHex(theme.ToneMuted)).
				Padding(0, 1)

	// toolGroupBoxStyle wraps a live multi-tool-call batch (renderToolGroup)
	// — neutral (not status-colored, since it holds a mix of statuses).
	toolGroupBoxStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(themeHex(theme.ToneMuted))
	toolGroupHeaderStyle   = lipgloss.NewStyle().Foreground(themeHex(theme.ToneMuted)).Bold(true)
	toolFocusMarkerStyle   = lipgloss.NewStyle().Foreground(themeHex(theme.CommandFG)).Bold(true)
	toolGroupSummaryStyle  = lipgloss.NewStyle().Foreground(themeHex(theme.ToneMuted))
	toolGroupOverflowStyle = lipgloss.NewStyle().Foreground(themeHex(theme.ToneMuted)).Italic(true)
)
