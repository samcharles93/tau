package tui2

import (
	"strings"
	"testing"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// --- renderWidget: text ------------------------------------------------------

func TestRenderWidgetTextPlain(t *testing.T) {
	w := tauchat.Widget{Kind: tauchat.WidgetKindText, Text: &tauchat.TextWidget{Text: "hello"}}
	if got := stripANSI(renderWidget(w)); got != "hello" {
		t.Fatalf("got %q, want %q", got, "hello")
	}
}

func TestRenderWidgetTextNilPayload(t *testing.T) {
	w := tauchat.Widget{Kind: tauchat.WidgetKindText}
	if got := renderWidget(w); got != "" {
		t.Fatalf("expected empty string for nil Text payload, got %q", got)
	}
}

func TestRenderWidgetUnknownKind(t *testing.T) {
	w := tauchat.Widget{Kind: "some-future-kind"}
	if got := renderWidget(w); got != "" {
		t.Fatalf("expected empty string for an unrecognized kind, got %q", got)
	}
}

// --- renderStackWidget -------------------------------------------------------

func TestRenderStackWidgetVertical(t *testing.T) {
	w := tauchat.Widget{Kind: tauchat.WidgetKindStack, Stack: &tauchat.StackWidget{
		Children: []tauchat.Widget{
			{Kind: tauchat.WidgetKindText, Text: &tauchat.TextWidget{Text: "one"}},
			{Kind: tauchat.WidgetKindText, Text: &tauchat.TextWidget{Text: "two"}},
		},
	}}
	out := stripANSI(renderWidget(w))
	if out != "one\ntwo" {
		t.Fatalf("got %q, want %q", out, "one\ntwo")
	}
}

func TestRenderStackWidgetVerticalWithGap(t *testing.T) {
	w := tauchat.Widget{Kind: tauchat.WidgetKindStack, Stack: &tauchat.StackWidget{
		Gap: 1,
		Children: []tauchat.Widget{
			{Kind: tauchat.WidgetKindText, Text: &tauchat.TextWidget{Text: "one"}},
			{Kind: tauchat.WidgetKindText, Text: &tauchat.TextWidget{Text: "two"}},
		},
	}}
	out := stripANSI(renderWidget(w))
	if out != "one\n\ntwo" {
		t.Fatalf("got %q, want %q", out, "one\n\ntwo")
	}
}

func TestRenderStackWidgetHorizontal(t *testing.T) {
	w := tauchat.Widget{Kind: tauchat.WidgetKindStack, Stack: &tauchat.StackWidget{
		Direction: tauchat.StackHorizontal,
		Gap:       1,
		Children: []tauchat.Widget{
			{Kind: tauchat.WidgetKindText, Text: &tauchat.TextWidget{Text: "a"}},
			{Kind: tauchat.WidgetKindText, Text: &tauchat.TextWidget{Text: "b"}},
		},
	}}
	out := stripANSI(renderWidget(w))
	if out != "a b" {
		t.Fatalf("got %q, want %q", out, "a b")
	}
}

// --- renderKeyValueWidget -----------------------------------------------------

func TestRenderKeyValueWidgetAlignsColumn(t *testing.T) {
	w := tauchat.Widget{Kind: tauchat.WidgetKindKeyValue, KeyValue: &tauchat.KeyValueWidget{
		Entries: []tauchat.KeyValueEntry{
			{Key: "a", Value: "1"},
			{Key: "longer", Value: "2"},
		},
	}}
	lines := strings.Split(stripANSI(renderWidget(w)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	// Both value columns should start at the same offset.
	idxA := strings.Index(lines[0], "1")
	idxB := strings.Index(lines[1], "2")
	if idxA != idxB {
		t.Fatalf("value columns misaligned: %q vs %q", lines[0], lines[1])
	}
}

// --- renderListWidget ---------------------------------------------------------

func TestRenderListWidgetUnordered(t *testing.T) {
	w := tauchat.Widget{Kind: tauchat.WidgetKindList, List: &tauchat.ListWidget{Items: []string{"a", "b"}}}
	out := stripANSI(renderWidget(w))
	if out != "• a\n• b" {
		t.Fatalf("got %q, want %q", out, "• a\n• b")
	}
}

func TestRenderListWidgetOrdered(t *testing.T) {
	w := tauchat.Widget{Kind: tauchat.WidgetKindList, List: &tauchat.ListWidget{Items: []string{"a", "b"}, Ordered: true}}
	out := stripANSI(renderWidget(w))
	if out != "1. a\n2. b" {
		t.Fatalf("got %q, want %q", out, "1. a\n2. b")
	}
}

// --- renderTableWidget ---------------------------------------------------------

func TestRenderTableWidget(t *testing.T) {
	w := tauchat.Widget{Kind: tauchat.WidgetKindTable, Table: &tauchat.TableWidget{
		Headers: []string{"Name", "Status"},
		Rows: []tauchat.TableRow{
			{Cells: []string{"foo", "ok"}},
		},
	}}
	out := stripANSI(renderWidget(w))
	for _, want := range []string{"Name", "Status", "foo", "ok"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table output %q missing %q", out, want)
		}
	}
}

// --- renderDividerWidget -------------------------------------------------------

func TestRenderDividerWidgetNoLabel(t *testing.T) {
	w := tauchat.Widget{Kind: tauchat.WidgetKindDivider}
	out := renderWidget(w)
	if !strings.Contains(out, "─") {
		t.Fatalf("expected a rule character, got %q", out)
	}
}

func TestRenderDividerWidgetWithLabel(t *testing.T) {
	w := tauchat.Widget{Kind: tauchat.WidgetKindDivider, Divider: &tauchat.DividerWidget{Label: "done"}}
	out := renderWidget(w)
	if !strings.Contains(out, "done") {
		t.Fatalf("expected label in divider, got %q", out)
	}
}

// --- renderStatusWidget --------------------------------------------------------

func TestRenderStatusWidgetStates(t *testing.T) {
	cases := []struct {
		state tauchat.StatusState
		glyph string
	}{
		{tauchat.StatusRunning, "◐"},
		{tauchat.StatusSuccess, "●"},
		{tauchat.StatusFailed, "✗"},
		{tauchat.StatusNeutral, "○"},
	}
	for _, c := range cases {
		w := tauchat.Widget{Kind: tauchat.WidgetKindStatus, Status: &tauchat.StatusWidget{
			State: c.state, Label: "task", Detail: "info",
		}}
		out := stripANSI(renderWidget(w))
		if !strings.Contains(out, c.glyph) || !strings.Contains(out, "task") || !strings.Contains(out, "info") {
			t.Fatalf("state %q: got %q, want glyph %q + label + detail", c.state, out, c.glyph)
		}
	}
}

// --- widgetStyle / styleOrPlain -------------------------------------------------

func TestStyleOrPlainNilStyle(t *testing.T) {
	if got := styleOrPlain(nil, "text"); got != "text" {
		t.Fatalf("got %q, want %q", got, "text")
	}
}

func TestStyleOrPlainInvisibleStyle(t *testing.T) {
	if got := styleOrPlain(&tauchat.Style{}, "text"); got != "text" {
		t.Fatalf("expected unstyled passthrough for an empty Style, got %q", got)
	}
}

func TestStyleOrPlainBoldAppliesANSI(t *testing.T) {
	got := styleOrPlain(&tauchat.Style{Bold: true}, "text")
	if got == "text" {
		t.Fatal("expected ANSI styling to be applied for Bold: true")
	}
	if stripANSI(got) != "text" {
		t.Fatalf("stripped output = %q, want %q", stripANSI(got), "text")
	}
}

// --- resolveTone / parseHexColor ------------------------------------------------

func TestResolveToneKnown(t *testing.T) {
	if _, ok := resolveTone(tauchat.ToneError); !ok {
		t.Fatal("expected ToneError to resolve")
	}
}

func TestResolveToneUnknown(t *testing.T) {
	if _, ok := resolveTone(tauchat.ToneDefault); ok {
		t.Fatal("expected ToneDefault to not resolve to a color")
	}
}

func TestParseHexColorValid(t *testing.T) {
	c, ok := parseHexColor("#FF8000")
	if !ok {
		t.Fatal("expected a valid hex color to parse")
	}
	if c[0] != 0xFF || c[1] != 0x80 || c[2] != 0x00 {
		t.Fatalf("got %v, want {255 128 0}", c)
	}
}

func TestParseHexColorInvalid(t *testing.T) {
	if _, ok := parseHexColor("not-a-color"); ok {
		t.Fatal("expected an invalid hex string to fail")
	}
}

// --- renderPluginView ----------------------------------------------------------

func TestRenderPluginViewJoinsWidgets(t *testing.T) {
	view := tauchat.ExtensionView{
		Title: "My Panel",
		Widgets: []tauchat.Widget{
			{Kind: tauchat.WidgetKindText, Text: &tauchat.TextWidget{Text: "line one"}},
			{Kind: tauchat.WidgetKindText, Text: &tauchat.TextWidget{Text: "line two"}},
		},
	}
	out := stripANSI(renderPluginView(view))
	if out != "line one\nline two" {
		t.Fatalf("got %q, want %q", out, "line one\nline two")
	}
	// Title is rendered by the caller (the panel border), not embedded here.
	if strings.Contains(out, "My Panel") {
		t.Fatal("renderPluginView should not embed the view title")
	}
}

// --- handleChatEvent: ExtensionViewRenderedEvent / ExtensionCommandResultEvent --

func TestHandleChatEventExtensionViewRenderedRendersWidgets(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ExtensionViewRenderedEvent{
		ViewID: "plugin:panel-1",
		View: tauchat.ExtensionView{
			Title:   "Panel",
			Widgets: []tauchat.Widget{{Kind: tauchat.WidgetKindText, Text: &tauchat.TextWidget{Text: "hi"}}},
		},
	})

	panel, ok := m.panels["plugin:panel-1"]
	if !ok {
		t.Fatal("expected panel to be stored")
	}
	if !strings.Contains(stripANSI(panel.content), "hi") {
		t.Fatalf("expected rendered widget content, got %q", panel.content)
	}
}

func TestHandleChatEventExtensionCommandResultRendersView(t *testing.T) {
	m := newTestModel(&fakeRuntime{}, nil)

	m.handleChatEvent(tauchat.ExtensionCommandResultEvent{
		Output: "ran ok",
		View: &tauchat.ExtensionView{
			Title:   "Result",
			Widgets: []tauchat.Widget{{Kind: tauchat.WidgetKindText, Text: &tauchat.TextWidget{Text: "detail"}}},
		},
	})

	joined := strings.Join(m.renderedLines, "\n")
	if !strings.Contains(joined, "ran ok") {
		t.Fatalf("expected Output in rendered lines, got %q", joined)
	}
	if !strings.Contains(stripANSI(joined), "Result") || !strings.Contains(stripANSI(joined), "detail") {
		t.Fatalf("expected View title+widgets in rendered lines, got %q", joined)
	}
}
