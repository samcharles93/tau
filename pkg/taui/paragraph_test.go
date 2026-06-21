package taui

import (
	"reflect"
	"strings"
	"testing"
)

func TestParagraphWraps(t *testing.T) {
	p := NewParagraph("the quick brown fox jumps")
	lines := p.Render(11) // width 11 columns
	for _, ln := range lines {
		if VisibleWidth(ln) > 11 {
			t.Errorf("wrapped line exceeds width: %q (%d)", ln, VisibleWidth(ln))
		}
	}
	// Reassembling the words should reproduce the input.
	if got := strings.Join(strings.Fields(strings.Join(lines, " ")), " "); got != "the quick brown fox jumps" {
		t.Errorf("wrapped content lost words: %q", got)
	}
}

func TestParagraphHonorsNewlines(t *testing.T) {
	p := NewParagraph("line one\nline two")
	lines := p.Render(80)
	if want := []string{"line one", "line two"}; !reflect.DeepEqual(lines, want) {
		t.Errorf("lines = %q, want %q", lines, want)
	}
}

func TestParagraphAppendStreaming(t *testing.T) {
	p := NewParagraph("")
	for _, tok := range []string{"Hello", ", ", "world", "!"} {
		p.Append(tok)
	}
	if got := p.Text(); got != "Hello, world!" {
		t.Errorf("Text = %q, want %q", got, "Hello, world!")
	}
	lines := p.Render(80)
	if len(lines) != 1 || lines[0] != "Hello, world!" {
		t.Errorf("render = %q, want one line %q", lines, "Hello, world!")
	}
}

func TestParagraphEmptyRendersNothing(t *testing.T) {
	p := NewParagraph("")
	if lines := p.Render(80); lines != nil {
		t.Errorf("empty paragraph rendered %q, want nil", lines)
	}
}
