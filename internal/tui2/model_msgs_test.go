package tui2

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/eventbus"
)

func TestChatEventLoopRearmsAfterEachEvent(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()

	pub := eventbus.Publish[tauchat.ChatEvent](bus.Client("pub"))
	sub := eventbus.Subscribe[tauchat.ChatEvent](bus.Client("sub"))
	defer sub.Close()

	rt := &fakeRuntime{}
	m := newTestModel(rt, sub)

	pub.Publish(tauchat.ChatNotificationEvent{Message: "one"})
	pub.Publish(tauchat.ChatNotificationEvent{Message: "two"})
	pub.Publish(tauchat.ChatNotificationEvent{Message: "three"})

	var delivered []string
	cmd := m.Init()
	for i := 0; i < 3 && len(delivered) < 3; i++ {
		var next tea.Cmd
		for _, msg := range drainCmd(cmd) {
			if ce, ok := msg.(chatEventMsg); ok {
				if n, ok := ce.event.(tauchat.ChatNotificationEvent); ok {
					delivered = append(delivered, n.Message)
				}
			}
			if _, c := m.Update(msg); c != nil {
				next = c
			}
		}
		cmd = next
	}

	if want := []string{"one", "two", "three"}; !reflect.DeepEqual(delivered, want) {
		t.Fatalf("re-arm pattern dropped or reordered events: got %v, want %v", delivered, want)
	}
}

func TestUpdateChatEventsClosed(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	_, cmd := m.Update(chatEventsClosedMsg{})
	if cmd == nil {
		t.Fatal("expected a Cmd (tea.Quit) from chatEventsClosedMsg")
	}
}

func TestUpdateStartupMsg(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	_, cmd := m.Update(startupMsg{sessionID: "sess-new", modelName: "gpt-5", provider: "openai"})
	if cmd != nil {
		t.Fatal("expected nil Cmd from startupMsg")
	}
	if m.sessionID != "sess-new" {
		t.Fatalf("sessionID = %q, want %q", m.sessionID, "sess-new")
	}
	if m.modelName != "gpt-5" {
		t.Fatalf("modelName = %q, want %q", m.modelName, "gpt-5")
	}
}

func TestSendCommandErrorPropagates(t *testing.T) {
	rt := &fakeRuntime{err: errIntentional}

	cmd := sendCommand(rt, tauchat.SubmitChatPromptCommand{})
	msg := cmd()

	rm, ok := msg.(sendResultMsg)
	if !ok {
		t.Fatalf("expected sendResultMsg, got %T", msg)
	}
	if rm.err != errIntentional {
		t.Fatalf("err = %v, want %v", rm.err, errIntentional)
	}
}

func TestReadNextEventChannelClosed(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()

	sub := eventbus.Subscribe[tauchat.ChatEvent](bus.Client("test"))
	sub.Close() // close before reading — should get chatEventsClosedMsg

	cmd := readNextEvent(sub)
	msg := cmd()

	if _, ok := msg.(chatEventsClosedMsg); !ok {
		t.Fatalf("expected chatEventsClosedMsg, got %T", msg)
	}
}
