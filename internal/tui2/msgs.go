package tui2

import (
	"time"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
)

// noopCmd performs no action. Used where a handler must always return a
// non-nil Cmd even when there's nothing to schedule.
var noopCmd tea.Cmd = func() tea.Msg { return nil }

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
// bashSendResultMsg - kept distinct from sendResultMsg so a failed send can
// clear bashRunning/bashCallID specifically, without misreading an unrelated
// in-flight chat turn as failed (or vice versa).
func sendBashCommand(runtime tauchat.ChatRuntime, cmd tauchat.RunBashCommand) tea.Cmd {
	return func() tea.Msg {
		return bashSendResultMsg{err: runtime.Send(cmd)}
	}
}
