package tui2

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// renderPluginView renders a plugin-provided ExtensionView's widgets into a
// plain string block — the lipgloss/v2 equivalent of
// internal/tui/inline_views.go's buildViewComponent. The view's own Title is
// rendered separately by the caller (tui2 shows it in the panel border), so
// only the widget bodies are joined here.
func renderPluginView(view tauchat.ExtensionView) string {
	parts := make([]string, 0, len(view.Widgets))
	for _, w := range view.Widgets {
		parts = append(parts, renderWidget(w))
	}
	return strings.Join(parts, "\n")
}

// renderWidget converts one domain Widget into its rendered string. An
// unrecognized/zero-value Widget (e.g. a newer kind sent by a plugin this
// host doesn't understand yet) renders empty — the same additive-evolution
// guarantee as the proto oneof it was decoded from.
func renderWidget(w tauchat.Widget) string {
	switch w.Kind {
	case tauchat.WidgetKindText:
		if w.Text == nil {
			return ""
		}
		return styleOrPlain(w.Text.Style, w.Text.Text)

	case tauchat.WidgetKindStack:
		if w.Stack == nil {
			return ""
		}
		return renderStackWidget(w.Stack)

	case tauchat.WidgetKindKeyValue:
		if w.KeyValue == nil {
			return ""
		}
		return renderKeyValueWidget(w.KeyValue)

	case tauchat.WidgetKindList:
		if w.List == nil {
			return ""
		}
		return renderListWidget(w.List)

	case tauchat.WidgetKindTable:
		if w.Table == nil {
			return ""
		}
		return renderTableWidget(w.Table)

	case tauchat.WidgetKindProgress:
		if w.Progress == nil {
			return ""
		}
		bar := termkit.ProgressBar(w.Progress.Fraction, 20)
		label := bar
		if w.Progress.Label != "" {
			label = w.Progress.Label + " " + bar
		}
		return styleOrPlain(w.Progress.Style, label)

	case tauchat.WidgetKindDivider:
		label := ""
		if w.Divider != nil {
			label = w.Divider.Label
		}
		return renderDividerWidget(label)

	case tauchat.WidgetKindStatus:
		if w.Status == nil {
			return ""
		}
		return renderStatusWidget(w.Status)

	default:
		return ""
	}
}

// renderStackWidget lays out children vertically (one per line, Gap blank
// lines between them) or horizontally (side by side, Gap spaces between).
func renderStackWidget(w *tauchat.StackWidget) string {
	parts := make([]string, 0, len(w.Children))
	for _, child := range w.Children {
		parts = append(parts, renderWidget(child))
	}
	if w.Direction == tauchat.StackHorizontal {
		if len(parts) == 0 {
			return ""
		}
		spaced := make([]string, 0, len(parts)*2-1)
		gap := strings.Repeat(" ", w.Gap)
		for i, p := range parts {
			if i > 0 && gap != "" {
				spaced = append(spaced, gap)
			}
			spaced = append(spaced, p)
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, spaced...)
	}
	sep := strings.Repeat("\n", w.Gap+1)
	return strings.Join(parts, sep)
}

// renderKeyValueWidget renders a two-column key/value layout, keys
// bold and right-padded to a common column so values align.
func renderKeyValueWidget(w *tauchat.KeyValueWidget) string {
	keyCol := 0
	for _, e := range w.Entries {
		if n := lipgloss.Width(e.Key); n > keyCol {
			keyCol = n
		}
	}
	keyStyle := lipgloss.NewStyle().Bold(true)
	lines := make([]string, 0, len(w.Entries))
	for _, e := range w.Entries {
		pad := keyCol - lipgloss.Width(e.Key)
		key := keyStyle.Render(e.Key) + strings.Repeat(" ", max(pad, 0))
		lines = append(lines, key+"  "+styleOrPlain(e.ValueStyle, e.Value))
	}
	return strings.Join(lines, "\n")
}

// renderListWidget renders a bulleted (or numbered, if Ordered) list.
func renderListWidget(w *tauchat.ListWidget) string {
	lines := make([]string, 0, len(w.Items))
	for i, item := range w.Items {
		bullet := "•"
		if w.Ordered {
			bullet = strconv.Itoa(i+1) + "."
		}
		lines = append(lines, styleOrPlain(w.Style, bullet+" "+item))
	}
	return strings.Join(lines, "\n")
}

// renderTableWidget renders headers/rows via lipgloss/v2's table package.
func renderTableWidget(w *tauchat.TableWidget) string {
	t := table.New()
	if len(w.Headers) > 0 {
		t.Headers(w.Headers...)
	}
	for _, r := range w.Rows {
		t.Row(r.Cells...)
	}
	return t.Render()
}

// renderDividerWidget draws a horizontal rule, optionally interrupted by a
// centered label (e.g. "── done ──────").
func renderDividerWidget(label string) string {
	const width = 40
	if label == "" {
		return strings.Repeat("─", width)
	}
	return "── " + label + " " + strings.Repeat("─", max(width-len(label)-4, 0))
}

// renderStatusWidget renders a single status row: a state glyph, label, and
// optional detail, toned to match the state.
func renderStatusWidget(w *tauchat.StatusWidget) string {
	glyph := "○"
	tone := tauchat.ToneMuted
	switch w.State {
	case tauchat.StatusRunning:
		glyph = "◐"
		tone = tauchat.ToneInfo
	case tauchat.StatusSuccess:
		glyph = "●"
		tone = tauchat.ToneSuccess
	case tauchat.StatusFailed:
		glyph = "✗"
		tone = tauchat.ToneError
	}
	line := glyph + " " + w.Label
	if w.Detail != "" {
		line += "  " + w.Detail
	}
	return styleOrPlain(&tauchat.Style{Tone: tone}, line)
}

// styleOrPlain applies s (if non-nil and visible) to text via lipgloss;
// returns text unchanged for a nil/invisible style.
func styleOrPlain(s *tauchat.Style, text string) string {
	if style := widgetStyle(s); style != nil {
		return style.Render(text)
	}
	return text
}

// widgetStyle converts a domain Style into a lipgloss.Style, or nil when the
// style carries no visible effect. Mirrors internal/tui/inline_views.go's
// styleFn.
func widgetStyle(s *tauchat.Style) *lipgloss.Style {
	if s == nil {
		return nil
	}
	style := lipgloss.NewStyle()
	visible := false
	if c, ok := parseHexColor(s.FgHex); ok {
		style = style.Foreground(themeHex(c))
		visible = true
	} else if c, ok := resolveTone(s.Tone); ok {
		style = style.Foreground(themeHex(c))
		visible = true
	}
	if s.Bold {
		style = style.Bold(true)
		visible = true
	}
	if s.Dim {
		style = style.Faint(true)
		visible = true
	}
	if s.Italic {
		style = style.Italic(true)
		visible = true
	}
	if s.Underline {
		style = style.Underline(true)
		visible = true
	}
	if !visible {
		return nil
	}
	return &style
}

// resolveTone maps a plugin's semantic Style.Tone to tau's theme palette.
// Mirrors internal/tui/inline_views.go's function of the same name.
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

// parseHexColor parses a "#rrggbb" string into a termkit.Color. Mirrors
// internal/tui/inline_views.go's function of the same name.
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
