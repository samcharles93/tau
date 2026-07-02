package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tauchat "github.com/samcharles93/tau/internal/chat"
)

// TestSkillsCommandIntegration tests the /skills list command integration.
func TestSkillsCommandIntegration(t *testing.T) {
	// Test that the command is in the help
	helpText := printHelpForTest()
	if !strings.Contains(helpText, "/skills list") {
		t.Errorf("Help text should contain '/skills list', got: %s", helpText)
	}

	// Test that the command sends the right command type
	runtime := &mockRuntime{}
	chat := &inlineChat{
		runtime: runtime,
	}

	chat.handleSlashCommand("/skills list")
	time.Sleep(10 * time.Millisecond)

	if len(runtime.commands) == 0 {
		t.Fatal("Expected a command to be sent")
	}

	_, ok := runtime.commands[0].(tauchat.ListSkillsCommand)
	if !ok {
		t.Errorf("Expected ListSkillsCommand, got %T", runtime.commands[0])
	}
}

// printHelpForTest returns the help text for testing.
func printHelpForTest() string {
	var b strings.Builder
	for i := range slashCommands {
		cmd := &slashCommands[i]
		label := "/" + cmd.name
		if cmd.usage != "" {
			label += " " + cmd.usage
		}
		fmt.Fprintf(&b, "  %-38s %s\n", label, cmd.description)
	}
	return b.String()
}

// mockRuntime captures commands sent to the runtime.
type mockRuntime struct {
	commands []tauchat.ChatCommand
}

func (m *mockRuntime) Send(cmd tauchat.ChatCommand) error {
	m.commands = append(m.commands, cmd)
	return nil
}

func (m *mockRuntime) Close() {
	// No-op
}
