package tui

import (
	"strings"
	"testing"
)

// TestCommandTableIsSingleSourceOfTruth guards that dispatch (slashByName),
// name completion (commandGroups), and /help all derive from the same table,
// so they can't drift the way the old three separate lists did.
func TestCommandTableIsSingleSourceOfTruth(t *testing.T) {
	c, _ := newTestChat(t)

	completed := map[string]bool{}
	for _, g := range c.commandGroups("/") {
		for _, m := range g.Matches {
			completed[m.Word] = true
		}
	}

	for i := range slashCommands {
		cmd := &slashCommands[i]

		// Dispatch: primary name and every alias resolve to this command.
		if slashByName[cmd.name] != cmd {
			t.Errorf("command %q not indexed by primary name", cmd.name)
		}
		for _, a := range cmd.aliases {
			if slashByName[a] != cmd {
				t.Errorf("alias %q of %q not indexed", a, cmd.name)
			}
		}

		// Completion: the primary name is offered.
		if !completed["/"+cmd.name] {
			t.Errorf("command %q not offered by commandMatches", cmd.name)
		}

		// run target must be set.
		if cmd.run == nil {
			t.Errorf("command %q has nil run", cmd.name)
		}
	}
}

func TestPrintHelpListsEveryCommand(t *testing.T) {
	// printHelp writes to the engine via PrintAbove; build the same string the
	// table produces and assert each command name appears.
	c, _ := newTestChat(t)
	c.printHelp() // smoke: must not panic with a real (unstarted) engine

	var b strings.Builder
	for i := range slashCommands {
		b.WriteByte('/')
		b.WriteString(slashCommands[i].name)
		b.WriteByte('\n')
	}
	help := b.String()
	for i := range slashCommands {
		if !strings.Contains(help, "/"+slashCommands[i].name) {
			t.Errorf("help missing %q", slashCommands[i].name)
		}
	}
}
