package tui2

import "testing"

func TestTextFieldInsertAndValue(t *testing.T) {
	f := newTextField("")
	f.Insert("hello")
	if got := f.Value(); got != "hello" {
		t.Fatalf("Value() = %q, want %q", got, "hello")
	}
	if f.cursor != 5 {
		t.Fatalf("cursor = %d, want 5", f.cursor)
	}
}

func TestTextFieldInsertAtCursorMidString(t *testing.T) {
	f := newTextField("")
	f.SetValue("helloworld")
	f.MoveCursor(-5) // cursor now between "hello" and "world"
	f.Insert(" ")
	if got := f.Value(); got != "hello world" {
		t.Fatalf("Value() = %q, want %q", got, "hello world")
	}
}

func TestTextFieldBackspace(t *testing.T) {
	f := newTextField("")
	f.SetValue("abc")
	f.Backspace()
	if got := f.Value(); got != "ab" {
		t.Fatalf("Value() = %q, want %q", got, "ab")
	}
	if f.cursor != 2 {
		t.Fatalf("cursor = %d, want 2", f.cursor)
	}
}

func TestTextFieldBackspaceAtStartIsNoop(t *testing.T) {
	f := newTextField("")
	f.SetValue("abc")
	f.Home()
	f.Backspace()
	if got := f.Value(); got != "abc" {
		t.Fatalf("Value() = %q, want unchanged %q", got, "abc")
	}
}

func TestTextFieldDelete(t *testing.T) {
	f := newTextField("")
	f.SetValue("abc")
	f.Home()
	f.Delete()
	if got := f.Value(); got != "bc" {
		t.Fatalf("Value() = %q, want %q", got, "bc")
	}
}

func TestTextFieldDeleteAtEndIsNoop(t *testing.T) {
	f := newTextField("")
	f.SetValue("abc")
	f.Delete()
	if got := f.Value(); got != "abc" {
		t.Fatalf("Value() = %q, want unchanged %q", got, "abc")
	}
}

func TestTextFieldMoveCursorClamps(t *testing.T) {
	f := newTextField("")
	f.SetValue("abc")
	f.MoveCursor(-100)
	if f.cursor != 0 {
		t.Fatalf("cursor = %d, want clamped to 0", f.cursor)
	}
	f.MoveCursor(100)
	if f.cursor != 3 {
		t.Fatalf("cursor = %d, want clamped to 3", f.cursor)
	}
}

func TestTextFieldHomeEnd(t *testing.T) {
	f := newTextField("")
	f.SetValue("abc")
	f.Home()
	if f.cursor != 0 {
		t.Fatalf("cursor after Home = %d, want 0", f.cursor)
	}
	f.End()
	if f.cursor != 3 {
		t.Fatalf("cursor after End = %d, want 3", f.cursor)
	}
}

func TestTextFieldSetValueMovesCursorToEnd(t *testing.T) {
	f := newTextField("")
	f.SetValue("hello")
	if f.cursor != 5 {
		t.Fatalf("cursor = %d, want 5 (end of value)", f.cursor)
	}
}

func TestTextFieldRenderShowsPlaceholderWhenEmpty(t *testing.T) {
	f := newTextField("type here")
	out := stripANSI(f.Render(40))
	if out == "" {
		t.Fatal("expected non-empty render for placeholder")
	}
}

func TestTextFieldRenderShowsValue(t *testing.T) {
	f := newTextField("")
	f.SetValue("mycompany.ghe.com")
	out := stripANSI(f.Render(40))
	if out != "mycompany.ghe.com " {
		t.Fatalf("Render() = %q, want value with trailing cursor space", out)
	}
}

func TestTextFieldRenderScrollsWithCursor(t *testing.T) {
	f := newTextField("")
	f.SetValue("this is a longer value than the field width")
	out := stripANSI(f.Render(10))
	if len(out) > 11 { // 10 visible chars + possible trailing cursor space
		t.Fatalf("Render(10) = %q (len %d), want it clamped to the given width", out, len(out))
	}
}
