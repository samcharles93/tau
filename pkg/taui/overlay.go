package taui

import "sync"

// Overlay is a component that can be pushed onto an OverlayStack to take
// priority over a base component while it is active — a Prompt-style dialog,
// a Completions-style dropdown, or any future modal/picker.
type Overlay interface {
	Component
	InputHandler
}

type overlayEntry struct {
	overlay   Overlay
	exclusive bool
}

// OverlayStack routes input to an ordered set of active overlays. It
// generalizes the "if activePrompt != nil {...} else if
// completions.HandleInput(...) {...}" chain that otherwise grows one
// hand-written branch per modal-like widget: adding a new overlay becomes a
// Push/Pop call instead of a new branch in the input-dispatch method, and
// HandleInput/HandlePaste forwarding is automatic for every overlay rather
// than something each caller has to remember to wire up individually.
//
// Two overlay flavors are supported, matching what already exists in
// practice:
//
//   - Exclusive overlays (pushed with exclusive=true — e.g. a confirm/
//     question dialog) swallow all input unconditionally once active,
//     regardless of their own HandleInput's return value. This matches the
//     "treat input as consumed while a Prompt is active regardless of the
//     return value" contract Prompt already documents.
//   - Soft overlays (pushed with exclusive=false — e.g. a completions
//     dropdown) self-report activity: their HandleInput return value decides
//     whether input falls through to the next overlay down, or ultimately to
//     the caller's own fallback (e.g. a base text input).
//
// A stack with no overlays pushed behaves as a no-op: HandleInput and
// HandlePaste both return false immediately.
type OverlayStack struct {
	mu    sync.Mutex
	stack []overlayEntry
	base  Focusable // optional; nil if the caller doesn't want focus syncing
}

// NewOverlayStack creates an empty stack. base, if non-nil, has SetFocused
// kept in sync with whether an exclusive overlay is active (focused unless
// an exclusive overlay, e.g. a Prompt, is pushed — soft overlays like a
// completions dropdown don't take focus away from base) — this is what makes
// the Focusable/SetFocused mechanism meaningful for a component (like
// LineInput) that never otherwise passes through TUI.SetFocus.
func NewOverlayStack(base Focusable) *OverlayStack {
	s := &OverlayStack{base: base}
	if base != nil {
		base.SetFocused(true)
	}
	return s
}

// Push adds an overlay on top of the stack (topmost = most recently pushed =
// tried first).
func (s *OverlayStack) Push(o Overlay, exclusive bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stack = append(s.stack, overlayEntry{overlay: o, exclusive: exclusive})
	s.syncBaseFocusLocked()
}

// Pop removes o from the stack, wherever it sits — overlays don't always
// resolve in strict LIFO order (e.g. a queued prompt can replace a resolved
// one without anything else ever being pushed above it).
func (s *OverlayStack) Pop(o Overlay) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.stack {
		if e.overlay == o {
			s.stack = append(s.stack[:i], s.stack[i+1:]...)
			break
		}
	}
	s.syncBaseFocusLocked()
}

// HasExclusive reports whether an exclusive overlay is currently active —
// useful for callers that need to queue further exclusive overlays (e.g.
// interactive prompts) rather than presenting them concurrently.
func (s *OverlayStack) HasExclusive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hasExclusiveLocked()
}

func (s *OverlayStack) hasExclusiveLocked() bool {
	for _, e := range s.stack {
		if e.exclusive {
			return true
		}
	}
	return false
}

func (s *OverlayStack) syncBaseFocusLocked() {
	if s.base != nil {
		s.base.SetFocused(!s.hasExclusiveLocked())
	}
}

// HandleInput walks the stack from topmost down. Exclusive overlays swallow
// unconditionally; soft overlays are tried in order and only consume when
// their own HandleInput returns true. Returns false if nothing on the stack
// consumed the input — including when the stack is empty — leaving the
// caller to apply its own fallback.
func (s *OverlayStack) HandleInput(data string) bool {
	entries := s.snapshot()
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		consumed := e.overlay.HandleInput(data)
		if e.exclusive || consumed {
			return true
		}
	}
	return false
}

// HandlePaste walks the stack the same way as HandleInput. An exclusive
// overlay that doesn't implement PasteHandler still swallows the paste (it
// has exclusive focus, so nothing else should receive it); a soft overlay
// that doesn't implement PasteHandler is skipped, letting the paste fall
// through to the next overlay or the caller's own fallback.
func (s *OverlayStack) HandlePaste(content string) bool {
	entries := s.snapshot()
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		h, ok := e.overlay.(PasteHandler)
		if !ok {
			if e.exclusive {
				return true
			}
			continue
		}
		consumed := h.HandlePaste(content)
		if e.exclusive || consumed {
			return true
		}
	}
	return false
}

// snapshot copies the stack under lock so HandleInput/HandlePaste can call
// into overlays (which may take their own locks or invoke callbacks) without
// holding s.mu.
func (s *OverlayStack) snapshot() []overlayEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]overlayEntry(nil), s.stack...)
}
