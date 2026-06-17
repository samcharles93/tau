package tui

import (
	"time"

	gt "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
)

// KeyMap defines app-level keyboard shortcuts.
func (c *ChatPanel) KeyMap() gt.KeyMap {
	km := gt.KeyMap{
		gt.On(gt.KeyEscape, func(ke gt.KeyEvent) {
			if c.showDebug.Get() || c.showDebugList.Get() {
				c.showDebug.Set(false)
				c.showDebugList.Set(false)
				return
			}
			if c.showSessionList.Get() {
				c.showSessionList.Set(false)
				return
			}
			if c.showHelp.Get() {
				c.showHelp.Set(false)
				return
			}
			if c.showSettings.Get() {
				c.showSettings.Set(false)
				return
			}
			if c.showSessionTree.Get() {
				c.showSessionTree.Set(false)
				return
			}
			if len(c.completions.Get()) > 0 {
				c.closeCompletions()
				return
			}
			if c.inputValue.Get() != "" {
				c.clearInput()
				return
			}
			c.notice.Set("Press Ctrl+C to quit")
		}),
		gt.On(gt.KeyTab, func(ke gt.KeyEvent) {
			c.applySelectedCompletion()
		}),
		gt.On(gt.KeyCtrlC, func(ke gt.KeyEvent) {
			if c.showSettings.Get() || c.showSessionTree.Get() || c.showSessionList.Get() || c.showDebug.Get() || c.showDebugList.Get() || c.showHelp.Get() {
				c.showSettings.Set(false)
				c.showSessionTree.Set(false)
				c.showSessionList.Set(false)
				c.showDebug.Set(false)
				c.showDebugList.Set(false)
				c.showHelp.Set(false)
				return
			}
			switch {
			case c.status.Get() == tauchat.ChatSessionStreaming:
				c.status.Set(tauchat.ChatSessionCancelling)
				c.sendCommand(tauchat.CancelChatRequestCommand{
					SessionID:   c.cfg.SessionID,
					RequestID:   c.activeRequestID.Get(),
					RequestedAt: time.Now().UTC(),
				})
				c.notice.Set("cancelling… Ctrl+C again to quit")
			case c.inputValue.Get() != "":
				c.clearInput()
			default:
				ke.App().Stop()
			}
		}),
		gt.On(gt.KeyCtrlR, func(ke gt.KeyEvent) {
			c.showReasoning.Set(!c.showReasoning.Get())
		}),
		gt.On(gt.KeyPageUp, func(ke gt.KeyEvent) {
			if c.messageViewport != nil {
				_, h := c.messageViewport.ViewportSize()
				c.messageViewport.ScrollBy(0, -h)
			}
		}),
		gt.On(gt.KeyPageDown, func(ke gt.KeyEvent) {
			if c.messageViewport != nil {
				_, h := c.messageViewport.ViewportSize()
				c.messageViewport.ScrollBy(0, h)
			}
		}),
		gt.On(gt.KeyUp, func(ke gt.KeyEvent) {
			if len(c.completions.Get()) > 0 {
				c.selectCompletion(-1)
				return
			}
			if c.messageViewport != nil {
				c.messageViewport.ScrollBy(0, -1)
			}
		}),
		gt.On(gt.KeyDown, func(ke gt.KeyEvent) {
			if len(c.completions.Get()) > 0 {
				c.selectCompletion(1)
				return
			}
			if c.messageViewport != nil {
				c.messageViewport.ScrollBy(0, 1)
			}
		}),
	}
	return km
}

// wheelScrollLines is how many lines one mouse-wheel notch scrolls the message
// viewport.
const wheelScrollLines = 3

// HandleMouse implements gt.MouseListener. The go-tui app dispatches mouse
// events to MouseListener components before element hit-testing, which is what
// makes this work: hit-testing resolves the wheel to the deepest element under
// the cursor (usually a non-scrollable text leaf) and does not bubble to the
// scrollable viewport ancestor, so wheel-over-content would otherwise do
// nothing. Handling it here scrolls regardless of what sits under the cursor.
//
// Only wheel events are consumed. Clicks and drags are left unhandled so
// terminal-level selection (Shift+drag in most terminals) keeps working.
func (c *ChatPanel) HandleMouse(me gt.MouseEvent) bool {
	if c.messageViewport == nil {
		return false // inline mode, or before the first full-screen render
	}
	// While a full-screen overlay (settings, help, session views) owns the
	// screen, leave the wheel for it rather than scrolling the hidden chat.
	for _, s := range c.alternateScreenStates() {
		if s.Get() {
			return false
		}
	}
	switch me.Button {
	case gt.MouseWheelUp:
		c.messageViewport.ScrollBy(0, -wheelScrollLines)
		return true
	case gt.MouseWheelDown:
		c.messageViewport.ScrollBy(0, wheelScrollLines)
		return true
	default:
		return false
	}
}
