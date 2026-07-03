package taui

import "testing"

func TestStackVerticalDelegatesToContainer(t *testing.T) {
	s := NewStack(StackVertical, 0)
	s.AddChild(NewText("one"))
	s.AddChild(NewText("two"))
	lines := s.Render(80)
	want := []string{"one", "two"}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d: %v", len(lines), len(want), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestStackHorizontalZipsColumns(t *testing.T) {
	s := NewStack(StackHorizontal, 1)
	s.AddChild(NewText("left"))
	s.AddChild(NewText("right"))
	lines := s.Render(40)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1: %v", len(lines), lines)
	}
	if lines[0] != "left right" {
		t.Errorf("got %q, want %q", lines[0], "left right")
	}
}

func TestStackHorizontalPadsMismatchedHeights(t *testing.T) {
	tall := &Container{}
	tall.AddChild(NewText("a"))
	tall.AddChild(NewText("b"))
	short := &Container{}
	short.AddChild(NewText("x"))

	s := NewStack(StackHorizontal, 1)
	s.AddChild(tall)
	s.AddChild(short)
	lines := s.Render(40)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (padded to tallest column): %v", len(lines), lines)
	}
}

func TestStackEmpty(t *testing.T) {
	s := NewStack(StackHorizontal, 0)
	if lines := s.Render(80); lines != nil {
		t.Errorf("expected nil lines for empty Stack, got %v", lines)
	}
}
