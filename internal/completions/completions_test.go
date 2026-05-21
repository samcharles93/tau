package completions

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/require"
)

func newTestCompletions() *Completions {
	c := New(lipgloss.NewStyle(), lipgloss.NewStyle(), lipgloss.NewStyle())
	c.SetCommands([]CommandDef{
		{Name: "/new", Description: "Start a new conversation"},
		{Name: "/system", Description: "Set system prompt", AcceptsArgs: true},
		{Name: "/model", Description: "Switch model", AcceptsArgs: true},
		{Name: "/exit", Description: "Quit the app"},
		{Name: "/quit", Description: "Quit the app"},
	})
	c.SetArgumentCompletions("/model", []string{
		"nemotron-nano-9b",
		"llama-3.1-70b",
		"granite-13b",
	})
	return c
}

func TestSync_OpensOnSlash(t *testing.T) {
	t.Parallel()
	c := newTestCompletions()

	c.Sync("/")
	require.True(t, c.IsOpen())
}

func TestSync_ClosesOnNonSlash(t *testing.T) {
	t.Parallel()
	c := newTestCompletions()

	c.Sync("/")
	require.True(t, c.IsOpen())

	c.Sync("hello")
	require.False(t, c.IsOpen())
}

func TestSync_ClosesWhenArgsTypedForNonArgCommand(t *testing.T) {
	t.Parallel()
	c := newTestCompletions()

	c.Sync("/system")
	require.True(t, c.IsOpen())

	// /system has AcceptsArgs but no registered arg completions.
	c.Sync("/system hello")
	require.False(t, c.IsOpen())
}

func TestSync_OpensArgCompletionsForModel(t *testing.T) {
	t.Parallel()
	c := newTestCompletions()

	c.Sync("/model ")
	require.True(t, c.IsOpen())
	require.Equal(t, 3, c.Height())
}

func TestSync_FiltersArgCompletions(t *testing.T) {
	t.Parallel()
	c := newTestCompletions()

	c.Sync("/model nem")
	require.True(t, c.IsOpen())
	require.Equal(t, 1, c.Height())

	sel, ok := c.Selected()
	require.True(t, ok)
	require.Equal(t, "/model", sel.Name)
}

func TestSync_ArgSelectionReturnsFullCommand(t *testing.T) {
	t.Parallel()
	c := newTestCompletions()

	c.Sync("/model ")

	msg, consumed := c.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	require.True(t, consumed)

	selMsg, ok := msg.(SelectionMsg)
	require.True(t, ok)
	require.True(t, selMsg.IsArg)
	require.Equal(t, "/model nemotron-nano-9b", selMsg.Text)
}

func TestFilter_PrefixMatch(t *testing.T) {
	t.Parallel()
	c := newTestCompletions()

	c.Sync("/ne")
	require.True(t, c.IsOpen())

	sel, ok := c.Selected()
	require.True(t, ok)
	require.Equal(t, "/new", sel.Name)
}

func TestFilter_SubstringMatch(t *testing.T) {
	t.Parallel()
	c := newTestCompletions()

	c.Sync("/ys") // matches "/system" via contains
	require.True(t, c.IsOpen())

	sel, ok := c.Selected()
	require.True(t, ok)
	require.Equal(t, "/system", sel.Name)
}

func TestFilter_NoMatch(t *testing.T) {
	t.Parallel()
	c := newTestCompletions()

	c.Sync("/zzz")
	require.True(t, c.IsOpen())

	_, ok := c.Selected()
	require.False(t, ok)
}

func TestUpdate_ArrowNavigation(t *testing.T) {
	t.Parallel()
	c := newTestCompletions()
	c.Sync("/")

	// Move down from first item.
	msg, consumed := c.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	require.True(t, consumed)
	require.Nil(t, msg)

	sel, ok := c.Selected()
	require.True(t, ok)
	require.Equal(t, "/system", sel.Name) // second item
}

func TestUpdate_SelectReturnsMsg(t *testing.T) {
	t.Parallel()
	c := newTestCompletions()
	c.Sync("/")

	msg, consumed := c.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	require.True(t, consumed)

	selMsg, ok := msg.(SelectionMsg)
	require.True(t, ok)
	require.Equal(t, "/new", selMsg.Text)
	require.False(t, selMsg.IsArg)
	require.False(t, c.IsOpen())
}

func TestUpdate_EscapeCloses(t *testing.T) {
	t.Parallel()
	c := newTestCompletions()
	c.Sync("/")

	msg, consumed := c.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	require.True(t, consumed)
	require.IsType(t, ClosedMsg{}, msg)
	require.False(t, c.IsOpen())
}

func TestUpdate_WrapAround(t *testing.T) {
	t.Parallel()
	c := newTestCompletions()
	c.Sync("/")

	// Move up from first item should wrap to last.
	c.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	sel, ok := c.Selected()
	require.True(t, ok)
	require.Equal(t, "/quit", sel.Name) // last item
}

func TestRender_NotOpenReturnsEmpty(t *testing.T) {
	t.Parallel()
	c := newTestCompletions()

	require.Empty(t, c.Render(80))
}

func TestRender_ShowsItems(t *testing.T) {
	t.Parallel()
	c := newTestCompletions()
	c.Sync("/")

	output := c.Render(40)
	require.NotEmpty(t, output)
	require.Contains(t, output, "/new")
	require.Contains(t, output, "/system")
}

func TestHeight_ReturnsItemCount(t *testing.T) {
	t.Parallel()
	c := newTestCompletions()

	require.Equal(t, 0, c.Height())

	c.Sync("/")
	require.Equal(t, 5, c.Height())
}

func TestFilter_ClampsSelection(t *testing.T) {
	t.Parallel()
	c := newTestCompletions()
	c.Sync("/")

	// Select last item (index 4).
	for range 4 {
		c.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	sel, _ := c.Selected()
	require.Equal(t, "/quit", sel.Name)

	// Now filter to only 1 result — selection must clamp.
	c.Sync("/ne")
	sel, ok := c.Selected()
	require.True(t, ok)
	require.Equal(t, "/new", sel.Name)
}

func TestSync_BackspaceFromArgToCommandMode(t *testing.T) {
	t.Parallel()
	c := newTestCompletions()

	// Enter arg mode.
	c.Sync("/model ")
	require.True(t, c.IsOpen())
	require.Equal(t, 3, c.Height()) // shows model args

	// Backspace removes the space — back to command mode.
	c.Sync("/model")
	require.True(t, c.IsOpen())

	sel, ok := c.Selected()
	require.True(t, ok)
	require.Equal(t, "/model", sel.Name) // back to command completions
}

