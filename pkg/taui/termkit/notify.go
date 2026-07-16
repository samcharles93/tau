package termkit

import (
	"encoding/base64"
	"strings"
)

// osc99NotifyID is a fixed notification id so successive tau notifications
// replace the previous one (per the OSC 99 spec) rather than piling up in
// the terminal's/OS's notification center.
const osc99NotifyID = "tau-turn"

// notifyBodyMaxRunes bounds the notification body across every protocol
// below. Desktop notification UIs truncate long text anyway, and a smaller
// payload keeps each escape sequence itself small.
const notifyBodyMaxRunes = 300

// Notify returns a desktop-notification escape sequence carrying title and
// body, layering four protocols back to back so it degrades gracefully
// across terminals without sniffing $TERM/$TERM_PROGRAM (unreliable once a
// multiplexer is in the picture):
//
//   - OSC 99 (Kitty, Ghostty): rich notification, sent as two chunks sharing
//     one id - a bare OSC 99 payload is title-only, so title and body need
//     separate chunks per the spec.
//   - OSC 777 (rxvt-style; also honored by some other emulators): simpler
//     "notify;title;body" form with no escaping mechanism of its own, so
//     title/body are sanitized rather than base64-encoded.
//   - OSC 9 (iTerm2 and broad fallback): body-only, so title is folded in as
//     "title: body" when present.
//   - BEL (\a): universal audible/visual bell, understood by every terminal
//     since forever - the last-resort signal if none of the above land.
//
// A terminal that recognizes none of the first three simply ignores each
// (correctly terminated) OSC sequence rather than printing it as garbage,
// so layering them costs nothing on terminals that support fewer of them -
// except that a terminal supporting more than one may show more than one
// notification for the same event. That's a deliberate trade for broad
// compatibility over fragile terminal sniffing.
func Notify(title, body string) string {
	body = truncateRunes(body, notifyBodyMaxRunes)

	var b strings.Builder
	b.WriteString(osc99(title, body))
	b.WriteString(osc777(title, body))
	b.WriteString(osc9(title, body))
	b.WriteString("\a")
	return b.String()
}

func osc99(title, body string) string {
	var b strings.Builder
	b.WriteString("\x1b]99;i=")
	b.WriteString(osc99NotifyID)
	b.WriteString(":d=0:e=1;")
	b.WriteString(base64.StdEncoding.EncodeToString([]byte(title)))
	b.WriteString("\x1b\\")
	b.WriteString("\x1b]99;i=")
	b.WriteString(osc99NotifyID)
	b.WriteString(":d=1:e=1:p=body;")
	b.WriteString(base64.StdEncoding.EncodeToString([]byte(body)))
	b.WriteString("\x1b\\")
	return b.String()
}

// osc777 builds the rxvt-style "\033]777;notify;title;body\007" sequence.
// The format has no escaping of its own, so ";" and control bytes in
// title/body are sanitized rather than encoded.
func osc777(title, body string) string {
	return "\x1b]777;notify;" + sanitizeNotifyText(title) + ";" + sanitizeNotifyText(body) + "\x07"
}

// osc9 builds the iTerm2-style "\033]9;text\007" sequence. It carries a
// single string, so title is folded into the body when present.
func osc9(title, body string) string {
	text := sanitizeNotifyText(body)
	if title = sanitizeNotifyText(title); title != "" {
		text = title + ": " + text
	}
	return "\x1b]9;" + text + "\x07"
}

// sanitizeNotifyText collapses newlines/tabs to spaces, strips other control
// bytes, replaces ";" (which OSC 777 splits fields on with no escape
// mechanism), and collapses repeated spaces from that substitution.
func sanitizeNotifyText(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return ' '
		case r == ';':
			return ','
		case r < 0x20 || r == 0x7f:
			return -1
		default:
			return r
		}
	}, s)
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
