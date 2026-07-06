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
	// room for the header/streaming/separator/input/status bar) — the actual
	// per-frame height is shrunk to fit shorter content in View(), so short
	// conversations don't leave a blank gap between the last message and the
	// input (tui2 runs inline, not alt-screen, so unused viewport height is
	// just wasted vertical space, not a fixed full-screen canvas).
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
	turnQueue  []string // queued prompts behind a running turn
	lastSubmit time.Time

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
}

type toolState struct {
	id     string
	name   string
	args   string
	result string
	status string // "pending", "running", "done", "error"
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
	usage *metrics.UsageTracker,
	webURL string,
	debug bool,
) *model {
	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	vp.SoftWrap = true
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
		availableModels:   availableModels,
		refresh:           refresh,
		usage:             usage,
		webURL:            webURL,
		debug:             debug,
		notifyQ:           notify.NewQueue(),
		extensionCommands: make(map[string]tauchat.ExtensionCommand),
		panels:            make(map[string]pluginPanel),
	}
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
		// Reserve space for header (~3 lines), streaming text, separator,
		// input line, and status bar (~5 lines total). View() shrinks the
		// actual per-frame height to fit shorter content — see
		// maxViewportHeight's doc comment.
		m.maxViewportHeight = max(msg.Height-5, 4)
		m.viewport.SetHeight(m.maxViewportHeight)
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
	var sb strings.Builder

	// Header.
	sb.WriteString(headerStyle.Render(" tau — Bubbletea v2 (experimental)"))
	sb.WriteString("\n\n")

	// Plugin panel (if active).
	if p := m.activePanel(); p != nil {
		sb.WriteString(panelStyle.Render("┌─ " + p.title + " ─┐"))
		sb.WriteString("\n")
		sb.WriteString(p.content)
		sb.WriteString("\n")
		sb.WriteString(panelStyle.Render("└" + strings.Repeat("─", min(m.width, 40)) + "┘"))
		sb.WriteString("\n\n")
	}

	// Messages — rendered through the viewport (scrollable, no cap). Shrunk
	// to the actual content height (capped at maxViewportHeight) each frame
	// so a short conversation doesn't pad out with blank lines down to the
	// input — TotalLineCount only depends on content+width, not the
	// currently-set Height, so this is safe to query before resizing.
	if m.maxViewportHeight > 0 {
		m.viewport.SetHeight(max(min(m.viewport.TotalLineCount(), m.maxViewportHeight), 1))
	}
	sb.WriteString(m.viewport.View())

	// Streaming text (in-progress assistant response).
	if m.reasoning != "" && m.showReasoning {
		sb.WriteString(reasoningStyle.Render("Thinking: " + m.reasoning))
		sb.WriteString("\n")
	}
	if m.streaming != "" {
		sb.WriteString(streamStyle.Render(m.streaming))
	}

	// Tool calls.
	for _, t := range m.tools {
		sb.WriteString("\n")
		sb.WriteString(renderTool(t))
	}
	if len(m.tools) > 0 {
		sb.WriteString("\n")
	}

	// Interactive prompt (modal).
	if m.activePrompt != nil {
		sb.WriteString("\n")
		sb.WriteString(renderPrompt(m.activePrompt, m.promptConfirmYes))
		sb.WriteString("\n")
	}

	// Completion dropdown.
	if rows, _ := m.completionRows(); len(rows) > 0 {
		sb.WriteString("\n")
		selected := m.compSelected
		if selected < 0 || selected >= len(rows) {
			selected = 0
		}
		sb.WriteString(renderCompletions(rows, selected, m.width))
	}

	// Notification (Phase 1 compat).
	if m.notification != "" {
		sb.WriteString("\n")
		sb.WriteString(notifyStyle.Render(m.notification))
	}

	sb.WriteString("\n")

	// Separator before input area.
	sepWidth := m.width
	if sepWidth <= 0 {
		sepWidth = 80
	}
	sb.WriteString(strings.Repeat("─", sepWidth))
	sb.WriteString("\n")

	// Input area.
	if m.activePrompt != nil {
		if m.activePrompt.Kind == "confirm" {
			// Nothing — input is blocked
		} else {
			sb.WriteString(m.renderInputArea())
		}
	} else if m.inResponse && !m.steering {
		sb.WriteString(inputStyle.Render("waiting for response…"))
	} else {
		sb.WriteString(m.renderInputArea())
	}
	sb.WriteString("\n")

	// Status bar — rich, segmented.
	if m.width > 0 {
		sb.WriteString(m.computeStatusBar())
	}

	v := tea.NewView(sb.String())
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

	case "ctrl+l":
		m.clearScreen()
		return nil

	case "esc":
		if m.bashRunning {
			return m.cancelBash()
		}
		if m.input != "" {
			m.clearInput()
			return nil
		}
		return nil

	// Up/Down recall history from the first/last logical line, and move the
	// cursor vertically within a multi-line buffer otherwise — matching
	// pkg/taui/lineinput.go's atFirstLineStart/atLastLineEnd gate.
	case "up":
		if m.atFirstLineStart() {
			return m.recallHistory(-1)
		}
		m.moveCursorVert(-1)
		return nil
	case "down":
		if m.atLastLineEnd() {
			return m.recallHistory(1)
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
		return nil

	case "enter":
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

	out := make([]string, len(lines))
	for i, ln := range lines {
		prefix := "> "
		if i > 0 {
			prefix = "  "
		}
		if i == curLine {
			out[i] = prefix + renderLineWithCursor(ln, curCol)
		} else {
			out[i] = prefix + inputStyle.Render(string(ln))
		}
	}
	return strings.Join(out, "\n")
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

func (m *model) submitInput() tea.Cmd {
	// Interactive prompt active: handle prompt input.
	if m.activePrompt != nil {
		return m.resolvePrompt(m.input)
	}

	// N6: guard against double-submit while a response is in-flight.
	if m.inResponse || m.bashRunning {
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

	// Bash mode: !command runs outside the LLM turn loop.
	if after, ok := strings.CutPrefix(text, "!"); ok {
		return m.handleBashCommand(after)
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

// handleSteer sends a steering command mid-turn.
func (m *model) handleSteer() tea.Cmd {
	if !m.inResponse {
		return m.setNotification("no active response to steer")
	}
	text := strings.TrimSpace(m.input)
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

// handleBashCommand runs a shell command outside the LLM turn loop. The
// CallID is generated here (not by the coordinator) and recorded in
// m.bashCallID before the command is sent, so the matching
// ChatToolExecutionCompletedEvent can be recognised as "ours" and clear
// bashRunning — mirroring the legacy inline_chat.go behaviour.
func (m *model) handleBashCommand(cmd string) tea.Cmd {
	callID := "bash-" + newRequestID()
	m.bashRunning = true
	m.bashCallID = callID
	m.appendMessage("user", "!"+cmd)
	return sendBashCommand(m.runtime, tauchat.RunBashCommand{
		SessionID:   m.sessionID,
		CallID:      callID,
		Command:     cmd,
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
		if m.bashCallID != "" && e.CallID == m.bashCallID {
			m.bashRunning = false
			m.bashCallID = ""
		}

	case tauchat.ChatToolOutputEvent:
		m.setToolResult(e.CallID, e.Chunk)

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
		m.inResponse = false
		return m.setNotification(fmt.Sprintf("error: %s", e.Message))

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
		return m.setNotification("session loaded: " + e.State.SessionID)

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
		return m.setNotification(fmt.Sprintf("skills: %d available", len(e.Skills)))

	default:
		return nil
	}
	return nil
}

func (m *model) applySnapshot(e tauchat.ChatSessionSnapshotEvent) {
	state := e.State
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
				m.renderedLines = append(m.renderedLines, continuationStyle.Render(l))
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
		m.appendMessage("assistant", content)
	}
	m.streaming = ""
	m.reasoning = ""
	m.inResponse = false
	return content
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
		id:     callID,
		name:   toolName,
		args:   argumentsSummary,
		status: "pending",
	})
}

func (m *model) setToolStatus(id, status string) {
	for i := range m.tools {
		if m.tools[i].id == id {
			m.tools[i].status = status
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

// --- notification helper ---------------------------------------------------

// setNotification sets m.notification and returns a tea.Cmd that clears it
// after 4 seconds, using a generation counter so a newer notification is not
// clobbered by an older timer that fires late (N1).
func (m *model) setNotification(text string) tea.Cmd {
	m.notificationGen++
	m.notification = text
	gen := m.notificationGen
	return tea.Tick(4*time.Second, func(t time.Time) tea.Msg {
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
			m.renderedLines = append(m.renderedLines, continuationStyle.Render(l))
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

func renderTool(t toolState) string {
	line := fmt.Sprintf("  %s", t.name)
	// N11: render a short result summary when available.
	const resultLimit = 60
	if t.result != "" {
		summary := strings.ReplaceAll(t.result, "\n", " ")
		if len(summary) > resultLimit {
			summary = summary[:resultLimit] + "…"
		}
		line += " — " + summary
	}
	return toolStyle.Render(line)
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
	// Brand color — no exact theme match.
	headerColor = lipgloss.Color("#7B2FBE")
	// Semantic colors sourced from internal/theme.
	userColor      = themeHex(theme.CommandFG)
	assistantColor = themeHex(theme.ToolSuccess.FG)
	reasoningColor = themeHex(theme.ToneWarn)
	streamColor    = lipgloss.Color("#FFFFFF") // no theme equivalent
	toolColor      = lipgloss.Color("#FFDC00") // no theme equivalent
	notifyColor    = themeHex(theme.ToolFailed.FG)
	inputColor     = lipgloss.Color("#7FDBFF") // no theme equivalent

	headerStyle       = lipgloss.NewStyle().Bold(true).Foreground(headerColor).Padding(0, 1)
	userStyle         = lipgloss.NewStyle().Foreground(userColor)
	assistantStyle    = lipgloss.NewStyle().Foreground(assistantColor)
	reasoningStyle    = lipgloss.NewStyle().Foreground(reasoningColor).Italic(true)
	streamStyle       = lipgloss.NewStyle().Foreground(streamColor)
	toolStyle         = lipgloss.NewStyle().Foreground(toolColor)
	notifyStyle       = lipgloss.NewStyle().Foreground(notifyColor).Bold(true)
	inputStyle        = lipgloss.NewStyle().Foreground(inputColor)
	continuationStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).PaddingLeft(6)

	// inputCursorStyle is the block-cursor background — matches the default
	// mid-grey pkg/taui/lineinput.go uses (\x1b[48;2;128;134;150m).
	inputCursorStyle = lipgloss.NewStyle().Background(lipgloss.Color("#808696"))

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
)
