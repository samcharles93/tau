package tui2

import (
	"reflect"
	"testing"
)

func TestInterposeBlankLines(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		count int
		want  []string
	}{
		{
			name:  "zero count returns original",
			lines: []string{"a", "b", "c"},
			count: 0,
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "negative count returns original",
			lines: []string{"a", "b"},
			count: -1,
			want:  []string{"a", "b"},
		},
		{
			name:  "single line returns original",
			lines: []string{"only"},
			count: 2,
			want:  []string{"only"},
		},
		{
			name:  "nil slice returns nil",
			lines: nil,
			count: 1,
			want:  nil,
		},
		{
			name:  "one blank between two lines",
			lines: []string{"a", "b"},
			count: 1,
			want:  []string{"a", "", "b"},
		},
		{
			name:  "two blanks between three lines",
			lines: []string{"a", "b", "c"},
			count: 2,
			want:  []string{"a", "", "", "b", "", "", "c"},
		},
		{
			name:  "three blanks between two lines",
			lines: []string{"x", "y"},
			count: 3,
			want:  []string{"x", "", "", "", "y"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := interposeBlankLines(tt.lines, tt.count)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("interposeBlankLines(%v, %d) = %v, want %v",
					tt.lines, tt.count, got, tt.want)
			}
		})
	}
}

func TestLayoutConstants(t *testing.T) {
	// Prove the constants carry the values they were designed for.
	if contentPadLeft != 6 {
		t.Errorf("contentPadLeft = %d, want 6", contentPadLeft)
	}
	if contentPadRight != 0 {
		t.Errorf("contentPadRight = %d, want 0", contentPadRight)
	}
	if spacingReasoningToContent != 0 {
		t.Errorf("spacingReasoningToContent = %d, want 0", spacingReasoningToContent)
	}
	if spacingToolToContent != 0 {
		t.Errorf("spacingToolToContent = %d, want 0", spacingToolToContent)
	}
	if spacingBetweenMessages != 0 {
		t.Errorf("spacingBetweenMessages = %d, want 0", spacingBetweenMessages)
	}
	if toolBoxPadTopBottom != 0 {
		t.Errorf("toolBoxPadTopBottom = %d, want 0", toolBoxPadTopBottom)
	}
	if toolBoxPadLeftRight != 1 {
		t.Errorf("toolBoxPadLeftRight = %d, want 1", toolBoxPadLeftRight)
	}
	if separatorMarginTop != 0 {
		t.Errorf("separatorMarginTop = %d, want 0", separatorMarginTop)
	}
	if separatorMarginBottom != 0 {
		t.Errorf("separatorMarginBottom = %d, want 0", separatorMarginBottom)
	}
}
