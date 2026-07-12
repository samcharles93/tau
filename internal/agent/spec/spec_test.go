package spec

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltins_ParsesAllDefinitions(t *testing.T) {
	defs, err := Builtins()
	require.NoError(t, err)
	require.Len(t, defs, len(builtinFiles))

	names := make(map[string]*Definition, len(defs))
	for _, def := range defs {
		require.NotEmpty(t, def.Name)
		require.NotEmpty(t, def.Description)
		require.NotEmpty(t, def.Body)
		names[def.Name] = def
	}

	// task is reserved for future programmatic/headless use, not a slash command.
	task, ok := names["task"]
	require.True(t, ok)
	require.False(t, task.UserInvocable)
	require.False(t, task.ModeSwitcher)

	// Everything else defaults (or is explicitly set) to user-invocable.
	for _, def := range defs {
		if def.Name == "task" || def.Name == "tau" {
			continue
		}
		require.True(t, def.UserInvocable, "expected %q to be user-invocable", def.Name)
	}

	plan, ok := names["plan"]
	require.True(t, ok)
	require.Contains(t, plan.Tools, "read")
	require.NotContains(t, plan.Tools, "write", "plan should not be able to write files")
	require.Equal(t, "Planning", plan.DisplayName)
	require.Equal(t, "134", plan.Color)
	require.True(t, plan.ModeSwitcher)

	compact, ok := names["compact"]
	require.True(t, ok)
	require.True(t, compact.UserInvocable)
	require.False(t, compact.ModeSwitcher)

	// Agents without an explicit display-name still get one, defaulted from
	// the command name, so every agent's input-mode indicator has a label.
	research, ok := names["research"]
	require.True(t, ok)
	require.Equal(t, "Research", research.DisplayName)
	require.Empty(t, research.Color)
}

func TestLookup(t *testing.T) {
	def, ok := Lookup("research")
	require.True(t, ok)
	require.Equal(t, "research", def.Name)

	_, ok = Lookup("does-not-exist")
	require.False(t, ok)
}

func TestParse_RequiresNameAndDescription(t *testing.T) {
	_, err := Parse([]byte("---\ndescription: missing name\n---\nbody"))
	require.Error(t, err)

	_, err = Parse([]byte("---\nname: missing-description\n---\nbody"))
	require.Error(t, err)
}

func TestParse_UserInvocableDefaultsTrue(t *testing.T) {
	def, err := Parse([]byte("---\nname: x\ndescription: y\n---\nbody"))
	require.NoError(t, err)
	require.True(t, def.UserInvocable)

	def, err = Parse([]byte("---\nname: x\ndescription: y\nuser-invocable: false\n---\nbody"))
	require.NoError(t, err)
	require.False(t, def.UserInvocable)
}

func TestParse_ModeSwitcherDefaultsToUserInvocable(t *testing.T) {
	def, err := Parse([]byte("---\nname: x\ndescription: y\n---\nbody"))
	require.NoError(t, err)
	require.True(t, def.ModeSwitcher)

	def, err = Parse([]byte("---\nname: x\ndescription: y\nuser-invocable: false\n---\nbody"))
	require.NoError(t, err)
	require.False(t, def.ModeSwitcher)

	def, err = Parse([]byte("---\nname: x\ndescription: y\nmode-switcher: false\n---\nbody"))
	require.NoError(t, err)
	require.True(t, def.UserInvocable)
	require.False(t, def.ModeSwitcher)
}

// TestParse_DisplayNameDefaultsToTitleCase guards against a regression where
// an agent command without an explicit display-name would have no label for
// its input-mode indicator at all.
func TestParse_DisplayNameDefaultsToTitleCase(t *testing.T) {
	def, err := Parse([]byte("---\nname: rubber-duck\ndescription: y\n---\nbody"))
	require.NoError(t, err)
	require.Equal(t, "Rubber-duck", def.DisplayName)

	def, err = Parse([]byte("---\nname: plan\ndescription: y\ndisplay-name: Planning\n---\nbody"))
	require.NoError(t, err)
	require.Equal(t, "Planning", def.DisplayName)
}

// TestParse_ColorPassthrough verifies the optional xterm-256 color string is
// carried through unmodified (as a string — the spec package intentionally
// has no termkit dependency; internal/tui converts it).
func TestParse_ColorPassthrough(t *testing.T) {
	def, err := Parse([]byte("---\nname: x\ndescription: y\ncolor: \"134\"\n---\nbody"))
	require.NoError(t, err)
	require.Equal(t, "134", def.Color)

	def, err = Parse([]byte("---\nname: x\ndescription: y\n---\nbody"))
	require.NoError(t, err)
	require.Empty(t, def.Color)
}
