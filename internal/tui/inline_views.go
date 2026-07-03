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
		if w.Text == nil {
			return &taui.Container{}
		}
		t := taui.NewStyledText(w.Text.Text, styleFn(w.Text.Style), nil)
		return t

	case tauchat.WidgetKindStack:
		if w.Stack == nil {
			return &taui.Container{}
		}
		dir := taui.StackVertical
		if w.Stack.Direction == tauchat.StackHorizontal {
			dir = taui.StackHorizontal
		}
		s := taui.NewStack(dir, w.Stack.Gap)
		for _, child := range w.Stack.Children {
			s.AddChild(renderWidget(child))
		}
		return s

	case tauchat.WidgetKindKeyValue:
		if w.KeyValue == nil {
			return &taui.Container{}
		}
		entries := make([]taui.KeyValueEntry, 0, len(w.KeyValue.Entries))
		for _, e := range w.KeyValue.Entries {
			entries = append(entries, taui.KeyValueEntry{
				Key:     e.Key,
				Value:   e.Value,
				ValueFn: styleFn(e.ValueStyle),
			})
		}
		return taui.NewKeyValue(entries)

	case tauchat.WidgetKindList:
		if w.List == nil {
			return &taui.Container{}
		}
		l := taui.NewList(w.List.Items, w.List.Ordered)
		if fn := styleFn(w.List.Style); fn != nil {
			l.SetStyle(fn)
		}
		return l

	case tauchat.WidgetKindTable:
		if w.Table == nil {
			return &taui.Container{}
		}
		rows := make([][]string, 0, len(w.Table.Rows))
		for _, r := range w.Table.Rows {
			rows = append(rows, r.Cells)
		}
		return taui.NewTable(w.Table.Headers, rows)

	case tauchat.WidgetKindProgress:
		if w.Progress == nil {
			return &taui.Container{}
		}
		bar := termkit.ProgressBar(w.Progress.Fraction, 20)
		label := bar
		if w.Progress.Label != "" {
			label = w.Progress.Label + " " + bar
		}
		return taui.NewStyledText(label, styleFn(w.Progress.Style), nil)

	case tauchat.WidgetKindDivider:
		label := ""
		if w.Divider != nil {
			label = w.Divider.Label
		}
		return taui.NewDivider(label)

	case tauchat.WidgetKindStatus:
		if w.Status == nil {
			return &taui.Container{}
		}
		state := taui.StatusRowNeutral
		switch w.Status.State {
		case tauchat.StatusRunning:
			state = taui.StatusRowRunning
		case tauchat.StatusSuccess:
			state = taui.StatusRowSuccess
		case tauchat.StatusFailed:
			state = taui.StatusRowFailed
		}
		return taui.NewStatusRow(w.Status.Label, w.Status.Detail, state)

	default:
		return &taui.Container{}
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
