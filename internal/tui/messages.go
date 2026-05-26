package tui

import (
	tea "charm.land/bubbletea/v2"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/tui/notify"
)

// runtimeEventMsg wraps a chat runtime event for the Bubble Tea update loop.
type runtimeEventMsg struct {
	event tauchat.ChatEvent
}

// runtimeClosedMsg signals the event subscription channel has closed.
type runtimeClosedMsg struct{}

// notifyExpiredMsg signals that the front notification may have expired.
type notifyExpiredMsg struct{}

// helpExpiredMsg hides the expanded help after the display timeout.
type helpExpiredMsg struct{}

// notifyReceivedMsg delivers an external notification from the pubsub bus.
type notifyReceivedMsg struct {
	notification notify.Notification
}

// modelsRefreshedMsg delivers the results of an async model refresh.
type modelsRefreshedMsg struct {
	models []tauchat.ChatModelRef
	err    error
}

// waitForRuntimeEvent returns a tea.Cmd that blocks until the next event
// arrives on the subscription channel, then delivers it as a runtimeEventMsg.
// When the channel closes it delivers runtimeClosedMsg.
func waitForRuntimeEvent(events <-chan tauchat.ChatEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return runtimeClosedMsg{}
		}
		return runtimeEventMsg{event: event}
	}
}

// waitForNotification returns a tea.Cmd that blocks until a notification
// arrives on the subscription channel.
func waitForNotification(ch <-chan notify.Notification) tea.Cmd {
	return func() tea.Msg {
		n, ok := <-ch
		if !ok {
			return nil
		}
		return notifyReceivedMsg{notification: n}
	}
}
