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

func demoView(id string) tauchat.ExtensionView {
	return tauchat.ExtensionView{
		ID:    id,
		Title: "Demo",
		Widgets: []tauchat.Widget{
			{Kind: tauchat.WidgetKindKeyValue, KeyValue: &tauchat.KeyValueWidget{
				Entries: []tauchat.KeyValueEntry{{Key: "k", Value: "v"}},
			}},
		},
	}
}

// TestHandleEvent_ExtensionViewRendered_MountsPanel verifies a pushed async
// view is added to c.panels and indexed by its host-qualified id.
func TestHandleEvent_ExtensionViewRendered_MountsPanel(t *testing.T) {
	c, _ := newTestChat(t)

	runHandleEvent(t, c, tauchat.ExtensionViewRenderedEvent{
		PluginName: "hello",
		ViewID:     "hello:panel-1",
		View:       demoView("panel-1"),
	})

	if _, ok := c.panelsByID["hello:panel-1"]; !ok {
		t.Fatal("expected panel-1 to be indexed after ExtensionViewRenderedEvent")
	}
	if len(c.panels.Children) != 1 {
		t.Fatalf("expected 1 mounted panel, got %d", len(c.panels.Children))
	}
}

// TestHandleEvent_ExtensionViewRendered_ReplacesExistingID verifies that
// re-rendering the same view id replaces the mounted component rather than
// accumulating duplicates.
func TestHandleEvent_ExtensionViewRendered_ReplacesExistingID(t *testing.T) {
	c, _ := newTestChat(t)

	runHandleEvent(t, c, tauchat.ExtensionViewRenderedEvent{
		PluginName: "hello", ViewID: "hello:panel-1", View: demoView("panel-1"),
	})
	runHandleEvent(t, c, tauchat.ExtensionViewRenderedEvent{
		PluginName: "hello", ViewID: "hello:panel-1", View: demoView("panel-1"),
	})

	if len(c.panels.Children) != 1 {
		t.Fatalf("expected re-render to replace, not duplicate: got %d children", len(c.panels.Children))
	}
}

// TestHandleEvent_ExtensionViewClosed_RemovesPanel verifies a close event
// removes the panel and its index entry.
func TestHandleEvent_ExtensionViewClosed_RemovesPanel(t *testing.T) {
	c, _ := newTestChat(t)

	runHandleEvent(t, c, tauchat.ExtensionViewRenderedEvent{
		PluginName: "hello", ViewID: "hello:panel-1", View: demoView("panel-1"),
	})
	runHandleEvent(t, c, tauchat.ExtensionViewClosedEvent{
		PluginName: "hello", ViewID: "hello:panel-1",
	})

	if _, ok := c.panelsByID["hello:panel-1"]; ok {
		t.Fatal("expected panel-1 to be removed after ExtensionViewClosedEvent")
	}
	if len(c.panels.Children) != 0 {
		t.Fatalf("expected 0 mounted panels after close, got %d", len(c.panels.Children))
	}
}

// TestHandleEvent_ExtensionViewClosed_UnknownIDIsNoOp guards against a panic
// when a close event arrives for an id that was never mounted (e.g. a
// duplicate close, or a race with an already-processed close).
func TestHandleEvent_ExtensionViewClosed_UnknownIDIsNoOp(t *testing.T) {
	c, _ := newTestChat(t)

	runHandleEvent(t, c, tauchat.ExtensionViewClosedEvent{
		PluginName: "hello", ViewID: "hello:never-opened",
	})
}

// TestHandleEvent_ExtensionCommandResult_WithView_NoDeadlock guards the sync
// view-rendering path (used when a slash command's response carries a
// structured View instead of plain text) against a deadlock/panic from a
// mismatched Lock/Unlock pair.
func TestHandleEvent_ExtensionCommandResult_WithView_NoDeadlock(t *testing.T) {
	c, _ := newTestChat(t)
	v := demoView("cmd-result")

	runHandleEvent(t, c, tauchat.ExtensionCommandResultEvent{
		Name: "hello", View: &v,
	})
}
