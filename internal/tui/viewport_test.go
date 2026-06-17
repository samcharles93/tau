package tui

import (
	"context"
	"strings"
	"testing"

	gt "github.com/grindlemire/go-tui"
	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
)

func TestRenderMessagesViewportIncludesToolLogs(t *testing.T) {
	runtime := &fakeRuntime{}
	bus := eventbus.New()
	client := bus.Client("test")
	chatSub := eventbus.Subscribe[tauchat.ChatEvent](client)
	panel := NewChatPanel(context.Background(), runtime, chatSub, TUIConfig{
		SessionID: "session_1",
		ModelName: "model-a",
	})

	panel.toolLogs.Set(map[string]string{
		"call_1": "executing ls...\nfile1.txt\nfile2.txt",
	})

	viewport := panel.renderMessagesViewport(nil)

	found := false
	// We expect tool logs to be rendered in the viewport.
	// Current implementation doesn't do this.
	for _, child := range viewport.Children() {
		if strings.Contains(child.Text(), "executing ls...") {
			found = true
			break
		}
	}

	if !found {
		t.Error("tool logs not found in viewport")
	}
}

func TestChatPanel_OnKey_Scrolling(t *testing.T) {
	runtime := &fakeRuntime{}
	panel := newTestPanel(runtime)

	// Initialize messageViewport
	panel.renderMessagesViewport(nil)
	if panel.messageViewport == nil {
		t.Fatal("messageViewport should be initialized")
	}

	// Test PageDown handler
	km := panel.KeyMap()
	found := false
	for _, kb := range km {
		if kb.Pattern.Key == gt.KeyPageDown {
			found = true
			// We can't easily call the handler without a real App if it uses App methods,
			// but we can verify it exists and is correctly bound.
			if kb.Handler == nil {
				t.Error("PageDown handler is nil")
			}
			break
		}
	}

	if !found {
		t.Error("PageDown handler not found in KeyMap")
	}

	// Also check for PageUp, Up, Down
	keys := []gt.Key{gt.KeyPageUp, gt.KeyUp, gt.KeyDown}
	for _, key := range keys {
		found = false
		for _, kb := range km {
			if kb.Pattern.Key == key {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%v handler not found in KeyMap", key)
		}
	}
}
