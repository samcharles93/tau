package tui

import (
	aimchat "bitbucket.srv.westpac.com.au/m055731/aim/internal/chat"
	tea "charm.land/bubbletea/v2"
)

// runtimeEventMsg wraps a chat runtime event for the Bubble Tea update loop.
type runtimeEventMsg struct {
	event aimchat.ChatEvent
}

// runtimeClosedMsg signals the event subscription channel has closed.
type runtimeClosedMsg struct{}

// waitForRuntimeEvent returns a tea.Cmd that blocks until the next event
// arrives on the subscription channel, then delivers it as a runtimeEventMsg.
// When the channel closes it delivers runtimeClosedMsg.
func waitForRuntimeEvent(events <-chan aimchat.ChatEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return runtimeClosedMsg{}
		}
		return runtimeEventMsg{event: event}
	}
}
