package tui2

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

type pickerAction int

const (
	pickerActionNone pickerAction = iota
	pickerActionClose
	pickerActionSelect
)

// listPicker is a reusable searchable-list component. It owns only the UI
// mechanics that every picker shares: query editing, selection, scrolling,
// and rendering. Callers remain responsible for supplying rows and applying
// the selected value.
type listPicker struct {
	title    string
	query    string
	cursor   int
	selected int
}

func newListPicker(title string) listPicker {
	return listPicker{title: title}
}

func (p *listPicker) Query() string { return p.query }

func (p *listPicker) SetQuery(query string) {
	p.query = query
	p.cursor = utf8.RuneCountInString(query)
	p.selected = 0
}

// ClampSelection keeps the current selection inside the supplied row count
// and returns the resulting index. The mutating name is intentional: callers
// use this whenever filtering may have replaced the visible row set.
func (p *listPicker) ClampSelection(rowCount int) int {
	if p.selected < 0 || p.selected >= rowCount {
		p.selected = 0
	}
	return p.selected
}

// HandleKey consumes the common interaction vocabulary for a searchable
// picker. Global shortcuts are deliberately left to the caller so the same
// component can be embedded in different overlays.
func (p *listPicker) HandleKey(msg tea.KeyPressMsg, rowCount int) (pickerAction, bool) {
	p.ClampSelection(rowCount)

	switch msg.String() {
	case "esc":
		return pickerActionClose, true
	case "up", "shift+tab":
		if p.selected > 0 {
			p.selected--
		}
		return pickerActionNone, true
	case "down", "tab":
		if p.selected < rowCount-1 {
			p.selected++
		}
		return pickerActionNone, true
	case "enter":
		if rowCount > 0 {
			return pickerActionSelect, true
		}
		return pickerActionNone, true
	case "left":
		p.cursor = max(p.cursor-1, 0)
		return pickerActionNone, true
	case "right":
		p.cursor = min(p.cursor+1, utf8.RuneCountInString(p.query))
		return pickerActionNone, true
	case "home", "ctrl+a":
		p.cursor = 0
		return pickerActionNone, true
	case "end", "ctrl+e":
		p.cursor = utf8.RuneCountInString(p.query)
		return pickerActionNone, true
	case "ctrl+u":
		runes := []rune(p.query)
		p.query = string(runes[p.cursor:])
		p.cursor = 0
		p.selected = 0
		return pickerActionNone, true
	case "ctrl+k":
		runes := []rune(p.query)
		p.query = string(runes[:p.cursor])
		p.selected = 0
		return pickerActionNone, true
	case "ctrl+w", "ctrl+backspace":
		start := pickerWordLeft(p.query, p.cursor)
		runes := []rune(p.query)
		p.query = string(append(runes[:start], runes[p.cursor:]...))
		p.cursor = start
		p.selected = 0
		return pickerActionNone, true
	case "backspace":
		if p.cursor > 0 {
			runes := []rune(p.query)
			p.query = string(append(runes[:p.cursor-1], runes[p.cursor:]...))
			p.cursor--
			p.selected = 0
		}
		return pickerActionNone, true
	case "delete":
		runes := []rune(p.query)
		if p.cursor < len(runes) {
			p.query = string(append(runes[:p.cursor], runes[p.cursor+1:]...))
			p.selected = 0
		}
		return pickerActionNone, true
	default:
		text := msg.Key().Text
		if text == "" {
			return pickerActionNone, false
		}
		r, _ := utf8.DecodeRuneInString(text)
		if r < 32 || r == utf8.RuneError {
			return pickerActionNone, false
		}
		runes := []rune(p.query)
		insert := []rune(text)
		runes = append(runes[:p.cursor], append(insert, runes[p.cursor:]...)...)
		p.query = string(runes)
		p.cursor += len(insert)
		p.selected = 0
		return pickerActionNone, true
	}
}

func pickerWordLeft(query string, cursor int) int {
	runes := []rune(query)
	i := min(cursor, len(runes))
	for i > 0 && runes[i-1] == ' ' {
		i--
	}
	for i > 0 && runes[i-1] != ' ' {
		i--
	}
	return i
}

func (p *listPicker) Render(rows []compRow, width int) string {
	innerWidth := max(width-4, 1)
	selected := p.ClampSelection(len(rows))
	body := []string{
		renderPickerSearch(p.query, p.cursor, innerWidth),
		paletteDividerStyle.Render(strings.Repeat("─", innerWidth)),
	}
	if len(rows) == 0 {
		body = append(body, compDescStyle.Render("No matches"))
	} else {
		body = append(body, strings.Split(renderCompletions(rows, selected, innerWidth), "\n")...)
	}
	body = append(
		body,
		paletteDividerStyle.Render(strings.Repeat("─", innerWidth)),
		compDescStyle.Render(fmt.Sprintf("↑↓ navigate  enter select  esc close  ·  %d results", len(rows))),
	)
	return renderBoxAround(width, p.title, body)
}

func renderPickerSearch(query string, cursor, width int) string {
	const label = "Search  "
	available := max(width-visibleWidth(label), 1)
	runes := []rune(query)
	cursor = max(0, min(cursor, len(runes)))

	cursorCell := 0
	if cursor == len(runes) {
		cursorCell = 1
	}
	runeCapacity := max(available-cursorCell, 0)
	start := 0
	if cursor < len(runes) && cursor >= runeCapacity {
		start = cursor - runeCapacity + 1
	} else if cursor == len(runes) && len(runes) > runeCapacity {
		start = len(runes) - runeCapacity
	}
	showEllipsis := start > 0 && runeCapacity > 1
	if showEllipsis {
		runeCapacity--
		if cursor < len(runes) {
			start = max(cursor-runeCapacity+1, 0)
		} else {
			start = max(len(runes)-runeCapacity, 0)
		}
	}
	end := min(start+runeCapacity, len(runes))
	visible := runes[start:end]

	var value strings.Builder
	if showEllipsis {
		value.WriteString("…")
	}
	rel := cursor - start
	for i, r := range visible {
		if i == rel {
			value.WriteString(inputCursorStyle.Render(string(r)))
		} else {
			value.WriteRune(r)
		}
	}
	if cursor == len(runes) {
		value.WriteString(inputCursorStyle.Render(" "))
	}
	if query == "" {
		value.WriteString(compDescStyle.Render("type to filter"))
	}
	return paletteSearchLabelStyle.Render(label) + value.String()
}
