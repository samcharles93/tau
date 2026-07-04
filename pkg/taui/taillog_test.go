package taui

import "testing"

func TestTailLog_AppendSplitsLinesAcrossChunks(t *testing.T) {
	tl := NewTailLog(10, nil)

	tl.Append("hello ")
	tl.Append("world\nsecond line\nthird")

	got := tl.Render(0)
	want := []string{"hello world", "second line", "third"}
	if len(got) != len(want) {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Render()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTailLog_TrimsCarriageReturn(t *testing.T) {
	tl := NewTailLog(10, nil)
	tl.Append("line one\r\nline two\r\n")

	got := tl.Render(0)
	want := []string{"line one", "line two"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestTailLog_BoundedToMaxLines(t *testing.T) {
	tl := NewTailLog(3, nil)

	for i := range 6 {
		tl.Append(string(rune('a'+i)) + "\n")
	}

	got := tl.Render(0)
	want := []string{"d", "e", "f"}
	if len(got) != len(want) {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Render()[%d] = %q, want %q (oldest lines should have been evicted)", i, got[i], want[i])
		}
	}
}

func TestTailLog_IncludesInFlightPartialLine(t *testing.T) {
	tl := NewTailLog(10, nil)
	tl.Append("done line\n")
	tl.Append("still streaming, no newline yet")

	got := tl.Render(0)
	want := []string{"done line", "still streaming, no newline yet"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestTailLog_ReturnsEmptyBeforeAnyOutput(t *testing.T) {
	tl := NewTailLog(5, nil)
	if got := tl.Render(80); len(got) != 0 {
		t.Fatalf("Render() on empty tail = %q, want empty", got)
	}
}

func TestTailLog_Reset(t *testing.T) {
	tl := NewTailLog(5, nil)
	tl.Append("line one\npartial")
	tl.Reset()

	if got := tl.Render(0); len(got) != 0 {
		t.Fatalf("Render() after Reset() = %q, want empty", got)
	}
}

func TestTailLog_RenderTruncatesToWidth(t *testing.T) {
	tl := NewTailLog(5, nil)
	tl.Append("a very long line that exceeds the width\n")

	got := tl.Render(10)
	if len(got) != 1 {
		t.Fatalf("Render() = %q, want 1 line", got)
	}
	if vw := VisibleWidth(got[0]); vw != 10 {
		t.Fatalf("Render()[0] visible width = %d, want 10 (got %q)", vw, got[0])
	}
}
