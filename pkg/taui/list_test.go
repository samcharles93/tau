package taui

import (
	"strings"
	"testing"
)

func TestListBulleted(t *testing.T) {
	l := NewList([]string{"first", "second"}, false)
	lines := l.Render(80)
	want := []string{"• first", "• second"}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d: %v", len(lines), len(want), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestListOrdered(t *testing.T) {
	l := NewList([]string{"first", "second"}, true)
	lines := l.Render(80)
	want := []string{"1. first", "2. second"}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestListWrapsWithHangingIndent(t *testing.T) {
	l := NewList([]string{"one two three four five"}, false)
	lines := l.Render(10)
	if len(lines) < 2 {
		t.Fatalf("expected wrapped item to produce multiple lines, got %v", lines)
	}
	if !strings.HasPrefix(lines[0], "• ") {
		t.Errorf("first line should start with the bullet marker: %q", lines[0])
	}
	for _, line := range lines[1:] {
		if strings.HasPrefix(line, "•") {
			t.Errorf("continuation line should not repeat the bullet: %q", line)
		}
		if !strings.HasPrefix(line, "  ") {
			t.Errorf("continuation line should be indented under the marker: %q", line)
		}
	}
}

func TestListEmpty(t *testing.T) {
	l := NewList(nil, false)
	if lines := l.Render(80); lines != nil {
		t.Errorf("expected nil lines for empty List, got %v", lines)
	}
}
