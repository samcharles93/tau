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
	}
	if len(c.completions.Get()) > 0 {
		km = append(km,
			gt.OnPreemptStop(gt.KeyUp, func(gt.KeyEvent) { c.selectCompletion(-1) }),
			gt.OnPreemptStop(gt.KeyDown, func(gt.KeyEvent) { c.selectCompletion(1) }),
		)
	}
	return km
}
