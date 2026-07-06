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
	tools []toolState // active tool calls in display order

	// Input state. input may contain embedded '\n' (Shift+Enter/Ctrl+J
	// inserts a newline rather than submitting) — inputCursor is a rune
	// index into it, 0..len([]rune(input)). Editing/navigation mirror
	// pkg/taui/lineinput.go so both frontends behave identically.
	input       string
	inputCursor int
	history     []string // submitted inputs for up/down recall
	historyIdx  int      // -1 = not navigating; 0..len(history) = navigating

	// focused reports whether the terminal window currently has focus,
	// tracked via tea.FocusMsg/tea.BlurMsg (requires View.ReportFocus).
	// Defaults to true so a terminal that never reports focus (no
	// ReportFocus support) doesn't spuriously suppress notifications.
	focused bool

	// mouseClick{X,Y} records the last left-click coordinates for tool-box
	// hit-testing; Worker 2 consumes these during View rendering to decide
	// which tool row needs an expansion or selection highlight.
	mouseClickX int
	mouseClickY int

	// Completion dropdown state. compToken is the last token the dropdown was
	// computed against — when it changes (the user typed/deleted a
	// character), compSelected resets to the top-ranked match rather than
	// pointing at whatever now sits at that index.
	compSelected int
	compToken    string

	// Viewport content — rendered lines, built incrementally.
	renderedLines []string

	// lastAssistantText is the raw (unstyled) content of the most recent
	// assistant message, kept separately from renderedLines because those
	// are lipgloss-styled — the ANSI escape codes wrapping the content mean
	// a literal substring/prefix match against renderedLines can never
	// reliably succeed (this is what /copy needs; scanning styled output
	// doesn't work).
	lastAssistantText string

	// Status / transient.
	statusText      string // one-line status bar (model @ provider only)
	notification    string // transient notification (Phase 1 compat)
	notificationGen int    // bumped every time notification is set; guards clear race
	notifyQ         *notify.Queue

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

	// Interactive prompts.
	activePrompt     *tauchat.InteractivePromptRequestedEvent
	promptQueue      []tauchat.InteractivePromptRequestedEvent
	promptConfirmYes bool // confirm-kind prompts: which option (Yes/No) is highlighted

	// Plugin views.
	panels map[string]pluginPanel

	// Bash mode.
	bashRunning bool
	bashCallID  string

	// Phase 1: tool focus/expansion navigation.
	focusedTool int    // index into m.tools for keyboard nav (-1 = none)
	expandedID  string // tool ID currently expanded ("" = none)
	lastClickY  int    // Y coordinate of last mouse click for hit-testing
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

	// Phase 1: live output streaming, per-tool spinner, hold-before-commit,
	// and expand/collapse interaction.
	tailLines  []string  // live output streaming, max tailCap lines
	tailCap    int       // max tail lines to show (default 6)
	holdUntil  time.Time // when to move this completed tool to viewport (zero = not holding)
	spinnerIdx int       // per-tool spinner animation frame (bumped on tickMsg)
	expanded   bool      // user has clicked Enter to expand this tool
}

type pluginPanel struct {
	id      string
	title   string
	content string
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
		ctx:               ctx,
		runtime:           runtime,
		chatSub:           chatSub,
		sessionID:         sessionID,
		modelName:         modelName,
		provider:          provider,
		viewport:          vp,
		historyIdx:        -1,
		focused:           true,
		focusedTool:       -1,
		availableModels:   availableModels,
		refresh:           refresh,
		showReasoning:     showReasoning,
		reasoningEffort:   reasoningEffort,
		usage:             usage,
		webURL:            webURL,
		debug:             debug,
		notifyQ:           notify.NewQueue(),
		extensionCommands: make(map[string]tauchat.ExtensionCommand),
		panels:            make(map[string]pluginPanel),
		mdCache:           mdCache,
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
		case tea.MouseWheelDown:
			m.viewport.ScrollDown(3)
		case tea.MouseLeft:
			if _, ok := msg.(tea.MouseClickMsg); ok {
				m.mouseClickX = mev.X
				m.mouseClickY = mev.Y
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

	case tickMsg:
		m.spinnerFrame++
		// Phase 1: per-tool spinner animation — bump spinnerIdx for every
		// running tool so each tool row animates independently.
		for i := range m.tools {
			if m.tools[i].status == "running" {
				m.tools[i].spinnerIdx++
			}
		}
		// Phase 1: commit completed tools to viewport after hold-state expires.
		m.commitExpiredTools()
		if m.inResponse || m.hasHoldingTools() {
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
	_ = m.lastClickY // reserved for future mouse hit-testing (Phase 1)
	var sb strings.Builder

	// Pre-render non-viewport segments so we can measure their exact heights.
	// The viewport keeps the main pane, while the input block and status bar
	// stay anchored at the bottom of the alt-screen.

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

	// 3. Tool calls.
	var toolsStr string
	if len(m.tools) > 0 {
		toolBoxes := make([]string, len(m.tools))
		for i, t := range m.tools {
			expanded := m.expandedID != "" && m.expandedID == t.id
			if expanded {
				m.focusedTool = i
			}
			toolBoxes[i] = m.renderToolBox(t, expanded, len(m.tools))
		}
		toolsStr = strings.Join(toolBoxes, "\n\n")
	}

	// 4. Interactive prompt (modal).
	var promptStr string
	if m.activePrompt != nil {
		promptStr = renderPrompt(m.activePrompt, m.promptConfirmYes)
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

	// 6. Notification.
	var notifyStr string
	if m.notification != "" {
		notifyStr = notifyStyle.Render(m.notification)
	}

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
	if notifyStr != "" {
		chromeParts = append(chromeParts, notifyStr)
	}
	chromeParts = append(chromeParts, sepStr, inputStr, statusStr)

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
	if m.inResponse || m.viewport.PastBottom() {
		m.viewport.GotoBottom()
	}

	// 12. Assemble final view in strict top-to-bottom layout order.
	if panelSepStr != "" {
		sb.WriteString(panelSepStr)
	}
	if topPadding > 0 {
		sb.WriteString(strings.Repeat("\n", topPadding))
	}
	sb.WriteString(m.viewport.View())
	sb.WriteString("\n")
	sb.WriteString(chromeStr)

	v := tea.NewView(sb.String())
	// AltScreen owns the full terminal so we can use guaranteed screen real
	// estate for tool boxes, expansion panels, and flicker-free output.
	v.AltScreen = true
	// Requests terminal focus reporting so we only fire a desktop
	// notification (see handleChatEvent's ChatResponseCompletedEvent case)
	// when the user has actually looked away — matches the legacy
	// engine.Focused() gate in internal/tui/inline_events.go.
	v.ReportFocus = true
	return v
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
		return nil

	case "pgdown":
		m.viewport.HalfPageDown()
		return nil

	case "ctrl+l":
		m.clearScreen()
		return nil

	case "esc":
		if m.bashRunning {
			return m.cancelBash()
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
		// Phase 1: reverse tool focus navigation.
		if m.shouldNavigateTools() {
			m.focusNextTool(-1)
		}
		return nil

	case "enter":
		// Phase 1: toggle tool expansion when a tool is focused.
		if m.focusedTool >= 0 && m.input == "" && !m.inResponse && m.activePrompt == nil {
			return m.toggleToolExpansion()
		}
		return m.submitInput()

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
}

// clearScreen wipes the visible scrollback (Ctrl+L) without touching the
// underlying chat session — unlike /clear, which sends a
// ResetChatSessionCommand and actually starts a new session, this only
// clears what's rendered locally. The next ChatSessionSnapshotEvent (e.g.
// from /session, /resume) still rebuilds renderedLines from the real
// session history, so nothing is actually lost.
func (m *model) clearScreen() {
	m.renderedLines = m.renderedLines[:0]
	m.viewport.SetContentLines(m.renderedLines)
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

func (m *model) insertAtCursor(s string) {
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
	if m.inputCursor <= 0 {
		return
	}
	rs := []rune(m.input)
	rs = append(rs[:m.inputCursor-1], rs[m.inputCursor:]...)
	m.input = string(rs)
	m.inputCursor--
}

func (m *model) deleteAtCursor() {
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

	body := make([]string, len(lines))
	for i, ln := range lines {
		prefix := inputPromptStyle.Render("> ")
		if m.inResponse {
			prefix = inputSteerPromptStyle.Render("(steer) > ")
		}
		if i > 0 {
			prefix = strings.Repeat(" ", len([]rune(stripANSI(prefix))))
		}
		if i == curLine {
			body[i] = prefix + renderLineWithCursor(ln, curCol)
		} else {
			body[i] = prefix + inputStyle.Render(string(ln))
		}
	}

	res := renderInputBox(m.width, "input", body, "")
	if m.inResponse {
		ctrlC := lipgloss.NewStyle().Foreground(themeHex(theme.ToneError)).Bold(true).Render("Ctrl+C")
		enter := lipgloss.NewStyle().Foreground(themeHex(theme.CommandFG)).Bold(true).Render("Enter")
		hint := lipgloss.NewStyle().Foreground(themeHex(theme.ToneMuted)).Italic(true).Render(
			fmt.Sprintf("[%s] stop | [%s] steer", ctrlC, enter),
		)
		res = renderInputBox(m.width, "steer", body, hint)
	}
	return res
}

func (m *model) viewportLinesForView(visibleReasoning bool) []string {
	lines := make([]string, 0, len(m.renderedLines)+4)
	lines = append(lines, m.renderedLines...)

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

// renderLineWithCursor renders one logical line with a highlighted cell at
// column col. A cursor at or past the line end highlights a trailing blank
// cell so it's never invisible.
func renderLineWithCursor(ln []rune, col int) string {
	col = min(col, len(ln))
	before := inputStyle.Render(string(ln[:col]))
	after := inputStyle.Render(string(ln[col:]))
	return before + inputCursorStyle.Render(" ") + after
}

func (m *model) recallHistory(delta int) tea.Cmd {
	if len(m.history) == 0 {
		return nil
	}
	if m.historyIdx == -1 {
		// Start navigating from the end.
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
		if m.historyIdx >= len(m.history) {
			m.historyIdx = len(m.history) - 1
		}
	}
	m.input = m.history[m.historyIdx]
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
		return m.resolvePrompt(m.input)
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

	// Record the user message locally for immediate display.
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
		m.inResponse = true
		m.spinnerFrame = 0
		m.turnStartedAt = time.Now()
		// Seed the verb rotation from the turn's start so each turn opens on a
		// different word; the low bits of the nanosecond clock are plenty of
		// spread for a cosmetic choice.
		m.turnSeed = m.turnStartedAt.UnixNano()
		return spinTick()

	case tauchat.ChatResponseDeltaEvent:
		m.streaming += e.Delta

	case tauchat.ChatReasoningDeltaEvent:
		m.reasoning += e.Delta

	case tauchat.ChatToolCallDeltaEvent:
		m.upsertToolCall(e.CallID, e.ToolName, e.ArgumentsSummary)

	case tauchat.ChatToolExecutionStartedEvent:
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
		// Phase 1: start the 450ms hold-state before committing to viewport.
		m.setToolHoldUntil(e.CallID, time.Now().Add(450*time.Millisecond))
		if m.bashCallID != "" && e.CallID == m.bashCallID {
			m.bashRunning = false
			m.bashCallID = ""
		}

	case tauchat.ChatToolOutputEvent:
		m.setToolResult(e.CallID, e.Chunk)
		m.appendToolTail(e.CallID, e.Chunk)

	case tauchat.ChatResponseCompletedEvent:
		content := m.finalizeResponse()
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
		// Pushed through notifyQ (not setNotification) at error level with
		// Duration 0 (persists until dismissed) to match how
		// ChatNotificationEvent below reports errors, and also printed to
		// the scrollback so it isn't lost if overtaken by a later notice.
		m.notifyQ.Push(notify.Notification{
			Message:  e.Message,
			Level:    notify.LevelError,
			Duration: 0,
		})
		m.appendMessage("system", "✗ "+e.Message)
		return nil

	case tauchat.ChatNotificationEvent:
		m.notifyQ.Push(notify.Notification{
			Message:  e.Message,
			Level:    notifyLevelFromChat(e.Level),
			Duration: notifyDurationFromChat(e.Level),
		})
		if e.Level == tauchat.ChatNotificationError {
			m.appendMessage("system", e.Message)
		}
		return nil

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

	// Rebuild viewport content from session history.
	m.renderedLines = m.renderedLines[:0]
	for _, msg := range state.Messages {
		role := ""
		switch msg.Role {
		case tauchat.ChatRoleUser:
			role = "user"
		case tauchat.ChatRoleAssistant:
			role = "assistant"
			m.lastAssistantText = msg.Content
		default:
			continue
		}
		lines := strings.Split(msg.Content, "\n")
		for i, l := range lines {
			if i == 0 {
				m.renderedLines = append(m.renderedLines, renderLine(role, l))
			} else {
				m.renderedLines = append(m.renderedLines, renderContinuationLine(role, l))
			}
		}
	}
	m.viewport.SetContentLines(m.renderedLines)
	m.viewport.GotoBottom()
}

// N7: finalizeResponse synthesises a placeholder when a turn consisted
// purely of tool calls with no trailing assistant text, so the user sees a
// trace of what happened instead of nothing.
// finalizeResponse returns the finalized assistant text (empty if none) so
// the caller can decide whether to fire a desktop notification.
func (m *model) finalizeResponse() string {
	hadTools := len(m.tools) > 0
	content := m.streaming
	if m.reasoning != "" && content == "" {
		content = "[reasoning only]"
	}
	if content == "" && hadTools {
		var names []string
		for _, t := range m.tools {
			names = append(names, t.name)
		}
		content = fmt.Sprintf("[tools: %s]", strings.Join(names, ", "))
	}
	if content != "" {
		// Store raw markdown for /copy before glamour renders it.
		m.lastAssistantText = content
		// Render through glamour (markdown → ANSI) and append to viewport.
		// Glamour output is already fully styled, so each line goes
		// directly into renderedLines without passing through renderLine().
		rendered := m.renderMarkdown(content)
		m.renderedLines = append(m.renderedLines, strings.Split(rendered, "\n")...)
		m.viewport.SetContentLines(m.renderedLines)
		m.viewport.GotoBottom()
	}
	m.streaming = ""
	m.reasoning = ""
	m.inResponse = false
	return content
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

// setToolHoldUntil sets the hold-until time for a completed tool (Phase 1).
func (m *model) setToolHoldUntil(id string, until time.Time) {
	for i := range m.tools {
		if m.tools[i].id == id {
			m.tools[i].holdUntil = until
			return
		}
	}
}

// findToolIndex returns the index of the tool with the given id, or -1.
func (m *model) findToolIndex(id string) int {
	for i := range m.tools {
		if m.tools[i].id == id {
			return i
		}
	}
	return -1
}

// hasHoldingTools returns true when any tool is in its hold-state (Phase 1).
func (m *model) hasHoldingTools() bool {
	for i := range m.tools {
		if !m.tools[i].holdUntil.IsZero() && time.Now().Before(m.tools[i].holdUntil) {
			return true
		}
	}
	return false
}

// commitExpiredTools commits tools whose holdUntil has passed to the viewport
// and removes them from the live tools list (Phase 1).
func (m *model) commitExpiredTools() {
	now := time.Now()
	var remaining []toolState
	for _, t := range m.tools {
		if !t.holdUntil.IsZero() && now.After(t.holdUntil) {
			// Format the final tool box and append to viewport.
			box := m.renderToolBox(t, false, len(m.tools))
			m.appendMessage("tool", box)
		} else {
			remaining = append(remaining, t)
		}
	}
	m.tools = remaining
	// Adjust focusedTool if committed tools shifted indices.
	if m.focusedTool >= len(m.tools) {
		m.focusedTool = -1
	}
	if m.expandedID != "" && m.findToolIndex(m.expandedID) < 0 {
		m.expandedID = ""
	}
}

// shouldNavigateTools returns true when tool focus navigation is appropriate:
// not in a running response, no active prompt, and input is empty (Phase 1).
func (m *model) shouldNavigateTools() bool {
	return !m.inResponse && m.activePrompt == nil && m.input == ""
}

// focusNextTool moves focusedTool to the next (delta=1) or previous (delta=-1)
// completed tool (status "done" or "error"). Wraps around when at the edges.
// Only considers tools that are not in the hold-state (holdUntil is zero or
// already expired).
func (m *model) focusNextTool(delta int) {
	// Collect indices of eligible tools.
	var eligible []int
	now := time.Now()
	for i, t := range m.tools {
		if t.status == "done" || t.status == "error" {
			// Skip tools still in hold-state.
			if !t.holdUntil.IsZero() && now.Before(t.holdUntil) {
				continue
			}
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
var notificationClearDelay = 4 * time.Second

// setNotification sets m.notification and returns a tea.Cmd that clears it
// after notificationClearDelay, using a generation counter so a newer
// notification is not clobbered by an older timer that fires late (N1).
func (m *model) setNotification(text string) tea.Cmd {
	m.notificationGen++
	m.notification = text
	gen := m.notificationGen
	return tea.Tick(notificationClearDelay, func(t time.Time) tea.Msg {
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
func notifyDurationFromChat(level tauchat.ChatNotificationLevel) time.Duration {
	switch level {
	case tauchat.ChatNotificationError:
		return 0
	case tauchat.ChatNotificationWarn:
		return 8 * time.Second
	default:
		return 5 * time.Second
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
	for i, l := range lines {
		if i == 0 {
			m.renderedLines = append(m.renderedLines, renderLine(role, l))
		} else {
			m.renderedLines = append(m.renderedLines, renderContinuationLine(role, l))
		}
	}
	m.viewport.SetContentLines(m.renderedLines)
	m.viewport.GotoBottom()
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

	// Title line content: glyph + name + status + elapsed.
	title := glyph + " " + label + " " + t.status + elapsedStr

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
		innerTitle := "Full output"
		if len(t.result) == 0 {
			innerTitle = "No output"
		}
		var innerContent strings.Builder
		innerContent.WriteString(innerTitle + "\n")

		if looksLikeMarkdown(t.result) {
			// Wrap as a fenced markdown block and render through
			// the width-memoized glamour cache for syntax highlight.
			md := "```result.md\n" + t.result + "\n```"
			if r, ok := m.mdCache[innerWidth]; ok && r != nil {
				if out, err := r.Render(md); err == nil {
					for line := range strings.SplitSeq(out, "\n") {
						innerContent.WriteString(line + "\n")
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
			innerContent.WriteString(line + "\n")
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

// --- interactive prompt handling -------------------------------------------

func (m *model) enqueuePrompt(e tauchat.InteractivePromptRequestedEvent) tea.Cmd {
	if m.activePrompt != nil {
		m.promptQueue = append(m.promptQueue, e)
		return m.setNotification("prompt queued")
	}
	m.activePrompt = &e
	m.clearInput()
	m.promptConfirmYes = true
	return nil
}

// presentNextQueuedPrompt pops the next queued prompt (if any) into
// activePrompt, resetting per-prompt UI state (input buffer, Yes/No
// highlight) exactly as enqueuePrompt does for the first prompt shown.
func (m *model) presentNextQueuedPrompt() {
	m.activePrompt = nil
	m.clearInput()
	if len(m.promptQueue) == 0 {
		return
	}
	next := m.promptQueue[0]
	m.promptQueue = m.promptQueue[1:]
	m.activePrompt = &next
	m.promptConfirmYes = true
}

func (m *model) handlePromptKey(msg tea.KeyPressMsg) tea.Cmd {
	p := m.activePrompt
	if p == nil {
		return nil
	}
	switch msg.String() {
	case "esc":
		return m.resolvePromptCancel()
	case "enter":
		return m.resolvePrompt(m.input)
	case "y", "Y":
		if p.Kind == "confirm" {
			return m.resolvePromptConfirm(true)
		}
		// msg.String() is used rather than msg.Key().Text since the switch
		// above already guarantees it's exactly "y"/"Y" here.
		m.insertAtCursor(msg.String())
	case "n", "N":
		if p.Kind == "confirm" {
			return m.resolvePromptConfirm(false)
		}
		m.insertAtCursor(msg.String())
	case "tab", "left", "right":
		// Toggle the highlighted Yes/No option — matches the legacy
		// taui.Prompt behaviour (pkg/taui/prompt.go) where bare Enter
		// submits whichever option is currently highlighted, never an
		// unconditional "yes".
		if p.Kind == "confirm" {
			m.promptConfirmYes = !m.promptConfirmYes
		}
	case "backspace":
		m.backspaceAtCursor()
	default:
		if text := msg.Key().Text; text != "" {
			r, _ := utf8.DecodeRuneInString(text)
			if r >= 32 && r != utf8.RuneError {
				m.insertAtCursor(text)
			}
		}
	}
	return nil
}

func (m *model) resolvePrompt(input string) tea.Cmd {
	p := m.activePrompt
	if p == nil {
		return nil
	}

	var cmd tauchat.RespondInteractivePromptCommand
	cmd.RequestID = p.RequestID
	cmd.RespondedAt = time.Now().UTC()
	if p.Kind == "confirm" {
		// Enter submits whichever option is currently highlighted, not an
		// unconditional "yes" — see the tab/left/right case above.
		cmd.Confirmed = m.promptConfirmYes
		cmd.Canceled = !m.promptConfirmYes
	} else {
		cmd.Response = input
	}

	m.presentNextQueuedPrompt()
	return sendCommand(m.runtime, cmd)
}

func (m *model) resolvePromptConfirm(confirmed bool) tea.Cmd {
	p := m.activePrompt
	if p == nil {
		return nil
	}

	m.presentNextQueuedPrompt()
	return sendCommand(m.runtime, tauchat.RespondInteractivePromptCommand{
		RequestID:   p.RequestID,
		Confirmed:   confirmed,
		Canceled:    !confirmed,
		RespondedAt: time.Now().UTC(),
	})
}

func (m *model) resolvePromptCancel() tea.Cmd {
	p := m.activePrompt
	if p == nil {
		return nil
	}

	m.presentNextQueuedPrompt()
	return sendCommand(m.runtime, tauchat.RespondInteractivePromptCommand{
		RequestID:   p.RequestID,
		Canceled:    true,
		RespondedAt: time.Now().UTC(),
	})
}

// activePanel returns the first active plugin panel (if any).
func (m *model) activePanel() *pluginPanel {
	for _, p := range m.panels {
		return &p
	}
	return nil
}

// --- rendering helpers -----------------------------------------------------

func renderPrompt(p *tauchat.InteractivePromptRequestedEvent, confirmYes bool) string {
	var sb strings.Builder
	sb.WriteString(promptBoxStyle.Render("┌─ " + p.Title + " ─┐"))
	sb.WriteString("\n")
	sb.WriteString(promptTextStyle.Render("  " + p.Message))
	sb.WriteString("\n")
	if p.Kind == "confirm" {
		yes, no := "Yes", "No"
		if confirmYes {
			yes = promptHighlightStyle.Render(yes)
		} else {
			no = promptHighlightStyle.Render(no)
		}
		sb.WriteString("  ")
		sb.WriteString(yes)
		sb.WriteString("   ")
		sb.WriteString(no)
		sb.WriteString(promptHintStyle.Render("  (y/n · tab to switch · enter to confirm · esc to cancel)"))
	} else {
		sb.WriteString(promptHintStyle.Render("  [type + enter, esc to cancel]"))
	}
	sb.WriteString("\n")
	sb.WriteString(promptBoxStyle.Render("└" + strings.Repeat("─", 40) + "┘"))
	return sb.String()
}

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
	streamColor    = lipgloss.Color("#FFFFFF") // no theme equivalent
	notifyColor    = themeHex(theme.ToolFailed.FG)
	inputColor     = lipgloss.Color("#7FDBFF") // no theme equivalent

	userStyle      = lipgloss.NewStyle().Foreground(userColor)
	assistantStyle = lipgloss.NewStyle().Foreground(assistantColor)
	reasoningStyle = lipgloss.NewStyle().Foreground(reasoningColor).Italic(true)
	streamStyle    = lipgloss.NewStyle().Foreground(streamColor)
	notifyStyle    = lipgloss.NewStyle().Foreground(notifyColor).Bold(true)
	inputStyle     = lipgloss.NewStyle().Foreground(inputColor)

	userContinuationStyle      = lipgloss.NewStyle().Foreground(userColor).PaddingLeft(6)
	assistantContinuationStyle = lipgloss.NewStyle().Foreground(assistantColor).PaddingLeft(6)
	continuationStyle          = lipgloss.NewStyle().Foreground(themeHex(theme.ToneMuted)).PaddingLeft(6)

	// inputCursorStyle is the block-cursor background — matches the default
	// mid-grey pkg/taui/lineinput.go uses (\x1b[48;2;128;134;150m).
	// #808696 = R=128 G=134 B=150 = theme.ToneMuted.
	inputCursorStyle = lipgloss.NewStyle().Background(themeHex(theme.ToneMuted))

	// Prompt / completion styles.
	promptBoxStyle       = lipgloss.NewStyle().Foreground(themeHex(theme.ToneWarn))
	promptTextStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
	promptHintStyle      = lipgloss.NewStyle().Foreground(themeHex(theme.ToneMuted)).Italic(true)
	promptHighlightStyle = lipgloss.NewStyle().Bold(true).Underline(true)
	compTitleStyle       = lipgloss.NewStyle().Foreground(themeHex(theme.ToneMuted)).Bold(true)
	compItemStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("#CCCCCC"))
	compSelectedStyle    = lipgloss.NewStyle().Foreground(themeHex(theme.CommandFG)).Bold(true)
	compHighlightStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Underline(true)
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

	// toolBoxExpandedStyle is the style for an expanded tool box (subtle border).
	toolBoxExpandedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder(), false, false, false, true).
				BorderForeground(themeHex(theme.ToneMuted)).
				Padding(0, 1)
)
