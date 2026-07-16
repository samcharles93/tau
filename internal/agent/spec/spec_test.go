package spec

import (
	"testing"
	"time"

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

	// task.agent.md was retired in P0.4 - tau is now the spawnable
	// general-purpose child worker. tau is not user-invocable.
	// Everything else defaults (or is explicitly set) to user-invocable.
	for _, def := range defs {
		if def.Name == "tau" {
			require.False(t, def.UserInvocable, "expected tau to not be user-invocable")
			require.False(t, def.ModeSwitcher, "expected tau to not be mode-switcher")
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
// carried through unmodified (as a string - the spec package intentionally
// has no termkit dependency; internal/tui converts it).
func TestParse_ColorPassthrough(t *testing.T) {
	def, err := Parse([]byte("---\nname: x\ndescription: y\ncolor: \"134\"\n---\nbody"))
	require.NoError(t, err)
	require.Equal(t, "134", def.Color)

	def, err = Parse([]byte("---\nname: x\ndescription: y\n---\nbody"))
	require.NoError(t, err)
	require.Empty(t, def.Color)
}

func TestParse_ProviderPassthrough(t *testing.T) {
	def, err := Parse([]byte("---\nname: x\ndescription: y\nprovider: openai\n---\nbody"))
	require.NoError(t, err)
	require.Equal(t, "openai", def.Provider)

	def, err = Parse([]byte("---\nname: x\ndescription: y\n---\nbody"))
	require.NoError(t, err)
	require.Empty(t, def.Provider)
}

func TestParse_MaxTurnsValid(t *testing.T) {
	def, err := Parse([]byte("---\nname: x\ndescription: y\nmax-turns: 10\n---\nbody"))
	require.NoError(t, err)
	require.Equal(t, 10, def.MaxTurns)

	// Zero is valid (defer to config default).
	def, err = Parse([]byte("---\nname: x\ndescription: y\n---\nbody"))
	require.NoError(t, err)
	require.Equal(t, 0, def.MaxTurns)
}

func TestParse_MaxTurnsRejected(t *testing.T) {
	_, err := Parse([]byte("---\nname: x\ndescription: y\nmax-turns: -1\n---\nbody"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "max-turns")

	_, err = Parse([]byte("---\nname: x\ndescription: y\nmax-turns: 0\n---\nbody"))
	require.NoError(t, err) // 0 is valid - defers to config
}

func TestParse_TimeoutValid(t *testing.T) {
	def, err := Parse([]byte("---\nname: x\ndescription: y\ntimeout: 5m\n---\nbody"))
	require.NoError(t, err)
	require.Equal(t, 5*time.Minute, def.Timeout)

	def, err = Parse([]byte("---\nname: x\ndescription: y\ntimeout: 1h30m\n---\nbody"))
	require.NoError(t, err)
	require.Equal(t, 90*time.Minute, def.Timeout)

	// Unset is valid.
	def, err = Parse([]byte("---\nname: x\ndescription: y\n---\nbody"))
	require.NoError(t, err)
	require.Equal(t, time.Duration(0), def.Timeout)
}

func TestParse_TimeoutRejected(t *testing.T) {
	_, err := Parse([]byte("---\nname: x\ndescription: y\ntimeout: not-a-duration\n---\nbody"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "timeout")

	_, err = Parse([]byte("---\nname: x\ndescription: y\ntimeout: -5m\n---\nbody"))
	require.Error(t, err)

	_, err = Parse([]byte("---\nname: x\ndescription: y\ntimeout: 0s\n---\nbody"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "positive")
}

func TestParse_AllNewFieldsTogether(t *testing.T) {
	def, err := Parse([]byte("---\nname: test-agent\ndescription: y\nprovider: deepseek\nmodel: deepseek-v3\nmax-turns: 25\ntimeout: 15m\n---\nbody"))
	require.NoError(t, err)
	require.Equal(t, "deepseek", def.Provider)
	require.Equal(t, "deepseek-v3", def.Model)
	require.Equal(t, 25, def.MaxTurns)
	require.Equal(t, 15*time.Minute, def.Timeout)
}
