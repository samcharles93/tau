package tui

import (
	"testing"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/tui/notify"
	"github.com/samcharles93/tau/pkg/taui"
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

// TestHandleEvent_ToolOutput_AppendsToActiveTail verifies streamed tool
// output is routed into the running tool's bounded tail (not printed
// straight to permanent scrollback), and that the tail is removed from the
// box once the tool resolves so the resolved scrollback line doesn't carry
// the streamed output with it.
func TestHandleEvent_ToolOutput_AppendsToActiveTail(t *testing.T) {
	c, _ := newTestChat(t)
	c.stage = &taui.Container{}

	runHandleEvent(t, c, tauchat.ChatToolExecutionStartedEvent{CallID: "1", ToolName: "shell"})

	tb, ok := c.activeTools["1"]
	if !ok {
		t.Fatal("expected an active tool box for call 1")
	}
	if tb.tail == nil {
		t.Fatal("expected ToolExecutionStarted to create a tail log")
	}

	runHandleEvent(t, c, tauchat.ChatToolOutputEvent{CallID: "1", Chunk: "building...\n"})
	runHandleEvent(t, c, tauchat.ChatToolOutputEvent{CallID: "1", Chunk: "done\n"})

	if got := tb.tail.Render(0); len(got) != 2 || got[0] != "building..." || got[1] != "done" {
		t.Fatalf("tail.Render() = %q, want [%q %q]", got, "building...", "done")
	}

	runHandleEvent(t, c, tauchat.ChatToolExecutionCompletedEvent{CallID: "1", ToolName: "shell", ResultSummary: "ok"})

	if tb.tail != nil {
		t.Fatal("expected the tail reference to be cleared on the activeToolBox after completion")
	}
	for _, child := range tb.box.Children {
		if _, isRow := child.(*taui.ToolRow); !isRow {
			t.Fatalf("expected only the resolved ToolRow left in the box, found %T", child)
		}
	}
}

// TestHandleEvent_ToolOutput_UnknownCallIDIsNoop guards against a panic if
// output arrives for a call that has no active box (e.g. already resolved).
func TestHandleEvent_ToolOutput_UnknownCallIDIsNoop(t *testing.T) {
	c, _ := newTestChat(t)
	c.activeTools = map[string]*activeToolBox{}

	runHandleEvent(t, c, tauchat.ChatToolOutputEvent{CallID: "missing", Chunk: "stray output\n"})
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

// TestHandleEvent_ToolExecutionStarted_FlushesPrecedingText guards against a
// regression where a text segment preceding a tool call was left mounted in
// turnText instead of being flushed and reset, causing the next
// ChatResponseDeltaEvent to concatenate straight onto it with no separator
// (e.g. "...browsing `/`Now update the two callers...").
func TestHandleEvent_ToolExecutionStarted_FlushesPrecedingText(t *testing.T) {
	c, _ := newTestChat(t)
	c.stage = &taui.Container{}

	runHandleEvent(t, c, tauchat.ChatResponseDeltaEvent{Delta: "foo"})
	runHandleEvent(t, c, tauchat.ChatToolExecutionStartedEvent{CallID: "1", ToolName: "read"})

	if c.turnText != nil {
		t.Fatalf("expected turnText to be reset after a tool call starts, got %q", c.turnText.Text())
	}

	runHandleEvent(t, c, tauchat.ChatResponseDeltaEvent{Delta: "bar"})

	if got := c.turnText.Text(); got != "bar" {
		t.Fatalf("expected fresh segment %q, got %q (segments concatenated)", "bar", got)
	}
}

// TestHandleEvent_ToolExecutionStarted_BackToBackNoFlushStorm guards against
// a regression where consecutive tool-call starts with no intervening text
// delta (a parallel tool-call batch) would panic or emit blank scrollback
// lines from re-flushing an already-nil turnText/turnReasoning.
func TestHandleEvent_ToolExecutionStarted_BackToBackNoFlushStorm(t *testing.T) {
	c, _ := newTestChat(t)
	c.stage = &taui.Container{}

	runHandleEvent(t, c, tauchat.ChatToolExecutionStartedEvent{CallID: "1", ToolName: "read"})
	runHandleEvent(t, c, tauchat.ChatToolExecutionStartedEvent{CallID: "2", ToolName: "read"})

	if c.turnText != nil || c.turnReasoning != nil {
		t.Fatal("expected turnText/turnReasoning to remain nil across back-to-back tool starts")
	}
}

// TestHandleEvent_RuntimeError_ReachesStatusBar guards against a regression
// where ChatRuntimeErrorEvent only printed to scrollback and never surfaced
// in the status bar, unlike ChatNotificationEvent{Level: Error}.
func TestHandleEvent_RuntimeError_ReachesStatusBar(t *testing.T) {
	c, _ := newTestChat(t)

	runHandleEvent(t, c, tauchat.ChatRuntimeErrorEvent{Message: "boom"})

	n := c.notifyQueue.Current()
	if n == nil {
		t.Fatal("expected ChatRuntimeErrorEvent to push a status-bar notification")
	}
	if n.Message != "boom" {
		t.Fatalf("expected notification message %q, got %q", "boom", n.Message)
	}
	if n.Level != notify.LevelError {
		t.Fatalf("expected error level, got %v", n.Level)
	}
}
