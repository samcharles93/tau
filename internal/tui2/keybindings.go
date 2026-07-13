package tui2

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/google/uuid"
	tauchat "github.com/samcharles93/tau/internal/chat"
)

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

func (m *model) dispatchKey(msg tea.KeyPressMsg) tea.Cmd {
	// Interactive prompt active: route keys to prompt handler.
	if m.activePrompt != nil {
		return m.handlePromptKey(msg)
	}

	// /help overlay open: any key closes it (see handleHelpOverlayKey) — it's
	// a reference card, not something you type into, so there's no reason to
	// route specific keys anywhere else while it's up.
	if m.helpOverlay != nil {
		return m.handleHelpOverlayKey(msg)
	}

	// Diff viewer open: route keys to it, above the context menu — opening
	// the viewer always closes the menu that spawned it, so the two are
	// mutually exclusive, but this ordering keeps that invariant explicit.
	if m.diffViewer != nil {
		return m.handleDiffViewerKey(msg)
	}

	// Child transcript viewer open: same exclusivity story as diffViewer —
	// opening either overlay closes the other (see openChildTranscriptViewer).
	if m.childTranscriptViewer != nil {
		return m.handleChildTranscriptViewerKey(msg)
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

	case "ctrl+r":
		// Toggles the reasoning block from the turn that just finished —
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

	case "ctrl+home":
		m.viewport.GotoTop()
		m.autoFollow = false
		return nil

	case "ctrl+end":
		m.viewport.GotoBottom()
		m.autoFollow = true
		return nil

	case "ctrl+shift+l":
		m.clearScreen()
		return nil

	case "ctrl+?", "ctrl+shift+/":
		return m.cmdHelp("")

	case "esc":
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
			if m.focusedTool == -1 {
				m.focusNextChild(1)
			}
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
		// Drill down into a focused finished child agent's transcript.
		if m.focusedChild >= 0 && m.input == "" && !m.inResponse && m.activePrompt == nil {
			return m.openChildTranscriptViewer(m.childAgentOrder[m.focusedChild])
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
		m.history = append(m.history, text)
		m.historyIdx = -1
		return m.startOrQueueTurn(text)
	}

	m.clearInput()
	if text == "" {
		// No visible feedback needed here — the status bar already shows a
		// "steering…" segment (see computeStatusBar) whenever m.steering is true.
		m.steering = !m.steering
		return nil
	}
	m.history = append(m.history, text)
	m.historyIdx = -1
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
// inlineCtrl.HandleInput: cancel an in-flight turn or bash command first,
// clear any pending input next, and only treat Ctrl+C as "quit" (with a
// double-tap confirmation) when there's nothing running or typed to clear —
// so an accidental Ctrl+C during generation never silently kills the
// program.
func (m *model) handleCtrlC() tea.Cmd {
	if m.inResponse {
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
