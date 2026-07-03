package taui

import (
	"strings"
	"testing"
)

func TestTableRendersHeaderSeparatorAndRows(t *testing.T) {
	tbl := NewTable([]string{"name", "status"}, [][]string{
		{"go", "ok"},
		{"lint", "failed"},
	})
	lines := tbl.Render(80)
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4 (header, rule, 2 rows): %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "name") || !strings.Contains(lines[0], "status") {
		t.Errorf("header line missing headers: %q", lines[0])
	}
	if !strings.Contains(lines[1], "─") {
		t.Errorf("second line should be the separator rule: %q", lines[1])
	}
	if !strings.Contains(lines[2], "go") || !strings.Contains(lines[3], "lint") {
		t.Errorf("body rows missing expected content: %v", lines[2:])
	}
}

func TestTableColumnsAlignToWidestCell(t *testing.T) {
	tbl := NewTable([]string{"k"}, [][]string{{"a-very-long-value"}})
	lines := tbl.Render(80)
	headerWidth := VisibleWidth(lines[0])
	rowWidth := VisibleWidth(lines[2])
	if headerWidth != rowWidth {
		t.Errorf("header width %d != row width %d: header=%q row=%q", headerWidth, rowWidth, lines[0], lines[2])
	}
}

func TestTableShrinksToFitWidth(t *testing.T) {
	tbl := NewTable(
		[]string{"a-very-long-header-name", "another-very-long-header"},
		[][]string{{"value one here", "value two here too"}},
	)
	lines := tbl.Render(20)
	for _, line := range lines {
		if w := VisibleWidth(line); w > 20 {
			t.Errorf("line exceeds render width 20 (got %d): %q", w, line)
		}
	}
}

func TestTableEmpty(t *testing.T) {
	tbl := NewTable(nil, nil)
	if lines := tbl.Render(80); lines != nil {
		t.Errorf("expected nil lines for empty Table, got %v", lines)
	}
}
