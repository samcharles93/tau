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

	// Everything else defaults (or is explicitly set) to user-invocable.
	for _, def := range defs {
		if def.Name == "task" {
			continue
		}
		require.True(t, def.UserInvocable, "expected %q to be user-invocable", def.Name)
	}

	plan, ok := names["plan"]
	require.True(t, ok)
	require.Contains(t, plan.Tools, "read")
	require.NotContains(t, plan.Tools, "write", "plan should not be able to write files")
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
