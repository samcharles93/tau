package tui2

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
	"github.com/samcharles93/tau/internal/metrics"
	"github.com/samcharles93/tau/internal/providers"
	"github.com/samcharles93/tau/internal/providerui"
	"github.com/samcharles93/tau/internal/tui/notify"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// model is the root Bubbletea model for the chat TUI.
type model struct {
	ctx     context.Context //nolint:containedctx // app-lifetime context owned by the Bubbletea program
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

	// Conversation state - stored as raw content string fed to the viewport.
	viewport  viewport.Model
	streaming string // current streaming text delta
	reasoning string // current reasoning delta

	// agentState is the explicit, typed state driving the status bar (see
	// statusbar.go) and turn-in-flight gating (see inResponse) - set at each
	// transition point in handleChatEvent rather than re-derived by sniffing
	// m.notification text or combining ad hoc flags at render time. Zero
	// value (agentReady) is correct for a freshly constructed model with no
	// turn yet in flight. streamStartedAt marks when the active response's
	// streaming phase began (set the moment agentState first becomes
	// agentStreaming for a turn), used to derive a live tokens/sec estimate.
	agentState      agentState
	streamStartedAt time.Time

	// Tool state.
	tools           []toolState           // active tool calls in display order
	committedGroups []*committedToolGroup // multi-call batches already in scrollback, still foldable - see committedToolGroup

	// Reasoning state. committedReasoning holds completed reasoning blocks
	// already in scrollback, still collapsible/expandable - see
	// committedReasoningBlock. lastReasoningKey is the most recently
	// committed block's key, the target of the ctrl+r toggle (see
	// dispatchKey) - reasoning has no per-block focus-navigation the way
	// tools do, so ctrl+r always reaches the block from the turn that just
	// finished. reasoningKeySeq generates a fallback key (see
	// committedReasoningKey) for the rare turn that commits reasoning
	// before the owning message has a real ID.
	committedReasoning []*committedReasoningBlock

	// Child agent state - terminal summaries for each spawned child agent
	// tool call. Keyed by tool call id. Rendered as a compact status line
	// above the tool result per docs/specs/agents/05-ui.md (The state block).
	childAgents      map[string]childAgentResult
	childMessages    map[string][]tauchat.ChatMessage // live child transcripts, keyed by callID
	lastReasoningKey string
	reasoningKeySeq  int

	// childAgentOrder records each spawned child's tool-call ID in the
	// order it was first seen, since childAgents (a map) has no stable
	// iteration order - Tab-cycling (focusNextChild) needs one.
	childAgentOrder []string

	// helpOverlay is the currently open /help overlay, or nil if none - see
	// help.go. Unlike a committed scrollback message, it's redrawn fresh
	// every frame and never touches renderedLines, so it doesn't clutter
	// history the way a permanently-appended box would.
	helpOverlay *helpOverlayState

	// Input state. input may contain embedded '\n' (Shift+Enter/Ctrl+J
	// inserts a newline rather than submitting) - inputCursor is a rune
	// index into it, 0..len([]rune(input)). Editing/navigation mirror
	// pkg/taui/lineinput.go so both frontends behave identically.
	input       string
	inputCursor int
	// attachments holds pasted images referenced inline in input via
	// "[Image #N]" placeholders (see attachments.go). nextAttachmentID is
	// monotonic for the lifetime of the model so a placeholder recalled
	// from history can never collide with a fresh attachment's token.
	attachments      []inputAttachment
	nextAttachmentID int
	// inputModeCommand is the active agent-mode command selected with
	// Shift+Tab or the command palette. It is presentation/control state,
	// never part of the editable input buffer.
	inputModeCommand string
	history          []string // submitted inputs for up/down recall
	historyIdx       int      // -1 = not navigating; 0..len(history) = navigating
	draftInput       string   // stashed input on first history recall; restored at idx == len(history)

	// focused reports whether the terminal window currently has focus,
	// tracked via tea.FocusMsg/tea.BlurMsg (requires View.ReportFocus).
	// Defaults to true so a terminal that never reports focus (no
	// ReportFocus support) doesn't spuriously suppress notifications.
	focused bool

	// Completion dropdown state. compToken is the last token the dropdown was
	// computed against - when it changes (the user typed/deleted a
	// character), compSelected resets to the top-ranked match rather than
	// pointing at whatever now sits at that index. compDismissed and
	// compDismissedToken together record that Esc last hid the dropdown for
	// a specific token (compDismissed guards against "" being both the
	// zero value and a legitimate token, e.g. a bare "/"); it stays hidden
	// for that token (so a second Esc can reach the input-clearing handler)
	// and reappears only once compToken moves on to a different token.
	compSelected       int
	compToken          string
	compDismissed      bool
	compDismissedToken string

	// palette is the shared Ctrl+P command picker / Ctrl+L model picker.
	// Its search query is intentionally independent from the chat input.
	palette *paletteState

	// Viewport content - rendered lines, built incrementally.
	renderedLines []string

	// messageRanges records which span of renderedLines each ChatMessage's
	// rendered lines occupy, so a click can be resolved to "which message"
	// (see messageAtRow) rather than just "which line" - mirrors
	// committedToolGroup's lineIdx/lineCount, for whole messages instead of
	// tool boxes. Only messages with a real ID (see chat.ChatMessage.ID)
	// get an entry; entries must be kept in sync by anything that mutates
	// renderedLines in place (see spliceCommittedGroup).
	messageRanges []messageLineRange

	// lastAssistantText is the raw (unstyled) content of the most recent
	// assistant message, kept separately from renderedLines because those
	// are lipgloss-styled - the ANSI escape codes wrapping the content mean
	// a literal substring/prefix match against renderedLines can never
	// reliably succeed (this is what /copy needs; scanning styled output
	// doesn't work).
	lastAssistantText string

	// canonicalMessages is the filtered, copy-safe history of user and
	// assistant messages from the most recent ChatSessionSnapshotEvent,
	// stored in time order. System prompts, reasoning, tool protocol, and
	// internal events are excluded — this is exactly what /copy session and
	// /copy N format into a transcript.
	canonicalMessages []tauchat.ChatMessage

	// Status / transient.
	statusText        string       // one-line status bar (model @ provider only)
	notification      string       // transient notification banner, shown above the input area
	notificationLevel notify.Level // drives the banner's color (info/warn/error)
	notificationGen   int          // bumped every time notification is set; guards clear race

	// Model / provider state (populated by run.go).
	availableModels       []tauchat.ChatModelRef
	refresh               func(context.Context) ([]tauchat.ChatModelRef, error)
	completeProviderLogin func(string, providers.OAuthCredentials) error
	// updateFn backs /update. The app layer owns it because only it knows the
	// running binary's version and how to re-exec after an install. It returns
	// the line to show, and whether a restart is now pending. Nil disables the
	// command.
	updateFn        func(ctx context.Context, install bool) (string, bool, error)
	showReasoning   bool // reasoning is shown at all (peek or full)
	reasoningPeek   bool // peek mode: live tail only, committed blocks collapsed
	peekExpanded    bool // Ctrl+R widened the live peek to the full trace for this turn
	reasoningEffort string
	ctxWindow       int // context window size for % display

	// skillsListPending records that the user ran /skills list, so the next
	// SkillsChangedEvent may render the catalog into scrollback. Startup
	// discovery and background reloads publish the same event and must stay
	// silent.
	skillsListPending bool

	// Extension commands (populated from ExtensionCommandsChangedEvent).
	extensionCommands map[string]tauchat.ExtensionCommand

	// Skills from SkillsChangedEvent, used for $ prefix completions/invocation.
	skills []tauchat.SkillInfo

	// Usage tracking.
	usage *metrics.UsageTracker

	// Configuration.
	debug bool

	// Session management.
	sessionSummaries []tauchat.SessionSummary

	// sessionsFetchInFlight guards maybePrefetchSessions against firing a
	// second silent ListSessionsCommand while one is already outstanding -
	// without it, every keystroke while typing "/session " with an empty
	// cache would fire a fresh request. Set via startSessionsFetch, cleared
	// via finishSessionsFetch - see docs/specs/state-taxonomy.md, Category 5.
	sessionsFetchInFlight bool

	// Turn management.
	turnQueue    []queuedTurn // queued prompts (with attachments) behind a running turn
	lastSubmit   time.Time
	spinnerFrame int // frame index for working indicator animation

	// Markdown rendering (P3 enhancement) - reusable glamour term renderers
	// keyed by terminal width so resize doesn't allocate a new renderer for
	// the same width. Each converts assistant messages from markdown to
	// ANSI-styled output with syntax-highlighted code blocks. Only applied
	// on finalized messages (ChatResponseCompletedEvent), never during
	// streaming - mid-token markdown re-parsing is unsafe.
	mdCache map[int]*glamour.TermRenderer

	// expandedToolCache stores fully styled bodies for expanded tool boxes.
	// The small live title is still rendered on every frame; see tools.go.
	expandedToolCache      map[string]expandedToolCacheEntry
	expandedToolCacheOrder []string

	// pendingQuit is the time of the last unanswered Ctrl+C (idle, nothing to
	// cancel) - a second Ctrl+C within quitConfirmWindow confirms the quit,
	// mirroring internal/tui/inline_chat.go's double-tap guard.
	pendingQuit time.Time

	// Steering.
	steering bool

	// Interactive prompts - see formPrompt in prompt.go for the shared
	// widget both the agent-question flow and local UI flows (e.g.
	// providerLogin's Enterprise-domain prompt) build and present through
	// presentPrompt/presentLocalPrompt.
	activePrompt *formPrompt
	promptQueue  []*formPrompt

	// contextMenu is the currently open right-click menu, or nil if none is
	// open - mirrors activePrompt's nil-sentinel idiom.
	contextMenu *contextMenu

	// diffViewer is the currently open "View diff" overlay, or nil if none
	// is open - same nil-sentinel idiom as contextMenu.
	diffViewer *diffViewerState

	// childTranscriptViewer is the currently open child-agent transcript
	// overlay, or nil if none is open - same nil-sentinel idiom as
	// diffViewer (see childtranscript.go).
	childTranscriptViewer *childTranscriptViewerState

	// sessionTreeOverlay is the currently open Ctrl+O session navigator, or
	// nil if none is open - same nil-sentinel idiom as contextMenu (see
	// sessiontree.go).
	sessionTreeOverlay *sessionTreeState

	// Plugin views.
	panels map[string]pluginPanel

	// Bash mode.
	bashRunning bool
	bashCallID  string

	// Phase 1: tool focus/expansion navigation (keyboard and mouse).
	focusedTool int    // index into m.tools for keyboard nav (-1 = none)
	expandedID  string // tool ID currently expanded ("" = none)

	// focusedChild indexes m.childAgentOrder for keyboard nav across
	// finished child-agent state blocks (-1 = none). Tab reaches this ring
	// only once focusNextTool has nothing eligible left - the two rings
	// are mutually exclusive, each clearing the other when set.
	focusedChild int

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

	// Mouse drag text selection (see selection.go). Built from scratch on raw press/motion/release
	// events - bubbletea v2 and bubbles have no selection primitive to
	// reuse (verified against clipboard.go/mouse.go). Four regions each
	// get their own selectionState - viewport (whole logical lines), input
	// box (rune-precise), status bar (column-precise), and the live tool-call
	// box (whole lines) - but share one state machine and one finalize path
	// (finalizeSelection) rather than four hand-rolled ones. Copying is an
	// explicit right-click action after selection.
	viewportSel selectionState
	inputSel    selectionState
	statusSel   selectionState
	toolsSel    selectionState

	// dragRegion records which UI region a mouse gesture started in, so a
	// drag/release that moves outside that region (or outside the terminal
	// entirely, on emulators that clamp coordinates) still routes to the
	// right selection - press decides the region once; drag/release just
	// follow it.
	dragRegion dragRegion
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

func newModel(
	ctx context.Context,
	runtime tauchat.ChatRuntime,
	chatSub *eventbus.Subscriber[tauchat.ChatEvent],
	sessionID, modelName, provider string,
	availableModels []tauchat.ChatModelRef,
	refresh func(context.Context) ([]tauchat.ChatModelRef, error),
	updateFn func(ctx context.Context, install bool) (string, bool, error),
	reasoningMode string,
	reasoningEffort string,
	toolCallsDefaultCollapsed bool,
	usage *metrics.UsageTracker,
	debug bool,
) *model {
	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	vp.SoftWrap = true

	// Glamour markdown renderer cache, keyed by terminal width.
	mdCache := map[int]*glamour.TermRenderer{}
	// Pre-populate the default width so the first finalized message can
	// render immediately without a WindowSizeMsg having arrived yet.
	ensureMDRenderer(mdCache, 80)

	m := &model{
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
		focusedChild:              -1,
		autoFollow:                true,
		viewportSel:               newSelectionState(),
		inputSel:                  newSelectionState(),
		statusSel:                 newSelectionState(),
		toolsSel:                  newSelectionState(),
		availableModels:           availableModels,
		refresh:                   refresh,
		updateFn:                  updateFn,
		completeProviderLogin:     providers.NewManage(nil).LoginComplete,
		reasoningEffort:           reasoningEffort,
		toolCallsDefaultCollapsed: toolCallsDefaultCollapsed,
		toolGroupCollapsed:        toolCallsDefaultCollapsed,
		usage:                     usage,
		debug:                     debug,
		extensionCommands:         make(map[string]tauchat.ExtensionCommand),
		panels:                    make(map[string]pluginPanel),
		childAgents:               make(map[string]childAgentResult),
		childMessages:             make(map[string][]tauchat.ChatMessage),
		mdCache:                   mdCache,
		expandedToolCache:         make(map[string]expandedToolCacheEntry),
	}
	m.setReasoningMode(reasoningMode)
	return m
}

// spinnerFrames are the Unicode braille dots cycled through at 80ms -
// mirrors the legacy TUI's spinnerLoop (internal/tui/inline_chat.go).
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinTick returns a tea.Cmd that fires a tickMsg after 80ms.
func spinTick() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg{t: t}
	})
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(
		readNextEvent(m.chatSub),
		sendCommand(m.runtime, tauchat.ListSkillsCommand{
			RequestedAt: time.Now().UTC(),
		}),
		func() tea.Msg { return startupMsg{m.sessionID, m.modelName, m.provider} },
	)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle quit request from the program (Ctrl+C signal, etc.).
	if _, ok := msg.(tea.QuitMsg); ok {
		return m, nil
	}
	if cmd, ok := m.handleWindowSizeMsg(msg); ok {
		return m, cmd
	}
	if cmd, ok := m.handleMouseMsg(msg); ok {
		return m, cmd
	}
	if cmd, ok := m.handleInputMsg(msg); ok {
		return m, cmd
	}
	if cmd, ok := m.handleChatStreamMsg(msg); ok {
		return m, cmd
	}
	if cmd, ok := m.handleProviderMsg(msg); ok {
		return m, cmd
	}
	if cmd, ok := m.handleResultMsg(msg); ok {
		return m, cmd
	}
	return m, nil
}

// handleWindowSizeMsg applies terminal resize: viewport bounds, glamour renderer rebuild, child-transcript overlay resize. Returns (cmd, true) when the message was handled.
func (m *model) handleWindowSizeMsg(msg tea.Msg) (tea.Cmd, bool) {
	sz, ok := msg.(tea.WindowSizeMsg)
	if !ok {
		return nil, false
	}
	m.width = sz.Width
	m.height = sz.Height
	m.viewport.SetWidth(sz.Width)
	// Alt-screen layout: the viewport shares the terminal with the fixed
	// chrome (separator, padded input box, status bar). The actual space
	// left for the viewport is recomputed every frame in View(); here we
	// just store an upper-bound estimate for tests and sanity checks.
	inputLines := max(strings.Count(m.input, "\n")+1, 1)
	reserved := 1 + inputLines + 4 + 1 // separator + input box + status
	m.maxViewportHeight = max(sz.Height-reserved, 4)
	m.viewport.SetHeight(m.maxViewportHeight)
	// Rebuild the glamour renderer with the new terminal width so
	// subsequent finalized messages don't wrap at a stale column.
	ensureMDRenderer(m.mdCache, sz.Width)
	// Resize the child transcript overlay's viewport too, if open - the
	// diff viewer has this gap (its viewport is fixed at open-time), but
	// it's cheap to avoid here so a resize while drilled in doesn't leave
	// a stale-sized box.
	if m.childTranscriptViewer != nil {
		boxW := max(20, int(float64(m.width)*diffViewerWidthFrac))
		boxH := max(10, int(float64(m.height)*diffViewerHeightFrac))
		m.childTranscriptViewer.viewport.SetWidth(max(20, boxW-4))
		m.childTranscriptViewer.viewport.SetHeight(max(3, boxH-6))
	}
	return nil, true
}

// handleMouseMsg routes mouse wheel (scroll/help-overlay) and left/right click/drag/release. Returns (cmd, true) when the message was handled.
func (m *model) handleMouseMsg(msg tea.Msg) (tea.Cmd, bool) {
	mm, ok := msg.(tea.MouseMsg)
	if !ok {
		return nil, false
	}
	mev := mm.Mouse()
	switch mev.Button {
	case tea.MouseWheelUp:
		if m.helpOverlay != nil {
			m.helpOverlay.scrollOffset -= 3
			break
		}
		m.viewport.ScrollUp(3)
		m.autoFollow = false
	case tea.MouseWheelDown:
		if m.helpOverlay != nil {
			m.helpOverlay.scrollOffset += 3
			break
		}
		m.viewport.ScrollDown(3)
		if m.viewport.AtBottom() {
			m.autoFollow = true
		}
	case tea.MouseLeft:
		switch mm.(type) {
		case tea.MouseClickMsg:
			return m.handleMousePress(mev.X, mev.Y), true
		case tea.MouseMotionMsg:
			m.handleMouseDrag(mev.X, mev.Y)
		case tea.MouseReleaseMsg:
			return m.handleMouseRelease(), true
		}
	case tea.MouseRight:
		if _, ok := mm.(tea.MouseClickMsg); ok {
			// Right-click keeps its existing "copy my selection"
			// behavior; only when nothing is selected does it fall
			// through to opening a context menu at the click.
			if cmd := m.copyActiveSelection(); cmd != nil {
				return cmd, true
			}
			m.openContextMenuAt(mev.X, mev.Y)
		}
	}

	return nil, true
}

// handleInputMsg handles paste, clipboard reply, focus/blur, and key press/release. Returns (cmd, true) when the message was handled.
func (m *model) handleInputMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.PasteMsg:
		return m.handlePaste(msg.Content), true

	case tea.ClipboardMsg:
		// Response to an explicit tea.ReadClipboard (ctrl+v fallback for
		// terminals that don't deliver bracketed pastes): route through the
		// same image-aware paste handling.
		return m.handlePaste(msg.Content), true

	case tea.FocusMsg:
		m.focused = true
		return nil, true

	case tea.BlurMsg:
		m.focused = false
		return nil, true

	case tea.KeyPressMsg:
		return m.handleKey(msg), true

	case tea.KeyReleaseMsg:
		return noopCmd, true

	}
	return nil, false
}

// handleChatStreamMsg drives the chat event stream: re-arms the subscription and dispatches events; quits when the stream closes. Returns (cmd, true) when the message was handled.
func (m *model) handleChatStreamMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case chatEventMsg:
		var cmds []tea.Cmd
		cmds = append(cmds, readNextEvent(m.chatSub)) // re-arm
		if eventCmd := m.handleChatEvent(msg.event); eventCmd != nil {
			cmds = append(cmds, eventCmd)
		}
		return tea.Batch(cmds...), true

	case chatEventsClosedMsg:
		m.notification = "event stream closed - exiting"
		return tea.Quit, true

	}
	return nil, false
}

// handleProviderMsg handles update-check and provider toggle/login results. Returns (cmd, true) when the message was handled.
func (m *model) handleProviderMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case updateCheckMsg:
		if msg.err != nil {
			return m.setNotification("update failed: " + msg.err.Error()), true
		}
		m.appendMessage("system", msg.text)
		if msg.restart {
			// The binary on disk is already the new one; quitting lets the app
			// layer re-exec it in this same terminal.
			return tea.Quit, true
		}
		return nil, true

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
		return nil, true

	case providerLoginStartedMsg:
		if msg.err != nil {
			m.appendMessage("system", providerui.FailureMessage(msg.displayName, msg.err))
			return nil, true
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
		return tea.Batch(copyCmd, m.providerLoginPoll(msg.providerID, msg.displayName, msg.session)), true

	case providerLoginResultMsg:
		if msg.err != nil {
			m.appendMessage("system", providerui.FailureMessage(msg.displayName, msg.err))
			return nil, true
		}
		if len(msg.models) > 0 {
			m.availableModels = msg.models
			m.appendMessage("system", fmt.Sprintf("%s enabled, models available: %d", msg.displayName, len(msg.models)))
		} else {
			m.appendMessage("system", fmt.Sprintf("%s enabled", msg.displayName))
		}
		return nil, true

	}
	return nil, false
}

// handleResultMsg handles send/bash/refresh results, notification-clear ticks, spinner ticks, and startup. Returns (cmd, true) when the message was handled.
func (m *model) handleResultMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case clearNotificationMsg:
		if msg.gen == m.notificationGen {
			m.notification = ""
			m.notificationLevel = notify.LevelInfo
		}
		return nil, true

	case sendResultMsg:
		if msg.err != nil {
			m.agentState = agentReady
			m.notification = fmt.Sprintf("send failed: %v", msg.err)
		}
		return nil, true

	case bashSendResultMsg:
		if msg.err != nil {
			m.bashRunning = false
			m.bashCallID = ""
			m.notification = fmt.Sprintf("bash command failed: %v", msg.err)
		}
		return nil, true

	case refreshResultMsg:
		if msg.err != nil {
			return m.setNotification("refresh failed: " + msg.err.Error()), true
		}
		m.availableModels = msg.models
		return m.setNotification(fmt.Sprintf("refreshed: %d models available", len(msg.models))), true

	case tickMsg:
		m.spinnerFrame++
		// Phase 1: per-tool spinner animation - bump spinnerIdx for every
		// running tool so each tool row animates independently.
		for i := range m.tools {
			if m.tools[i].status == "running" {
				m.tools[i].spinnerIdx++
			}
		}
		if m.inResponse() {
			return spinTick(), true
		}
		return nil, true

	case startupMsg:
		m.sessionID = msg.sessionID
		m.modelName = msg.modelName
		m.provider = msg.provider
		return nil, true

	}
	return nil, false
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

	// This sequence is deliberately NOT driven by overlayPrecedence
	// (overlay.go): that list declares dispatch precedence (which overlay
	// wins key routing), a different concern from paint/Z-order here. Since
	// closeOtherExclusiveOverlays keeps at most one of these five mutually
	// exclusive, their relative order below never actually matters at
	// runtime - only completions (soft) can legitimately be visible
	// alongside one of them, which is why it's composited after all five.
	// activePrompt has no entry here at all: it renders as flow content via
	// computeLayout/renderPrompt above, not as a floating compositor layer.
	base := sb.String()
	if m.contextMenu != nil {
		base = m.compositeContextMenu(base)
	}
	if m.diffViewer != nil {
		base = m.compositeDiffViewer(base)
	}
	if m.childTranscriptViewer != nil {
		base = m.compositeChildTranscriptViewer(base)
	}
	if m.helpOverlay != nil {
		base = m.compositeHelpOverlay(base)
	}
	if m.palette != nil {
		base = m.compositePaletteOverlay(base)
	}
	if rows, token, ok := m.completionsVisible(); ok {
		base = m.compositeCompletionsOverlay(base, rows, token)
	}
	if m.sessionTreeOverlay != nil {
		base = m.compositeSessionTreeOverlay(base)
	}

	v := tea.NewView(base)
	// AltScreen owns the full terminal so we can use guaranteed screen real
	// estate for tool boxes, expansion panels, and flicker-free output.
	v.AltScreen = true
	// Requests terminal focus reporting so we only fire a desktop
	// notification (see handleChatEvent's ChatResponseCompletedEvent case)
	// when the user has actually looked away - matches the legacy
	// engine.Focused() gate in internal/tui/inline_events.go.
	v.ReportFocus = true
	// CellMotion enables click, release, and wheel events (plus drag) without
	// the constant hover-motion stream AllMotion would add, and is better
	// supported across terminals - all we need for scroll + click-to-expand.
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// startSessionsFetch and finishSessionsFetch centralize the
// sessionsFetchInFlight guard (see its field comment) so every failure path
// clears it through one grep-able name instead of a bare bool assignment
// repeated at each call site - see docs/specs/state-taxonomy.md, Category 5.
// A new failure path added later still has to remember to call
// finishSessionsFetch, same as before, but there is now one name to find
// every existing call site by.
func (m *model) startSessionsFetch()  { m.sessionsFetchInFlight = true }
func (m *model) finishSessionsFetch() { m.sessionsFetchInFlight = false }

// computeLayout renders every non-viewport UI region, decides how much
// vertical space the viewport gets, and records the on-screen row range of
// each tool box. It is called once by View() (for rendering) and again by
// handleMousePress/handleMouseDrag (for hit-testing) - keeping both derived
// from the same function is what guarantees they never disagree.
func (m *model) computeLayout() layoutGeometry {
	var g layoutGeometry

	// 1. Plugin panel.
	activePanelStr := m.renderActivePanel()

	// 2. Live response content is rendered inside the viewport so it occupies
	// the same bottom-aligned chat pane as finalized messages.
	visibleReasoning := m.reasoning != "" && m.showReasoning
	viewportLines := m.viewportLinesForView(visibleReasoning)
	m.viewport.SetContentLines(viewportLines)

	// 3. Live tool-call group (uncommitted - see flushToolGroup). Rendered as
	// one box: a lone tool keeps its full box, multiple concurrent/sequential
	// calls render as a single group so a long tool-calling burst doesn't
	// grow unbounded on screen while it's still running.
	toolsStr, toolRows := m.renderToolGroup()

	// 4. Interactive prompt (modal).
	promptStr := ""
	if m.activePrompt != nil {
		promptStr = renderPrompt(m.activePrompt, m.width)
	}

	// 5. Completion dropdown - floats as a centered overlay (see
	// compositeCompletionsOverlay in View()) rather than flow-laid chrome, so
	// it no longer occupies space here.

	// 6. Notification banner - a fixed notifyReservedLines-tall area is
	// always reserved directly above the separator/input, even when there's
	// nothing to show, matching how Claude Code's own status area never
	// resizes (see renderNotifyArea).
	notifyStr := m.renderNotifyArea()

	// 7. Divider and input area.
	sepStr := m.renderSeparator()
	inputStr := m.renderInputArea()

	// 8. Status bar.
	statusStr := ""
	if m.width > 0 {
		statusStr = m.computeStatusBar()
	}

	// 9-10. Assemble the chrome (everything below the viewport) and compute
	// the total height reserved for it plus the plugin panel.
	chromeStr, panelSepStr, reservedHeight := assembleChrome(activePanelStr, toolsStr, promptStr, notifyStr, sepStr, inputStr, statusStr)

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
	recordChromeGeometry(&g, chromeRender{
		viewportHeight: viewportHeight,
		toolsStr:       toolsStr,
		toolRows:       toolRows,
		promptStr:      promptStr,
		notifyStr:      notifyStr,
		sepStr:         sepStr,
		inputStr:       inputStr,
	})
	return g
}

// renderActivePanel renders the active plugin panel (title bar, content,
// bottom border) or "" when no panel is active.
func (m *model) renderActivePanel() string {
	p := m.activePanel()
	if p == nil {
		return ""
	}
	var psb strings.Builder
	psb.WriteString(panelStyle.Render("┌─ " + p.title + " ─┐"))
	psb.WriteString("\n")
	psb.WriteString(p.content)
	psb.WriteString("\n")
	psb.WriteString(panelStyle.Render("└" + strings.Repeat("─", min(m.width, 40)) + "┘"))
	return psb.String()
}

// renderNotifyArea renders the notification banner into exactly
// notifyReservedLines rows: width-wrapped when a notification is set, then
// padded/clipped so the reserved height truly never varies (see the step-6
// comment in computeLayout's history - previously this area only occupied
// space while m.notification was non-empty, so the viewport visibly grew and
// shrank by that height every time a notification appeared or cleared).
func (m *model) renderNotifyArea() string {
	notifyWidth := m.width
	if notifyWidth <= 0 {
		notifyWidth = 80
	}
	var notifyRendered string
	if m.notification != "" {
		// The Ctrl+C/steering hints already live in the status bar
		// (ctrlCStopSeg); this reserved area is only for actual
		// notifications, not a duplicate of that chrome.
		notifyRendered = notifyStyleForLevel(m.notificationLevel).Width(notifyWidth).Render(m.notification)
	}
	h := max(visualLineCount(notifyRendered), notifyReservedLines)
	return padOrClipLines(notifyRendered, h)
}

// renderSeparator renders the horizontal divider line above the input area,
// including the (currently zero) top/bottom margins.
func (m *model) renderSeparator() string {
	sepWidth := m.width
	if sepWidth <= 0 {
		sepWidth = 80
	}
	sepStr := separatorStyle.Render(strings.Repeat("─", sepWidth))
	// Apply separator margins (both zero today; self-documenting).
	if separatorMarginTop > 0 || separatorMarginBottom > 0 {
		var sb strings.Builder
		for range separatorMarginTop {
			sb.WriteString("\n")
		}
		sb.WriteString(sepStr)
		for range separatorMarginBottom {
			sb.WriteString("\n")
		}
		sepStr = sb.String()
	}
	return sepStr
}

// assembleChrome joins the chrome parts (everything below the viewport) into a
// single string, prepends the plugin panel separator when a panel is active,
// and returns the joined chrome, the panel separator, and the total reserved
// height (chrome + panel).
func assembleChrome(activePanelStr, toolsStr, promptStr, notifyStr, sepStr, inputStr, statusStr string) (chromeStr, panelSepStr string, reservedHeight int) {
	// The boundary newline between the viewport and the chrome is added
	// during final assembly in View(), so it is not counted here.
	chromeParts := []string{}
	if toolsStr != "" {
		chromeParts = append(chromeParts, toolsStr)
	}
	if promptStr != "" {
		chromeParts = append(chromeParts, promptStr)
	}
	// notifyStr is always present (padOrClipLines guarantees
	// notifyReservedLines rows even when there's no message).
	chromeParts = append(chromeParts, notifyStr, sepStr, inputStr, statusStr)

	chromeStr = strings.Join(chromeParts, "\n")
	reservedHeight = visualLineCount(chromeStr)
	if activePanelStr != "" {
		panelSepStr = activePanelStr + "\n\n"
		reservedHeight += visualLineCount(panelSepStr)
	}
	return chromeStr, panelSepStr, reservedHeight
}

// chromeRender carries the rendered chrome regions and viewport height that
// recordChromeGeometry needs to compute on-screen row ranges.
type chromeRender struct {
	viewportHeight int
	toolsStr       string
	toolRows       []toolBoxGeometry
	promptStr      string
	notifyStr      string
	sepStr         string
	inputStr       string
}

// recordChromeGeometry records the on-screen row range of each chrome region
// (and each tool box) in final rendered-view coordinates. chromeParts are
// joined by a single "\n" each (see assembleChrome), which is the same
// separator that already appears between any two lines of rendered text - it
// does not add a blank row of its own. So advancing from one part to the next
// is exactly visualLineCount(part) rows, with no extra "+1" per transition
// (unlike the viewport->chrome boundary below, which IS a standalone "\n"
// written outside chromeStr).
func recordChromeGeometry(g *layoutGeometry, c chromeRender) {
	row := 0
	if g.panelSepStr != "" {
		row += visualLineCount(g.panelSepStr)
	}
	if g.topPadding > 0 {
		row += g.topPadding
	}
	g.viewportStartY = row
	g.viewportEndY = g.viewportStartY + c.viewportHeight - 1

	row = g.viewportEndY + 1 // "\n" boundary between viewport and chrome
	if c.toolsStr != "" {
		g.toolsStartY = row
		g.toolsEndY = row + visualLineCount(c.toolsStr) - 1
		g.toolBoxes = make([]toolBoxGeometry, len(c.toolRows))
		for i, tr := range c.toolRows {
			g.toolBoxes[i] = toolBoxGeometry{id: tr.id, startY: row + tr.startY, endY: row + tr.endY}
		}
		row += visualLineCount(c.toolsStr)
	}
	if c.promptStr != "" {
		row += visualLineCount(c.promptStr)
	}
	row += visualLineCount(c.notifyStr) // notifyStr is always present
	row += visualLineCount(c.sepStr)    // separator is always present
	g.inputStartY = row
	inputHeight := visualLineCount(c.inputStr)
	g.inputEndY = row + inputHeight - 1
	g.statusY = g.inputEndY + 1
}
