package tui

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	gt "github.com/grindlemire/go-tui"
)

type chatInput struct {
	value            *gt.State[string]
	cursorPos        *gt.State[int]
	scrollPos        *gt.State[int]
	blink            *gt.State[bool]
	focused          *gt.State[bool]
	width            int
	placeholder      string
	textStyle        gt.Style
	placeholderStyle gt.Style
	focusColor       gt.Color
	onSubmit         func(string)
	onChange         func(string)
}

func newChatInput(
	value *gt.State[string],
	width int,
	placeholder string,
	textStyle gt.Style,
	placeholderStyle gt.Style,
	focusColor gt.Color,
	onSubmit func(string),
	onChange func(string),
) *chatInput {
	return &chatInput{
		value:            value,
		cursorPos:        gt.NewState(utf8.RuneCountInString(value.Get())),
		scrollPos:        gt.NewState(0),
		blink:            gt.NewState(true),
		focused:          gt.NewState(false),
		width:            width,
		placeholder:      placeholder,
		textStyle:        textStyle,
		placeholderStyle: placeholderStyle,
		focusColor:       focusColor,
		onSubmit:         onSubmit,
		onChange:         onChange,
	}
}

func (i *chatInput) BindApp(app *gt.App) {
	i.value.BindApp(app)
	i.cursorPos.BindApp(app)
	i.scrollPos.BindApp(app)
	i.blink.BindApp(app)
	i.focused.BindApp(app)
}

func (i *chatInput) Render(app *gt.App) *gt.Element {
	i.ensureCursorVisible()
	element := gt.New(
		gt.WithDirection(gt.Row),
		gt.WithWidth(i.width),
		gt.WithHeight(1),
		gt.WithFocusable(true),
		gt.WithAutoFocus(true),
	)
	element.SetOnFocus(func(*gt.Element) { i.Focus() })
	element.SetOnBlur(func(*gt.Element) { i.Blur() })

	if i.value.Get() == "" && i.placeholder != "" && !i.focused.Get() {
		element.AddChild(gt.New(
			gt.WithText(i.placeholder),
			gt.WithTextStyle(i.placeholderStyle),
		))
		return element
	}
	element.AddChild(gt.New(
		gt.WithText(i.displayText()),
		gt.WithTextStyle(i.textStyle),
	))
	return element
}

func (i *chatInput) KeyMap() gt.KeyMap {
	return gt.KeyMap{
		gt.OnFocused(gt.AnyRune, i.insertChar),
		gt.OnFocused(gt.KeyBackspace, i.backspace),
		gt.OnFocused(gt.KeyDelete, i.delete),
		gt.OnFocused(gt.KeyLeft, func(gt.KeyEvent) { i.moveLeft() }),
		gt.OnFocused(gt.KeyRight, func(gt.KeyEvent) { i.moveRight() }),
		gt.OnFocused(gt.KeyLeft.Ctrl(), func(gt.KeyEvent) { i.moveWordLeft() }),
		gt.OnFocused(gt.KeyRight.Ctrl(), func(gt.KeyEvent) { i.moveWordRight() }),
		gt.OnFocused(gt.KeyHome, func(gt.KeyEvent) { i.moveHome() }),
		gt.OnFocused(gt.KeyEnd, func(gt.KeyEvent) { i.moveEnd() }),
		gt.OnFocused(gt.KeyEnter, func(gt.KeyEvent) {
			if i.onSubmit != nil {
				i.onSubmit(i.value.Get())
			}
		}),
	}
}

func (i *chatInput) Watchers() []gt.Watcher {
	return []gt.Watcher{gt.OnTimer(500*time.Millisecond, func() {
		if i.focused.Get() {
			i.blink.Set(!i.blink.Get())
		}
	})}
}

func (i *chatInput) SetText(value string) {
	i.value.Set(value)
	i.cursorPos.Set(utf8.RuneCountInString(value))
	i.scrollPos.Set(max(0, i.cursorPos.Get()-i.visibleWidth()+1))
	i.blink.Set(true)
	if i.onChange != nil {
		i.onChange(value)
	}
}

func (i *chatInput) Focus() {
	if i.focused.Get() {
		return
	}
	i.focused.Set(true)
	i.blink.Set(true)
}

func (i *chatInput) Blur() {
	if !i.focused.Get() {
		return
	}
	i.focused.Set(false)
}

func (i *chatInput) insertChar(ke gt.KeyEvent) {
	runes := []rune(i.value.Get())
	pos := i.clampCursorPos()
	next := make([]rune, 0, len(runes)+1)
	next = append(next, runes[:pos]...)
	next = append(next, ke.Rune)
	next = append(next, runes[pos:]...)
	i.setRunes(next, pos+1)
}

func (i *chatInput) backspace(gt.KeyEvent) {
	runes := []rune(i.value.Get())
	pos := i.clampCursorPos()
	if pos == 0 {
		return
	}
	next := append(runes[:pos-1], runes[pos:]...)
	i.setRunes(next, pos-1)
}

func (i *chatInput) delete(gt.KeyEvent) {
	runes := []rune(i.value.Get())
	pos := i.clampCursorPos()
	if pos >= len(runes) {
		return
	}
	next := append(runes[:pos], runes[pos+1:]...)
	i.setRunes(next, pos)
}

func (i *chatInput) setRunes(runes []rune, cursor int) {
	i.value.Set(string(runes))
	i.cursorPos.Set(clamp(cursor, 0, len(runes)))
	i.blink.Set(true)
	i.ensureCursorVisible()
	if i.onChange != nil {
		i.onChange(i.value.Get())
	}
}

func (i *chatInput) moveLeft() {
	if pos := i.cursorPos.Get(); pos > 0 {
		i.cursorPos.Set(pos - 1)
		i.blink.Set(true)
		i.ensureCursorVisible()
	}
}

func (i *chatInput) moveRight() {
	pos := i.cursorPos.Get()
	if pos < utf8.RuneCountInString(i.value.Get()) {
		i.cursorPos.Set(pos + 1)
		i.blink.Set(true)
		i.ensureCursorVisible()
	}
}

func (i *chatInput) moveWordLeft() {
	runes := []rune(i.value.Get())
	pos := i.clampCursorPos()
	for pos > 0 && unicode.IsSpace(runes[pos-1]) {
		pos--
	}
	for pos > 0 && !unicode.IsSpace(runes[pos-1]) {
		pos--
	}
	i.cursorPos.Set(pos)
	i.blink.Set(true)
	i.ensureCursorVisible()
}

func (i *chatInput) moveWordRight() {
	runes := []rune(i.value.Get())
	pos := i.clampCursorPos()
	for pos < len(runes) && !unicode.IsSpace(runes[pos]) {
		pos++
	}
	for pos < len(runes) && unicode.IsSpace(runes[pos]) {
		pos++
	}
	i.cursorPos.Set(pos)
	i.blink.Set(true)
	i.ensureCursorVisible()
}

func (i *chatInput) moveHome() {
	i.cursorPos.Set(0)
	i.blink.Set(true)
	i.ensureCursorVisible()
}

func (i *chatInput) moveEnd() {
	i.cursorPos.Set(utf8.RuneCountInString(i.value.Get()))
	i.blink.Set(true)
	i.ensureCursorVisible()
}

func (i *chatInput) visibleWidth() int {
	return max(1, i.width)
}

func (i *chatInput) ensureCursorVisible() {
	pos := i.clampCursorPos()
	scroll := i.scrollPos.Get()
	visible := i.visibleWidth()
	if pos < scroll {
		i.scrollPos.Set(pos)
		return
	}
	if pos >= scroll+visible {
		i.scrollPos.Set(pos - visible + 1)
	}
}

func (i *chatInput) displayText() string {
	text := i.value.Get()
	runes := []rune(text)
	pos := i.clampCursorPos()
	visible := i.visibleWidth()
	scroll := clamp(i.scrollPos.Get(), 0, len(runes))

	if !i.focused.Get() {
		if len(runes) == 0 {
			return " "
		}
		return string(runes[scroll:min(len(runes), scroll+visible)])
	}

	cursor := '▌'
	if !i.blink.Get() {
		cursor = ' '
	}
	withCursor := make([]rune, 0, len(runes)+1)
	withCursor = append(withCursor, runes[:pos]...)
	withCursor = append(withCursor, cursor)
	withCursor = append(withCursor, runes[pos:]...)

	viewStart := scroll
	if scroll > pos {
		viewStart = scroll + 1
	}
	viewEnd := min(len(withCursor), viewStart+visible+1)
	return string(withCursor[viewStart:viewEnd])
}

func (i *chatInput) clampCursorPos() int {
	return clamp(i.cursorPos.Get(), 0, utf8.RuneCountInString(i.value.Get()))
}

func completionToken(value string) string {
	return strings.TrimSpace(value)
}
