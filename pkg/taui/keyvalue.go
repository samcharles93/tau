package taui

import (
	"strings"

	"github.com/samcharles93/tau/pkg/taui/termkit"
)

// KeyValueEntry is one row of a KeyValue component.
type KeyValueEntry struct {
	Key   string
	Value string
	// ValueFn optionally styles the value, applied after column alignment so
	// padding is computed from the unstyled visible width.
	ValueFn func(string) string
}

// KeyValue renders aligned "key: value" lines, with keys padded to the width
// of the longest key.
type KeyValue struct {
	entries []KeyValueEntry
}

// NewKeyValue creates a KeyValue component.
func NewKeyValue(entries []KeyValueEntry) *KeyValue {
	return &KeyValue{entries: entries}
}

// Invalidate is a no-op; KeyValue holds no cached render.
func (kv *KeyValue) Invalidate() {}

// Render returns one aligned line per entry.
func (kv *KeyValue) Render(width int) []string {
	if len(kv.entries) == 0 {
		return nil
	}
	keyWidth := 0
	for _, e := range kv.entries {
		if w := VisibleWidth(e.Key); w > keyWidth {
			keyWidth = w
		}
	}
	lines := make([]string, 0, len(kv.entries))
	for _, e := range kv.entries {
		key := e.Key + strings.Repeat(" ", keyWidth-VisibleWidth(e.Key))
		value := e.Value
		if e.ValueFn != nil && termkit.ColorEnabled() {
			value = e.ValueFn(value)
		}
		line := key + ": " + value
		if width > 0 {
			line = TruncateANSIToWidth(line, width, "…")
		}
		lines = append(lines, line)
	}
	return lines
}
