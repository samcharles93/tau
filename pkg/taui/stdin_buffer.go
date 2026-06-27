package taui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	escByte         = '\x1b'
	bracketPasteOn  = "\x1b[200~"
	bracketPasteOff = "\x1b[201~"
)

// Kitty keyboard-protocol modifier bits (the wire value is 1 + this bitmask).
const (
	kittyModShift = 1
	kittyModAlt   = 2
	kittyModCtrl  = 4
	// Lock modifiers must be ignored: a terminal with NumLock/CapsLock active
	// (e.g. ghostty) folds them into the modifier field, so Ctrl arrives as
	// "1+4+128"=133 rather than "1+4"=5. Masking them out keeps key matching
	// independent of lock state.
	kittyLockMods = 64 | 128 // CapsLock | NumLock
)

// canonicalKey normalizes Kitty keyboard-protocol key events back to the legacy
// bytes/sequences the rest of the codebase matches on, regardless of whether the
// protocol is active or which lock keys happen to be on. Lock modifiers
// (NumLock/CapsLock) are stripped from the modifier field so, e.g., an Up arrow
// reported as "\x1b[1;129A" with NumLock on still resolves to "\x1b[A":
//   - CSI-u unmodified keys (Esc/Enter/Tab/Backspace) -> their byte;
//   - CSI-u Ctrl+<letter> -> the control byte (Ctrl+C quit, Ctrl+J newline, ...);
//   - cursor/navigation/function keys -> their bare or canonically-modified form.
func canonicalKey(seq string) string {
	keyNum, mod, final, ok := parseCSI(seq)
	if !ok {
		return seq
	}

	bits := 0
	if mod > 0 {
		bits = (mod - 1) &^ kittyLockMods
	}

	if final == 'u' {
		return canonicalCSIu(keyNum, mod, bits, seq)
	}

	// Cursor / navigation / function keys ("\x1b[<n>;<mod><final>"). With no
	// real modifier left, collapse to the bare legacy form; otherwise re-emit
	// with the lock bits removed so handlers like Ctrl+Left ("\x1b[1;5D") match.
	if mod == 0 {
		return seq // already bare; nothing to normalize
	}
	if final == '~' {
		if bits == 0 {
			return fmt.Sprintf("\x1b[%d~", keyNum)
		}
		return fmt.Sprintf("\x1b[%d;%d~", keyNum, bits+1)
	}
	if bits == 0 {
		return fmt.Sprintf("\x1b[%c", final)
	}
	return fmt.Sprintf("\x1b[1;%d%c", bits+1, final)
}

// canonicalCSIu resolves a CSI-u key event given its codepoint, raw modifier and
// lock-stripped modifier bits.
func canonicalCSIu(cp, mod, bits int, seq string) string {
	if bits == 0 {
		switch cp {
		case 27:
			return "\x1b"
		case 13:
			return "\r"
		case 9:
			return "\t"
		case 127:
			return "\x7f"
		}
		if mod > 0 {
			return fmt.Sprintf("\x1b[%du", cp)
		}
		return seq
	}
	if bits == kittyModCtrl {
		switch {
		case cp >= 'a' && cp <= 'z':
			return string(byte(cp-'a') + 1)
		case cp >= 1 && cp <= 26:
			return string(byte(cp))
		}
	}
	return fmt.Sprintf("\x1b[%d;%du", cp, bits+1)
}

// isKeyFinal reports whether b terminates a CSI keyboard event we normalize.
// 'R' (cursor-position report) and mouse finals are deliberately excluded.
func isKeyFinal(b byte) bool {
	switch b {
	case 'A', 'B', 'C', 'D', 'E', 'F', 'H', 'P', 'Q', 'S', 'Z', '~', 'u':
		return true
	}
	return false
}

// parseCSI decodes a CSI key event "\x1b[<num>[:alt][;<mod>[:event]]<final>"
// into its leading number (1 for bare cursor keys), raw modifier value (0 when
// absent) and final byte. It returns ok=false for non-key CSI sequences such as
// mouse reports ("\x1b[<…M") or device-attribute replies.
func parseCSI(seq string) (keyNum, modifier int, final byte, ok bool) {
	if !strings.HasPrefix(seq, "\x1b[") || len(seq) < 3 {
		return 0, 0, 0, false
	}
	final = seq[len(seq)-1]
	if !isKeyFinal(final) {
		return 0, 0, 0, false
	}
	body := seq[2 : len(seq)-1]
	if body == "" {
		return 1, 0, final, true // bare cursor key, e.g. "\x1b[A"
	}
	if body[0] < '0' || body[0] > '9' {
		return 0, 0, 0, false // "?", "<", etc. — not a plain key event
	}

	keyField, modField, hasMod := strings.Cut(body, ";")
	keyField, _, _ = strings.Cut(keyField, ":")
	keyNum, err := strconv.Atoi(keyField)
	if err != nil {
		return 0, 0, 0, false
	}
	if hasMod && modField != "" {
		mf, _, _ := strings.Cut(modField, ":")
		if v, err := strconv.Atoi(mf); err == nil {
			modifier = v
		}
	}
	return keyNum, modifier, final, true
}

// stdinBuffer splits raw terminal input into discrete key sequences and
// segments bracketed-paste payloads.
//
// A single os.Stdin.Read can return several keystrokes at once ("1q"), a
// partial escape sequence, or a paste wrapped in \x1b[200~ … \x1b[201~. Without
// segmentation those leak into exact-string key matching: "1q" matches neither
// "1" nor "q", and a paste delivers its bracket markers as if they were keys.
//
// Ported from Pi's stdin-buffer.ts, simplified for Go's blocking-read model:
// terminals emit a full escape sequence in a single write, so a read returns
// complete sequences atomically. We therefore skip Pi's 10ms completion timer
// and hold only genuinely partial tails until the next chunk — except a lone
// ESC, which is flushed immediately so the Escape key still fires.
type stdinBuffer struct {
	buf       string
	pasteMode bool
	pasteBuf  string
	onKey     func(seq string)
	onPaste   func(content string)
}

func newStdinBuffer(onKey func(seq string), onPaste func(content string)) *stdinBuffer {
	return &stdinBuffer{onKey: onKey, onPaste: onPaste}
}

// process feeds a raw chunk of input, dispatching any complete key sequences via
// onKey and any complete paste payloads via onPaste.
func (s *stdinBuffer) process(data string) {
	if s.pasteMode {
		s.pasteBuf += data
		s.drainPaste()
		return
	}

	s.buf += data

	// A paste start marker takes priority: emit anything before it as keys,
	// then switch into paste mode for the remainder.
	if i := strings.Index(s.buf, bracketPasteOn); i != -1 {
		before := s.buf[:i]
		s.pasteBuf = s.buf[i+len(bracketPasteOn):]
		s.buf = ""
		s.pasteMode = true

		seqs, _ := extractSequences(before)
		for _, seq := range seqs {
			s.emitKey(seq)
		}
		s.drainPaste()
		return
	}

	seqs, remainder := extractSequences(s.buf)
	s.buf = remainder
	for _, seq := range seqs {
		s.emitKey(seq)
	}

	// Flush a lone ESC so the Escape key isn't held forever waiting for the
	// rest of a sequence that will never arrive.
	if s.buf == string(escByte) {
		s.emitKey(s.buf)
		s.buf = ""
	}
}

// drainPaste emits the paste payload once its end marker has been seen, then
// reprocesses anything that followed the marker as ordinary input.
func (s *stdinBuffer) drainPaste() {
	i := strings.Index(s.pasteBuf, bracketPasteOff)
	if i == -1 {
		return // end marker not here yet; keep buffering.
	}
	content := s.pasteBuf[:i]
	rest := s.pasteBuf[i+len(bracketPasteOff):]
	s.pasteMode = false
	s.pasteBuf = ""
	if s.onPaste != nil {
		s.onPaste(content)
	}
	if rest != "" {
		s.process(rest)
	}
}

func (s *stdinBuffer) emitKey(seq string) {
	if seq != "" && s.onKey != nil {
		s.onKey(seq)
	}
}

// extractSequences splits a buffer into complete input sequences. Each escape
// sequence becomes one element; each non-escape character becomes one element
// (decoded as a full UTF-8 rune). A trailing partial escape sequence is returned
// as the remainder for the caller to carry into the next chunk.
func extractSequences(buf string) (seqs []string, remainder string) {
	pos := 0
	for pos < len(buf) {
		rem := buf[pos:]
		if rem[0] != escByte {
			_, size := utf8.DecodeRuneInString(rem)
			seqs = append(seqs, rem[:size])
			pos += size
			continue
		}

		found := false
		for end := 1; end <= len(rem); end++ {
			switch sequenceStatus(rem[:end]) {
			case statusIncomplete:
				continue
			default: // complete (or, defensively, not-escape)
				seqs = append(seqs, rem[:end])
				pos += end
				found = true
			}
			if found {
				break
			}
		}
		if !found {
			return seqs, rem
		}
	}
	return seqs, ""
}

type seqStatus int

const (
	statusComplete seqStatus = iota
	statusIncomplete
	statusNotEscape
)

var sgrMouseRe = regexp.MustCompile(`^<\d+;\d+;\d+[Mm]$`)

// sequenceStatus reports whether data is a complete escape sequence, a partial
// one needing more bytes, or not an escape sequence at all.
func sequenceStatus(data string) seqStatus {
	if len(data) == 0 || data[0] != escByte {
		return statusNotEscape
	}
	if len(data) == 1 {
		return statusIncomplete
	}

	switch data[1] {
	case '[':
		return csiStatus(data)
	case ']':
		// OSC: terminated by ST (ESC \) or BEL.
		if strings.HasSuffix(data, "\x1b\\") || strings.HasSuffix(data, "\x07") {
			return statusComplete
		}
		return statusIncomplete
	case 'P', '_':
		// DCS / APC: terminated by ST (ESC \).
		if strings.HasSuffix(data, "\x1b\\") {
			return statusComplete
		}
		return statusIncomplete
	case 'O':
		// SS3: ESC O followed by a single final byte.
		if len(data) >= 3 {
			return statusComplete
		}
		return statusIncomplete
	default:
		// Meta key: ESC followed by a single character.
		return statusComplete
	}
}

// csiStatus reports completion for a CSI sequence (ESC [ … final-byte).
func csiStatus(data string) seqStatus {
	if len(data) < 3 {
		return statusIncomplete
	}
	payload := data[2:]

	// Old-style mouse: ESC [ M + 3 coordinate bytes = 6 total.
	if payload[0] == 'M' {
		if len(data) >= 6 {
			return statusComplete
		}
		return statusIncomplete
	}

	last := payload[len(payload)-1]
	if last >= 0x40 && last <= 0x7e {
		// SGR mouse: ESC [ < B;X;Y (M|m).
		if payload[0] == '<' {
			if sgrMouseRe.MatchString(payload) {
				return statusComplete
			}
			return statusIncomplete
		}
		return statusComplete
	}
	return statusIncomplete
}
