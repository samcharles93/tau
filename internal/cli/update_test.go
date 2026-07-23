package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdateCmdHelpMentionsBackground(t *testing.T) {
	t.Parallel()

	cmd := updateCmd("v1.0.0")
	require.Contains(t, cmd.Usage, "background checks",
		"update command usage should mention background checks via updates.mode")

	require.Contains(t, cmd.Description, "updates.mode",
		"update command description should reference updates.mode config key")

	require.Contains(t, cmd.Description, "auto",
		"update command description should mention auto mode")
}

func TestUpdateCmdUsageDoesNotContainOnlyManualFlow(t *testing.T) {
	t.Parallel()

	cmd := updateCmd("v1.0.0")
	require.False(t, strings.HasPrefix(cmd.Usage, "Update tau to the latest GitHub release"),
		"update command usage should reference background checks, not just manual flow")
}
