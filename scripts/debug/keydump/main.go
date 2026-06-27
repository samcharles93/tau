// Command keydump prints the raw bytes a terminal sends for each keypress, with
// the Kitty keyboard protocol enabled exactly as tau enables it (flag 1). It is
// a debugging aid for terminal key-handling issues — e.g. discovering that a
// terminal folds NumLock/CapsLock into the modifier field.
//
// Run it, press the keys you want to inspect, then press 'q' to quit:
//
//	go run ./scripts/debug/keydump
//
// For CSI-u key events it also decodes the codepoint and active modifiers, so a
// line like:
//
//	hex: 1b5b39393b31333375  raw: "\x1b[99;133u"  key=Ctrl+c  mods=ctrl+numlock
//
// makes it obvious when a lock key is altering the modifier value.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func main() {
	// Raw mode via stty: byte-at-a-time, no echo, no signal translation.
	stty("raw", "-echo")
	defer stty("sane")

	// Enable the Kitty keyboard protocol, flag 1 ("disambiguate escape codes"),
	// identical to tau's startup. Pop it again on exit.
	os.Stdout.WriteString("\x1b[>1u")
	defer os.Stdout.WriteString("\x1b[<u")

	os.Stdout.WriteString("keydump — Kitty keyboard protocol (flag 1) enabled.\r\n")
	os.Stdout.WriteString("Press keys to inspect (Ctrl+C, Ctrl+J, Shift+Enter, ...). Press 'q' to quit.\r\n\r\n")

	buf := make([]byte, 64)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			return
		}
		b := buf[:n]
		line := fmt.Sprintf("hex: %-22x  raw: %q", b, string(b))
		if d := decodeCSIu(string(b)); d != "" {
			line += "  " + d
		}
		os.Stdout.WriteString(line + "\r\n")
		if n == 1 && b[0] == 'q' {
			return
		}
	}
}

// decodeCSIu renders a human-readable summary of a Kitty "\x1b[<cp>;<mod>u" key
// event, or "" if seq is not one.
func decodeCSIu(seq string) string {
	if !strings.HasPrefix(seq, "\x1b[") || !strings.HasSuffix(seq, "u") {
		return ""
	}
	body := seq[2 : len(seq)-1]
	cpField, modField, hasMod := strings.Cut(body, ";")
	cpField, _, _ = strings.Cut(cpField, ":")
	cp, err := strconv.Atoi(cpField)
	if err != nil {
		return ""
	}

	mods := 0
	if hasMod {
		modField, _, _ = strings.Cut(modField, ":")
		if v, err := strconv.Atoi(modField); err == nil && v > 0 {
			mods = v - 1
		}
	}

	key := fmt.Sprintf("U+%04X", cp)
	if cp >= 0x20 && cp < 0x7f {
		key = string(rune(cp))
	}

	var names []string
	for _, m := range []struct {
		bit  int
		name string
	}{
		{1, "shift"},
		{2, "alt"},
		{4, "ctrl"},
		{8, "super"},
		{16, "hyper"},
		{32, "meta"},
		{64, "capslock"},
		{128, "numlock"},
	} {
		if mods&m.bit != 0 {
			names = append(names, m.name)
		}
	}
	mod := "none"
	if len(names) > 0 {
		mod = strings.Join(names, "+")
	}
	return fmt.Sprintf("key=%s  mods=%s", key, mod)
}

func stty(args ...string) {
	c := exec.CommandContext(context.Background(), "stty", args...)
	c.Stdin = os.Stdin
	_ = c.Run()
}
