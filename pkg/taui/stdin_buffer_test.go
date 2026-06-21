package taui

import (
	"reflect"
	"testing"
)

// collect drives a stdinBuffer over the given chunks and returns the keys and
// pastes it emitted, in order.
func collect(chunks ...string) (keys []string, pastes []string) {
	b := newStdinBuffer(
		func(seq string) { keys = append(keys, seq) },
		func(content string) { pastes = append(pastes, content) },
	)
	for _, c := range chunks {
		b.process(c)
	}
	return keys, pastes
}

func TestSplitsBatchedKeys(t *testing.T) {
	// The bug: "1q" arriving in one read matched neither "1" nor "q".
	keys, pastes := collect("1q")
	if want := []string{"1", "q"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %q, want %q", keys, want)
	}
	if len(pastes) != 0 {
		t.Errorf("unexpected pastes: %q", pastes)
	}
}

func TestSplitsPlainRun(t *testing.T) {
	keys, _ := collect("hello")
	if want := []string{"h", "e", "l", "l", "o"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %q, want %q", keys, want)
	}
}

func TestArrowKeyStaysIntact(t *testing.T) {
	keys, _ := collect("\x1b[A")
	if want := []string{"\x1b[A"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %q, want %q", keys, want)
	}
}

func TestBatchedArrowsSplit(t *testing.T) {
	keys, _ := collect("\x1b[A\x1b[B")
	if want := []string{"\x1b[A", "\x1b[B"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %q, want %q", keys, want)
	}
}

func TestLoneEscapeIsFlushed(t *testing.T) {
	// A standalone Escape press (used as "quit" in the demos) must be delivered,
	// not held waiting for the rest of a sequence.
	keys, _ := collect("\x1b")
	if want := []string{"\x1b"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %q, want %q", keys, want)
	}
}

func TestPasteEmittedSeparately(t *testing.T) {
	keys, pastes := collect("\x1b[200~hello world\x1b[201~")
	if len(keys) != 0 {
		t.Errorf("paste markers leaked as keys: %q", keys)
	}
	if want := []string{"hello world"}; !reflect.DeepEqual(pastes, want) {
		t.Errorf("pastes = %q, want %q", pastes, want)
	}
}

func TestPasteSpansChunks(t *testing.T) {
	keys, pastes := collect("\x1b[200~hel", "lo\x1b[201~")
	if len(keys) != 0 {
		t.Errorf("paste markers leaked as keys: %q", keys)
	}
	if want := []string{"hello"}; !reflect.DeepEqual(pastes, want) {
		t.Errorf("pastes = %q, want %q", pastes, want)
	}
}

func TestKeysAroundPaste(t *testing.T) {
	keys, pastes := collect("a\x1b[200~X\x1b[201~q")
	if want := []string{"a", "q"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %q, want %q", keys, want)
	}
	if want := []string{"X"}; !reflect.DeepEqual(pastes, want) {
		t.Errorf("pastes = %q, want %q", pastes, want)
	}
}

func TestPasteContainingControlBytes(t *testing.T) {
	// Newlines and other control bytes inside a paste must not be re-segmented
	// into keys — they belong to the paste payload.
	keys, pastes := collect("\x1b[200~line1\nline2\tx\x1b[201~")
	if len(keys) != 0 {
		t.Errorf("paste content leaked as keys: %q", keys)
	}
	if want := []string{"line1\nline2\tx"}; !reflect.DeepEqual(pastes, want) {
		t.Errorf("pastes = %q, want %q", pastes, want)
	}
}

func TestUTF8RuneNotSplit(t *testing.T) {
	keys, _ := collect("café")
	if want := []string{"c", "a", "f", "é"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %q, want %q", keys, want)
	}
}

func TestSequenceStatus(t *testing.T) {
	cases := []struct {
		in   string
		want seqStatus
	}{
		{"a", statusNotEscape},
		{"\x1b", statusIncomplete},
		{"\x1b[", statusIncomplete},
		{"\x1b[A", statusComplete},
		{"\x1bOP", statusComplete},        // SS3 (F1)
		{"\x1b[<0;1;1M", statusComplete},  // SGR mouse press
		{"\x1b[<0;1;1", statusIncomplete}, // SGR mouse, missing final byte
		{"\x1bb", statusComplete},         // meta (Alt+b)
	}
	for _, c := range cases {
		if got := sequenceStatus(c.in); got != c.want {
			t.Errorf("sequenceStatus(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
