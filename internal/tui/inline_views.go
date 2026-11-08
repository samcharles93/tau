package tui

import (
	"strconv"
	"strings"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/pkg/taui"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// buildViewComponent renders a plugin-provided ExtensionView into a taui
// component tree: a padded Box containing an optional bold title followed by
// each widget, in order. Used for both the sync path (rendered once to
// scrollback) and the async path (mounted live into c.panels).
func buildViewComponent(view tauchat.ExtensionView) taui.Component {
	box := taui.NewBox().Padding(1, 0).Build()
	if view.Title != "" {
		titleFn := func(s string) string { return termkit.Wrap(s, termkit.Bold) }
		if fn := styleFn(view.Style); fn != nil {
			titleFn = func(s string) string { return fn(termkit.Wrap(s, termkit.Bold)) }
		}
		box.AddChild(taui.NewStyledText(view.Title, titleFn, nil))
	}
	for _, w := range view.Widgets {
		box.AddChild(renderWidget(w))
	}
	return box
}

// renderWidget converts one domain Widget into its taui.Component. An
// unrecognized/zero-value Widget (e.g. a newer kind sent by a plugin this
// host doesn't understand yet) renders as an empty container - the same
// additive-evolution guarantee as the proto oneof it was decoded from.
func renderWidget(w tauchat.Widget) taui.Component {
	switch w.Kind {
	case tauchat.WidgetKindText:
		return renderTextWidget(w.Text)
	case tauchat.WidgetKindStack:
		return renderStackWidget(w.Stack)
	case tauchat.WidgetKindKeyValue:
		return renderKeyValueWidget(w.KeyValue)
	case tauchat.WidgetKindList:
		return renderListWidget(w.List)
	case tauchat.WidgetKindTable:
		return renderTableWidget(w.Table)
	case tauchat.WidgetKindProgress:
		return renderProgressWidget(w.Progress)
	case tauchat.WidgetKindDivider:
		return renderDividerWidget(w.Divider)
	case tauchat.WidgetKindStatus:
		return renderStatusWidget(w.Status)
	default:
		return &taui.Container{}
	}
}

func renderTextWidget(t *tauchat.TextWidget) taui.Component {
	if t == nil {
		return &taui.Container{}
	}
	return taui.NewStyledText(t.Text, styleFn(t.Style), nil)
}

func renderStackWidget(s *tauchat.StackWidget) taui.Component {
	if s == nil {
		return &taui.Container{}
	}
	dir := taui.StackVertical
	if s.Direction == tauchat.StackHorizontal {
		dir = taui.StackHorizontal
	}
	stack := taui.NewStack(dir, s.Gap)
	for _, child := range s.Children {
		stack.AddChild(renderWidget(child))
	}
	return stack
}

func renderKeyValueWidget(kv *tauchat.KeyValueWidget) taui.Component {
	if kv == nil {
		return &taui.Container{}
	}
	entries := make([]taui.KeyValueEntry, 0, len(kv.Entries))
	for _, e := range kv.Entries {
		entries = append(entries, taui.KeyValueEntry{
			Key:     e.Key,
			Value:   e.Value,
			ValueFn: styleFn(e.ValueStyle),
		})
	}
	return taui.NewKeyValue(entries)
}

func renderListWidget(l *tauchat.ListWidget) taui.Component {
	if l == nil {
		return &taui.Container{}
	}
	list := taui.NewList(l.Items, l.Ordered)
	if fn := styleFn(l.Style); fn != nil {
		list.SetStyle(fn)
	}
	return list
}

func renderTableWidget(t *tauchat.TableWidget) taui.Component {
	if t == nil {
		return &taui.Container{}
	}
	rows := make([][]string, 0, len(t.Rows))
	for _, r := range t.Rows {
		rows = append(rows, r.Cells)
	}
	return taui.NewTable(t.Headers, rows)
}

func renderProgressWidget(p *tauchat.ProgressWidget) taui.Component {
	if p == nil {
		return &taui.Container{}
	}
	bar := termkit.ProgressBar(p.Fraction, 20)
	label := bar
	if p.Label != "" {
		label = p.Label + " " + bar
	}
	return taui.NewStyledText(label, styleFn(p.Style), nil)
}

func renderDividerWidget(d *tauchat.DividerWidget) taui.Component {
	label := ""
	if d != nil {
		label = d.Label
	}
	return taui.NewDivider(label)
}

func renderStatusWidget(s *tauchat.StatusWidget) taui.Component {
	if s == nil {
		return &taui.Container{}
	}
	return taui.NewStatusRow(s.Label, s.Detail, statusRowState(s.State))
}

// statusRowState maps a domain status state to the taui status-row state.
func statusRowState(state tauchat.StatusState) taui.StatusRowState {
	switch state {
	case tauchat.StatusRunning:
		return taui.StatusRowRunning
	case tauchat.StatusSuccess:
		return taui.StatusRowSuccess
	case tauchat.StatusFailed:
		return taui.StatusRowFailed
	default:
		return taui.StatusRowNeutral
	}
}

// styleFn converts a domain Style into a taui foreground colour callback.
// Tone is resolved against tau's theme palette so plugin panels stay visually
// consistent across theme changes; FgHex is the escape hatch when a plugin
// needs a specific colour tone doesn't cover. Returns nil when the style
// carries no visible effect.
func styleFn(s *tauchat.Style) func(string) string {
	if s == nil {
		return nil
	}
	var codes []string
	if c, ok := parseHexColor(s.FgHex); ok {
		codes = append(codes, c.Fg())
	} else if c, ok := resolveTone(s.Tone); ok {
		codes = append(codes, c.Fg())
	}
	if s.Bold {
		codes = append(codes, termkit.Bold)
	}
	if s.Dim {
		codes = append(codes, termkit.Dim)
	}
	if s.Italic {
		codes = append(codes, termkit.Italic)
	}
	if s.Underline {
		codes = append(codes, termkit.Underline)
	}
	if len(codes) == 0 {
		return nil
	}
	return func(text string) string { return termkit.Wrap(text, codes...) }
}

// resolveTone maps a plugin's semantic Style.Tone to tau's theme palette.
func resolveTone(tone tauchat.StyleTone) (termkit.Color, bool) {
	switch tone {
	case tauchat.ToneInfo:
		return theme.ToneInfo, true
	case tauchat.ToneSuccess:
		return theme.ToneSuccess, true
	case tauchat.ToneWarn:
		return theme.ToneWarn, true
	case tauchat.ToneError:
		return theme.ToneError, true
	case tauchat.ToneMuted:
		return theme.ToneMuted, true
	default:
		return termkit.Color{}, false
	}
}

// parseHexColor parses a "#rrggbb" string into a termkit.Color.
func parseHexColor(s string) (termkit.Color, bool) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return termkit.Color{}, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return termkit.Color{}, false
	}
	return termkit.Color{uint8(v >> 16), uint8(v >> 8), uint8(v)}, true
}
