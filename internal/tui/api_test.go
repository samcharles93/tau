package tui

import "testing"

func TestBuiltinCommandNames(t *testing.T) {
	got := BuiltinCommandNames()
	want := map[string]bool{
		"new":       true,
		"system":    true,
		"model":     true,
		"refresh":   true,
		"reload":    true,
		"reasoning": true,
		"settings":  true,
		"session":   true,
		"resume":    true,
		"debug":     true,
		"exit":      true,
	}
	if len(got) != len(want) {
		t.Fatalf("commands = %#v, want %d commands", got, len(want))
	}
	for _, command := range got {
		if !want[command] {
			t.Fatalf("unexpected command %q in %#v", command, got)
		}
	}
}
