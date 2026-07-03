package taui

import "testing"

type fakeOverlay struct {
	handled     bool
	pasteOK     bool
	pasteCalled bool
	inputCalled bool
	lastInput   string
	lastPaste   string
}

func (f *fakeOverlay) Render(int) []string { return nil }
func (f *fakeOverlay) Invalidate()          {}
func (f *fakeOverlay) HandleInput(data string) bool {
	f.inputCalled = true
	f.lastInput = data
	return f.handled
}

type fakeOverlayWithPaste struct {
	fakeOverlay
}

func (f *fakeOverlayWithPaste) HandlePaste(content string) bool {
	f.pasteCalled = true
	f.lastPaste = content
	return f.pasteOK
}

func TestOverlayStackPermanentSoftOverlayKeepsBaseFocused(t *testing.T) {
	base := NewLineInput("› ")
	s := NewOverlayStack(base)
	s.Push(&fakeOverlay{}, false) // e.g. a permanently-registered completions dropdown

	if !base.Focused() {
		t.Fatal("base should stay focused while only a soft overlay is pushed")
	}
}

func TestOverlayStackExclusiveOverlayUnfocusesBase(t *testing.T) {
	base := NewLineInput("› ")
	s := NewOverlayStack(base)
	s.Push(&fakeOverlay{}, false)

	exclusive := &fakeOverlay{}
	s.Push(exclusive, true)
	if base.Focused() {
		t.Fatal("base should be unfocused while an exclusive overlay is active")
	}

	s.Pop(exclusive)
	if !base.Focused() {
		t.Fatal("base should regain focus once the exclusive overlay is popped")
	}
}

func TestOverlayStackHandleInputPrecedence(t *testing.T) {
	s := NewOverlayStack(nil)
	soft := &fakeOverlay{handled: false}
	s.Push(soft, false)

	if s.HandleInput("x") {
		t.Fatal("soft overlay that returns false should not consume input")
	}
	if !soft.inputCalled {
		t.Fatal("soft overlay should still be offered the input")
	}

	exclusive := &fakeOverlay{handled: false}
	s.Push(exclusive, true)

	if !s.HandleInput("y") {
		t.Fatal("exclusive overlay must swallow input regardless of its own return value")
	}
}

func TestOverlayStackHandlePasteFallsThroughSoftOverlayWithoutPasteHandler(t *testing.T) {
	s := NewOverlayStack(nil)
	s.Push(&fakeOverlay{}, false) // no PasteHandler implemented

	if s.HandlePaste("hello") {
		t.Fatal("soft overlay without a PasteHandler should not consume paste")
	}
}

func TestOverlayStackHandlePasteExclusiveSwallowsWithoutPasteHandler(t *testing.T) {
	s := NewOverlayStack(nil)
	s.Push(&fakeOverlay{}, true) // exclusive, no PasteHandler implemented

	if !s.HandlePaste("hello") {
		t.Fatal("exclusive overlay without a PasteHandler should still swallow paste")
	}
}

func TestOverlayStackHandlePasteRoutesToImplementer(t *testing.T) {
	s := NewOverlayStack(nil)
	p := &fakeOverlayWithPaste{fakeOverlay: fakeOverlay{pasteOK: true}}
	s.Push(p, false)

	if !s.HandlePaste("hello") {
		t.Fatal("expected paste to be consumed by the overlay's PasteHandler")
	}
	if !p.pasteCalled || p.lastPaste != "hello" {
		t.Fatal("expected HandlePaste to be forwarded with the given content")
	}
}
