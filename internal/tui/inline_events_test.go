package tui

import (
	"testing"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// runHandleEvent runs handleEvent in its own goroutine and fails the test if
// it panics or doesn't return within the timeout. handleEvent locks c.mu on
// entry and unlocks/re-locks it around UI calls in several branches; a
// mismatched Lock/Unlock pair either panics ("unlock of unlocked mutex") or
// deadlocks the event loop, so both failure modes need to be caught.
func runHandleEvent(t *testing.T, c *inlineChat, ev tauchat.ChatEvent) {
	t.Helper()

	panicked := make(chan any, 1)
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicked <- r
			}
			close(done)
		}()
		c.handleEvent(ev)
	}()

	select {
	case <-done:
		select {
		case r := <-panicked:
			t.Fatalf("handleEvent(%T) panicked: %v", ev, r)
		default:
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("handleEvent(%T) did not return — likely deadlock", ev)
	}
}

// TestHandleEvent_ToolExecutionCompleted_NoDoubleUnlock guards against a
// regression where the mutex was unlocked twice in a row (once before the
// tool box render, again before UpdateThenPrint, with no re-lock between),
// panicking on every tool call completion.
func TestHandleEvent_ToolExecutionCompleted_NoDoubleUnlock(t *testing.T) {
	c, _ := newTestChat(t)
	c.activeTools = map[string]*activeToolBox{}

	runHandleEvent(t, c, tauchat.ChatToolExecutionCompletedEvent{
		CallID:        "missing",
		ToolName:      "read",
		ResultSummary: "ok",
		CompletedAt:   time.Now(),
	})
}

// TestHandleEvent_ExtensionEvents_NoSelfDeadlock guards against a regression
// where handleEvent called setExtensionCommands (which locks c.mu itself)
// while still holding c.mu, deadlocking the event loop on every plugin
// reload or extension command change.
func TestHandleEvent_ExtensionEvents_NoSelfDeadlock(t *testing.T) {
	c, _ := newTestChat(t)

	runHandleEvent(t, c, tauchat.ExtensionsReloadedEvent{
		Result: tauchat.ExtensionReloadResult{ExtensionCount: 1},
	})
	runHandleEvent(t, c, tauchat.ExtensionCommandsChangedEvent{
		Commands: []tauchat.ExtensionCommand{{Name: "foo"}},
	})
}

// TestHandleEvent_ReasoningDelta_HiddenDoesNotPanic guards against a
// regression where the early return for a hidden reasoning delta skipped
// re-acquiring the mutex it had released, panicking on the deferred unlock.
func TestHandleEvent_ReasoningDelta_HiddenDoesNotPanic(t *testing.T) {
	c, _ := newTestChat(t)
	c.showReasoning = false

	runHandleEvent(t, c, tauchat.ChatReasoningDeltaEvent{Delta: "thinking…"})
}
