package skills

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrackerDeduplicatesActivations(t *testing.T) {
	t.Parallel()

	tracker := NewTracker()
	skill := &Skill{Name: "pdf-processing"}

	tracker.Activate(skill)
	tracker.Activate(skill)

	require.True(t, tracker.IsActivated("pdf-processing"))
	require.Equal(t, []string{"pdf-processing"}, tracker.Activated())

	tracker.Reset()
	require.False(t, tracker.IsActivated("pdf-processing"))
	require.Empty(t, tracker.Activated())
}
