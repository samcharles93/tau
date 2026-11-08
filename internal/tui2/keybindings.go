package tui2

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// handleKey dispatches a keypress and then re-syncs the completions
// selection against whatever the keystroke just did to m.input. The sync
// can't happen only inside handleCompletionKey's own pre-dispatch check -
// that check runs BEFORE a character insertion/deletion below it in the same
// keystroke, so it always compares against the token as of the START of this
// call. Without the post-dispatch sync, a query-narrowing keystroke leaves
// compSelected pointing at a stale index for one extra render frame instead
// of resetting immediately.
func (m *model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	cmd := m.dispatchKey(msg)
	m.syncCompletionSelection()
	if paletteCmd, opened := m.maybeOpenInputPalette(); opened {
		return tea.Batch(cmd, paletteCmd)
	}
	return tea.Batch(cmd, m.maybePrefetchSessions())
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

// dispatchKey routes msg by precedence: an active exclusive overlay
// (prompt > help > diff > child transcript > session tree > context menu,
// per overlayPrecedence in overlay.go) always wins outright; otherwise a
// pending input-selection clear runs, then the soft completions dropdown
// gets first refusal, and only then do normal keybindings apply.
func (m *model) dispatchKey(msg tea.KeyPressMsg) tea.Cmd {
	if cmd, ok := m.dispatchExclusiveOverlayKey(msg); ok {
		return cmd
	}
	m.clearInputSelOnCursorMove(msg)

	// The completions dropdown gets first refusal on every keystroke while
	// visible - matches taui's OverlayStack precedence (a "soft" overlay
	// that consumes only the keys it recognizes; everything else falls
	// through unchanged to the bindings below).
	if cmd, handled := m.handleCompletionKey(msg); handled {
		return cmd
	}

	if cmd, handled := m.dispatchActionKey(msg); handled {
		return cmd
	}
	if cmd, handled := m.dispatchViewKey(msg); handled {
		return cmd
	}
	if cmd, handled := m.dispatchVerticalKey(msg); handled {
		return cmd
	}
	if cmd, handled := m.dispatchToggleKey(msg); handled {
		return cmd
	}
	if cmd, handled := m.dispatchSubmitKey(msg); handled {
		return cmd
	}
	if cmd, handled := m.dispatchCursorKey(msg); handled {
		return cmd
	}
	return m.dispatchEditKey(msg)
}

// clearInputSelOnCursorMove clears a stale input selection when a pure
// cursor-movement key arrives (those keys don't edit the buffer, so they'd
// otherwise leave the selection highlighted while the cursor visibly moves
// away from it). Insert/backspace/delete consume the selection themselves
// (deleteInputSelection) and must NOT be cleared here first.
func (m *model) clearInputSelOnCursorMove(msg tea.KeyPressMsg) {
	if !m.inputSel.armed() {
		return
	}
	switch msg.String() {
	case "up", "down", "left", "right", "home", "end",
		"ctrl+a", "ctrl+e", "ctrl+left", "ctrl+right", "alt+left", "alt+right":
		m.inputSel.clear()
	}
}

// handleCtrlRKey implements Ctrl+R: in peek mode while reasoning is still
// streaming, widen the live tail to the full trace (and back) for the rest
// of this turn; otherwise toggle the reasoning block from the turn that just
// finished (a no-op while reasoning is off, before any turn has completed,
// or while the current turn's reasoning is still streaming).
func (m *model) handleCtrlRKey() tea.Cmd { // In peek mode, while reasoning is still streaming, widen the live
	// tail to the full trace (and back) for the rest of this turn.
	if m.reasoningPeek && m.reasoning != "" {
		m.peekExpanded = !m.peekExpanded
		return nil
	}
	// Otherwise toggle the reasoning block from the turn that just finished -
	// there's no per-block focus-navigation the way tools have
	// (focusNextTool), so this always reaches the most recent one
	// (m.lastReasoningKey). A no-op while reasoning is off, before any
	// turn has completed, or while the current turn's reasoning is
	// still streaming (nothing committed yet to toggle).
	if m.showReasoning && m.lastReasoningKey != "" {
		if m.toggleReasoningBlock(m.lastReasoningKey) {
			m.clearAllSelections()
		}
	}
	return nil
}

// handleEscKey implements Esc: cancel a running bash command, clear any
// armed selection, step out of an expanded/focused tool, or clear the
// input - in that order.
func (m *model) handleEscKey() tea.Cmd {
	if m.bashRunning {
		return m.cancelBash()
	}
	if m.viewportSel.armed() || m.inputSel.armed() || m.statusSel.armed() || m.toolsSel.armed() {
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
}

// dispatchActionKey handles the app-level action bindings (quit/steer/paste/copy, scroll, palette/help openers, Ctrl+R peek toggle, Esc). Returns (cmd, handled).
// dispatchActionKey handles the turn-level action bindings (Ctrl+C/D/S/V, copy, Ctrl+R peek toggle, Esc). Returns (cmd, handled).
func (m *model) dispatchActionKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "ctrl+c":
		return m.handleCtrlC(), true
	case "ctrl+d":
		if m.input == "" {
			return tea.Quit, true
		}
		m.clearInput()
		return nil, true
	case "ctrl+s":
		return m.handleSteer(), true
	case "ctrl+v":
		// Fallback for terminals that don't deliver bracketed pastes for
		// ctrl+v: ask for the clipboard via OSC 52; the ClipboardMsg reply
		// is routed through the same image-aware handlePaste. Terminals
		// with bracketed paste never send this key (it arrives as a
		// PasteMsg instead), so the two paths don't double-insert.
		return tea.ReadClipboard, true
	case "ctrl+shift+g":
		return m.cmdCopy(""), true
	case "ctrl+r":
		return m.handleCtrlRKey(), true
	case "esc":
		return m.handleEscKey(), true
	}
	return nil, false
}

// dispatchViewKey handles the view bindings: viewport scrolling (pgup/pgdown/home/end), clear screen, and the palette/help/session-tree openers. Returns (cmd, handled).
func (m *model) dispatchViewKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "pgup":
		m.viewport.HalfPageUp()
		m.autoFollow = false
		return nil, true
	case "pgdown":
		m.viewport.HalfPageDown()
		if m.viewport.AtBottom() {
			m.autoFollow = true
		}
		return nil, true
	case "ctrl+home":
		m.viewport.GotoTop()
		m.autoFollow = false
		return nil, true
	case "ctrl+end":
		m.viewport.GotoBottom()
		m.autoFollow = true
		return nil, true
	case "ctrl+shift+l":
		m.clearScreen()
		return nil, true
	case "ctrl+p":
		return m.openCommandPalette(), true
	case "ctrl+l":
		return m.openModelPalette(), true
	case "ctrl+o":
		return m.openSessionTree(), true
	case "ctrl+?", "ctrl+shift+/":
		return m.cmdHelp(""), true
	}
	return nil, false
}

// dispatchVerticalKey handles Up/Down: history recall at the first/last logical line, tool-focus navigation when history is exhausted, vertical cursor movement otherwise. Returns (cmd, handled).
func (m *model) dispatchVerticalKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "up":

		// Up/Down recall history from the first/last logical line, and move the
		// cursor vertically within a multi-line buffer otherwise - matching
		// pkg/taui/lineinput.go's atFirstLineStart/atLastLineEnd gate.
		// Phase 1: when input is empty and history is exhausted, Up/Down navigate
		// tool focus among completed tools.

		if m.atFirstLineStart() {
			m.recallHistory(-1)
			// No history to recall - navigate tool focus.
			if m.shouldNavigateTools() {
				m.focusNextTool(-1)
			}
			return nil, true
		}
		m.moveCursorVert(-1)
		return nil, true
	case "down":
		if m.atLastLineEnd() {
			m.recallHistory(1)
			if m.shouldNavigateTools() {
				m.focusNextTool(1)
			}
			return nil, true
		}
		m.moveCursorVert(1)
		return nil, true
	}
	return nil, false
}

// dispatchToggleKey handles Tab/Shift+Tab/Enter/Space: tool-focus and child cycling, input-mode cycling, tool expansion, transcript drill-down, submit. Returns (cmd, handled).
// dispatchToggleKey handles Tab/Shift+Tab: tool-focus and child cycling, input-mode cycling. Returns (cmd, handled).
func (m *model) dispatchToggleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "tab":
		// Tab is only meaningful while the completions dropdown is visible
		// (handleCompletionKey, checked before this switch, handles that
		// case) - matches taui, where a bare Tab with no dropdown showing is
		// a no-op (LineInput has no binding for it).
		// Phase 1: when not in response, no prompt, and input empty, Tab
		// navigates tool focus among completed tools.
		if m.shouldNavigateTools() {
			m.focusNextTool(1)
			if m.focusedTool == -1 {
				m.focusNextChild(1)
			}
		}
		return nil, true
	case "shift+tab":
		m.cycleInputMode()
		return nil, true
	}
	return nil, false
}

// dispatchSubmitKey handles Enter/Space: tool expansion on a focused tool, child transcript drill-down, insert-space, or submit. Returns (cmd, handled).
func (m *model) dispatchSubmitKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "enter":
		// Phase 1: toggle tool expansion when a tool is focused.
		if m.focusedTool >= 0 && m.input == "" && !m.inResponse() && m.activePrompt == nil {
			return m.toggleToolExpansion(), true
		}
		// Drill down into a focused finished child agent's transcript.
		if m.focusedChild >= 0 && m.input == "" && !m.inResponse() && m.activePrompt == nil {
			return m.openChildTranscriptViewer(m.childAgentOrder[m.focusedChild]), true
		}
		return m.submitInput(), true
	case "space":
		// Space also toggles expansion on a focused tool; otherwise it's a
		// normal printable character handled by the default case below.
		if m.focusedTool >= 0 && m.input == "" && !m.inResponse() && m.activePrompt == nil {
			return m.toggleToolExpansion(), true
		}
		m.insertAtCursor(" ")
		return nil, true
	}
	return nil, false
}

// dispatchCursorKey handles pure cursor movement within the input buffer. Returns (cmd, handled).
func (m *model) dispatchCursorKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "left":
		m.moveCursorLeft()
		return nil, true
	case "right":
		m.moveCursorRight()
		return nil, true
	case "ctrl+left", "alt+left":
		m.inputCursor = m.wordLeft()
		return nil, true
	case "ctrl+right", "alt+right":
		m.inputCursor = m.wordRight()
		return nil, true
	case "home", "ctrl+a":
		m.inputCursor = 0
		return nil, true
	case "end", "ctrl+e":
		m.inputCursor = utf8.RuneCountInString(m.input)
		return nil, true
	}
	return nil, false
}

// dispatchEditKey handles buffer-editing keys (kill to line start/end, kill
// word, newline, backspace/delete) and, by default, printable-character
// insertion. Always consumes the key (returns nil when it was a no-op).
func (m *model) dispatchEditKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+u":
		m.killToLineStart()
		return nil
	case "ctrl+k":
		m.killToLineEnd()
		return nil
	case "ctrl+w", "ctrl+backspace", "ctrl+h", "alt+backspace":
		// Most terminals send ^H for Ctrl+Backspace unless an enhanced keyboard
		// protocol is active, which Bubble Tea reports as ctrl+h.

		m.deleteWordBeforeCursor()
		return nil
	case "shift+enter", "ctrl+j":

		// Shift+Enter/Ctrl+J inserts a newline; bare Enter submits.

		m.insertAtCursor("\n")
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

// handleSteer sends a steering command mid-turn, or - while idle - falls
// through to a normal submit rather than rejecting the keystroke, so
// whatever the user typed is never silently lost. Mirrors
// internal/tui/inline_chat.go's onSteer.
func (m *model) handleSteer() tea.Cmd {
	text := strings.TrimSpace(m.input)
	attachments := m.liveAttachments(text)
	// Same vision gate as submitInput - a steering message with images would
	// hit the same text-only-model rejection mid-turn.
	if msg := m.imageGateNotification(attachments); msg != "" {
		return m.setNotification(msg)
	}

	if !m.inResponse() {
		if text == "" {
			return nil
		}
		m.clearInput()
		m.history = append(m.history, text)
		m.historyIdx = -1
		return m.startOrQueueTurn(text, attachments)
	}

	m.clearInput()
	if text == "" {
		// No visible feedback needed here - the status bar already shows a
		// "steering…" segment (see computeStatusBar) whenever m.steering is true.
		m.steering = !m.steering
		return nil
	}
	m.history = append(m.history, text)
	m.historyIdx = -1
	m.steering = true
	return sendCommand(m.runtime, tauchat.SteerChatPromptCommand{
		SessionID:   m.sessionID,
		RequestID:   tauchat.NewRequestID(),
		Prompt:      text,
		Attachments: attachments,
		SubmittedAt: time.Now().UTC(),
	})
}

// handleBashCommand runs a "!" (or "!!") bash-mode command. trimmed is the
// full submitted text, bang(s) included - "!!" (or "!!!", "!!!!", ...) marks
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

	callID := "bash-" + tauchat.NewRequestID()
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
// quit" - matches internal/tui/inline_chat.go's quitConfirmWindow.
const quitConfirmWindow = 800 * time.Millisecond

// handleCtrlC triages a Ctrl+C press exactly like inline_chat.go's
// inlineCtrl.HandleInput: cancel an in-flight turn or bash command first,
// clear any pending input next, and only treat Ctrl+C as "quit" (with a
// double-tap confirmation) when there's nothing running or typed to clear -
// so an accidental Ctrl+C during generation never silently kills the
// program.
func (m *model) handleCtrlC() tea.Cmd {
	if m.inResponse() {
		return m.cancelTurn()
	}
	if m.bashRunning {
		return m.cancelBash()
	}
	if m.input != "" {
		m.clearInput()
		return nil
	}
	now := time.Now()
	if now.Sub(m.pendingQuit) < quitConfirmWindow {
		return tea.Quit
	}
	m.pendingQuit = now
	return m.setNotification("quit: press Ctrl+C again")
}

// cancelTurn sends a CancelChatRequestCommand to stop the current
// generation. m.agentState (and so m.inResponse()) is cleared asynchronously
// by the resulting ChatResponseCancelledEvent, not here.
func (m *model) cancelTurn() tea.Cmd {
	m.steering = false
	return sendCommand(m.runtime, tauchat.CancelChatRequestCommand{
		SessionID:   m.sessionID,
		RequestedAt: time.Now().UTC(),
	})
}
