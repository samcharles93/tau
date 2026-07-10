package tui2

import (
	"reflect"
	"strings"
	"testing"
)

func TestWrapWords(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxWidth int
		want     []string
	}{
		{
			name:     "short text fits in one line",
			text:     "hello world",
			maxWidth: 80,
			want:     []string{"hello world"},
		},
		{
			name:     "words wrap at width",
			text:     "hello world this is a test",
			maxWidth: 12,
			want:     []string{"hello world", "this is a", "test"},
		},
		{
			name:     "explicit newlines preserved",
			text:     "hello\nworld",
			maxWidth: 80,
			want:     []string{"hello", "world"},
		},
		{
			name:     "empty text yields one blank line",
			text:     "",
			maxWidth: 80,
			want:     []string{""},
		},
		{
			name:     "consecutive spaces preserved",
			text:     "hello  world",
			maxWidth: 20,
			want:     []string{"hello  world"},
		},
		{
			name:     "word longer than width kept intact",
			text:     "a supercalifragilisticexpialidocious word",
			maxWidth: 10,
			want:     []string{"a", "supercalifragilisticexpialidocious", "word"},
		},
		{
			name:     "blank line between paragraphs",
			text:     "hello\n\nworld",
			maxWidth: 80,
			want:     []string{"hello", "", "world"},
		},
		{
			name:     "zero maxWidth defaults to 80",
			text:     "short",
			maxWidth: 0,
			want:     []string{"short"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapWords(tt.text, tt.maxWidth)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("wrapWords(%q, %d) = %q, want %q",
					tt.text, tt.maxWidth, got, tt.want)
			}
		})
	}
}

func TestWrapWordsWrappedLinesFitWidth(t *testing.T) {
	// Property test: no wrapped line exceeds maxWidth (for lines that
	// don't contain a word longer than maxWidth).
	const width = 40
	longPara := strings.Repeat("the quick brown fox ", 10)
	lines := wrapWords(longPara, width)
	for i, line := range lines {
		w := visibleWidth(line)
		if w > width {
			t.Errorf("line %d width %d > maxWidth %d: %q", i, w, width, line)
		}
	}
}

func TestSplitPreserving(t *testing.T) {
	words, spaces := splitPreserving("hello  world")
	if len(words) != 2 || words[0] != "hello" || words[1] != "world" {
		t.Fatalf("words = %q, want [hello world]", words)
	}
	if len(spaces) != 2 || spaces[0] != 0 || spaces[1] != 2 {
		t.Fatalf("spaces = %v, want [0 2]", spaces)
	}
}
