package tui2

import (
	tea "charm.land/bubbletea/v2"
)

// overlayID names a Category 2 (modal) slot, per
// docs/specs/state-taxonomy.md. Order matters: overlayPrecedence declares
// dispatch priority once, replacing what used to be a hand-written ladder in
// dispatchKey plus scattered manual "close my siblings" calls at each open
// site (openDiffViewer, openSessionTree, presentPrompt, ...).
//
// Completions is not registered here: it's the one soft overlay and has its
// own explicit call site in dispatchKey between "no exclusive overlay is
// open" and "normal keybindings," which a single combined loop can't
// express. It's listed in the const block only as documentation of the full
// precedence order; no live code reads overlayCompletions at runtime.
type overlayID int

const (
	overlayPrompt overlayID = iota
	overlayHelp
	overlayDiff
	overlayChildTranscript
	overlaySessionTree
	overlayContextMenu
	overlayCompletions // soft overlay — documented here, dispatched outside this registry
)

// overlay is the uniform surface every Category 2 modal implements, so
// dispatchKey and closeOtherExclusiveOverlays can act on any of them without
// a type switch. Rendering is deliberately NOT part of this interface:
// activePrompt renders inline as flow content (via computeLayout/
// renderPrompt), while the other five render as floating compositor layers
// composed directly in View() - two different rendering models with no
// shared benefit from being forced through one method, and View()'s existing
// compositing order also encodes paint (Z-order) concerns that are separate
// from dispatch precedence. Only dispatch precedence and mutual exclusion -
// the two things that were actually scattered and error-prone - are unified
// here.
type overlay interface {
	// active reports whether this slot currently has state open.
	active() bool
	// handleKey processes msg for this slot. consumed is always true for an
	// exclusive slot (each swallows every key while open, per its own
	// handleXKey doc comment); the soft completions slot returns false to
	// fall through to normal keybindings when it has nothing to do with msg.
	handleKey(m *model, msg tea.KeyPressMsg) (cmd tea.Cmd, consumed bool)
	// close clears this slot's state. A no-op if already inactive.
	close(m *model)
}

type overlaySlot struct {
	id        overlayID
	ov        overlay
	exclusive bool
}

// overlayPrecedence is the single declared dispatch order - first active
// exclusive slot wins. Rebuilt on every call: each adapter is a thin struct
// wrapping *model, so this always reflects live state and costs nothing
// beyond the slice allocation. Completions (soft) is deliberately NOT in
// this list - see the overlayID doc comment above.
func (m *model) overlayPrecedence() []overlaySlot {
	return []overlaySlot{
		{overlayPrompt, promptOverlay{m}, true},
		{overlayHelp, helpOverlay{m}, true},
		{overlayDiff, diffOverlay{m}, true},
		{overlayChildTranscript, childTranscriptOverlay{m}, true},
		{overlaySessionTree, sessionTreeOverlay{m}, true},
		{overlayContextMenu, contextMenuOverlay{m}, true},
	}
}

// dispatchExclusiveOverlayKey tries every exclusive slot in precedence order
// and returns (cmd, true) the first time one is active - an active exclusive
// slot always claims the key entirely. Returns (nil, false) when none is
// active, so the caller falls through to the inputSel-clear step and then
// the soft completions slot (see dispatchKey) before normal keybindings.
func (m *model) dispatchExclusiveOverlayKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	for _, slot := range m.overlayPrecedence() {
		if !slot.exclusive || !slot.ov.active() {
			continue
		}
		cmd, _ := slot.ov.handleKey(m, msg)
		return cmd, true
	}
	return nil, false
}

// closeOtherExclusiveOverlays closes every exclusive overlay except keep.
// Every "open" function calls this instead of remembering which specific
// siblings to nil out by hand - previously each open site only remembered to
// clear whichever sibling had bitten it before (usually just contextMenu),
// which is exactly how this stayed inconsistent across sites.
func (m *model) closeOtherExclusiveOverlays(keep overlayID) {
	for _, slot := range m.overlayPrecedence() {
		if slot.exclusive && slot.id != keep && slot.ov.active() {
			slot.ov.close(m)
		}
	}
}

// --- adapters ----------------------------------------------------------
// Each wraps *model and forwards to the existing handleXKey/render code
// unchanged - these are registry glue, not a data migration.

type promptOverlay struct{ m *model }

func (o promptOverlay) active() bool { return o.m.activePrompt != nil }

func (o promptOverlay) handleKey(m *model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	return m.handlePromptKey(msg), true
}

func (o promptOverlay) close(m *model) { m.activePrompt = nil }

type helpOverlay struct{ m *model }

func (o helpOverlay) active() bool { return o.m.helpOverlay != nil }

func (o helpOverlay) handleKey(m *model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	return m.handleHelpOverlayKey(msg), true
}

func (o helpOverlay) close(m *model) { m.helpOverlay = nil }

type diffOverlay struct{ m *model }

func (o diffOverlay) active() bool { return o.m.diffViewer != nil }

func (o diffOverlay) handleKey(m *model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	return m.handleDiffViewerKey(msg), true
}

func (o diffOverlay) close(m *model) { m.diffViewer = nil }

type childTranscriptOverlay struct{ m *model }

func (o childTranscriptOverlay) active() bool { return o.m.childTranscriptViewer != nil }

func (o childTranscriptOverlay) handleKey(m *model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	return m.handleChildTranscriptViewerKey(msg), true
}

func (o childTranscriptOverlay) close(m *model) { m.childTranscriptViewer = nil }

type sessionTreeOverlay struct{ m *model }

func (o sessionTreeOverlay) active() bool { return o.m.sessionTreeOverlay != nil }

func (o sessionTreeOverlay) handleKey(m *model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	return m.handleSessionTreeKey(msg), true
}

func (o sessionTreeOverlay) close(m *model) { m.sessionTreeOverlay = nil }

type contextMenuOverlay struct{ m *model }

func (o contextMenuOverlay) active() bool { return o.m.contextMenu != nil }

func (o contextMenuOverlay) handleKey(m *model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	return m.handleContextMenuKey(msg), true
}

func (o contextMenuOverlay) close(m *model) { m.contextMenu = nil }
