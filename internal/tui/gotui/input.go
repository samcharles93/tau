package gotui

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	gotui "github.com/grindlemire/go-tui"
)

type chatInput struct {
	value            *gotui.State[string]
	cursorPos        *gotui.State[int]
	scrollPos        *gotui.State[int]
	blink            *gotui.State[bool]
	focused          *gotui.State[bool]
	width            int
	placeholder      string
	textStyle        gotui.Style
	placeholderStyle gotui.Style
	focusColor       gotui.Color
	onSubmit         func(string)
	onChange         func(string)
}

func newChatInput(
	value *gotui.State[string],
	width int,
	placeholder string,
	textStyle gotui.Style,
	placeholderStyle gotui.Style,
	focusColor gotui.Color,
	onSubmit func(string),
	onChange func(string),
) *chatInput {
	return &chatInput{
		value:            value,
		cursorPos:        gotui.NewState(utf8.RuneCountInString(value.Get())),
		scrollPos:        gotui.NewState(0),
		blink:            gotui.NewState(true),
		focused:          gotui.NewState(false),
		width:            width,
		placeholder:      placeholder,
		textStyle:        textStyle,
		placeholderStyle: placeholderStyle,
		focusColor:       focusColor,
		onSubmit:         onSubmit,
		onChange:         onChange,
	}
}

func (i *chatInput) BindApp(app *gotui.App) {
	i.value.BindApp(app)
	i.cursorPos.BindApp(app)
	i.scrollPos.BindApp(app)
	i.blink.BindApp(app)
	i.focused.BindApp(app)
}

func (i *chatInput) Render(app *gotui.App) *gotui.Element {
	i.ensureCursorVisible()
	element := gotui.New(
		gotui.WithDirection(gotui.Row),
		gotui.WithWidth(i.width),
		gotui.WithHeight(1),
		gotui.WithFocusable(true),
		gotui.WithAutoFocus(true),
	)
	element.SetOnFocus(func(*gotui.Element) { i.Focus() })
	element.SetOnBlur(func(*gotui.Element) { i.Blur() })

	if i.value.Get() == "" && i.placeholder != "" && !i.focused.Get() {
		element.AddChild(gotui.New(
			gotui.WithText(i.placeholder),
			gotui.WithTextStyle(i.placeholderStyle),
		))
		return element
	}
	element.AddChild(gotui.New(
		gotui.WithText(i.displayText()),
		gotui.WithTextStyle(i.textStyle),
	))
	return element
}

func (i *chatInput) KeyMap() gotui.KeyMap {
	return gotui.KeyMap{
		gotui.OnFocused(gotui.AnyRune, i.insertChar),
		gotui.OnFocused(gotui.KeyBackspace, i.backspace),
		gotui.OnFocused(gotui.KeyDelete, i.delete),
		gotui.OnFocused(gotui.KeyLeft, func(gotui.KeyEvent) { i.moveLeft() }),
		gotui.OnFocused(gotui.KeyRight, func(gotui.KeyEvent) { i.moveRight() }),
		gotui.OnFocused(gotui.KeyLeft.Ctrl(), func(gotui.KeyEvent) { i.moveWordLeft() }),
		gotui.OnFocused(gotui.KeyRight.Ctrl(), func(gotui.KeyEvent) { i.moveWordRight() }),
		gotui.OnFocused(gotui.KeyHome, func(gotui.KeyEvent) { i.moveHome() }),
		gotui.OnFocused(gotui.KeyEnd, func(gotui.KeyEvent) { i.moveEnd() }),
		gotui.OnFocused(gotui.KeyEnter, func(gotui.KeyEvent) {
			if i.onSubmit != nil {
				i.onSubmit(i.value.Get())
			}
		}),
	}
}

func (i *chatInput) Watchers() []gotui.Watcher {
	return []gotui.Watcher{gotui.OnTimer(500*time.Millisecond, func() {
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

func (i *chatInput) insertChar(ke gotui.KeyEvent) {
	runes := []rune(i.value.Get())
	pos := i.clampCursorPos()
	next := make([]rune, 0, len(runes)+1)
	next = append(next, runes[:pos]...)
	next = append(next, ke.Rune)
	next = append(next, runes[pos:]...)
	i.setRunes(next, pos+1)
}

func (i *chatInput) backspace(gotui.KeyEvent) {
	runes := []rune(i.value.Get())
	pos := i.clampCursorPos()
	if pos == 0 {
		return
	}
	next := append(runes[:pos-1], runes[pos:]...)
	i.setRunes(next, pos-1)
}

func (i *chatInput) delete(gotui.KeyEvent) {
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
