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

	// Tool-log text lives in nested elements (a bordered container with a label
	// and a wrapped body), so search the whole subtree, not just direct children.
	if !elementContainsText(viewport, "executing ls...") {
		t.Error("tool logs not found in viewport")
	}
}

// elementContainsText reports whether el or any descendant has text containing
// substr.
func elementContainsText(el *gt.Element, substr string) bool {
	if el == nil {
		return false
	}
	if strings.Contains(el.Text(), substr) {
		return true
	}
	for _, child := range el.Children() {
		if elementContainsText(child, substr) {
			return true
		}
	}
	return false
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
