package skills

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManagerRefreshPublishesSnapshot(t *testing.T) {
	configDir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("TAU_CONFIG_DIR", configDir)
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	workingDir := t.TempDir()

	writeSkillFile(t, filepath.Join(configDir, "skills"), "pdf-processing", `---
name: pdf-processing
description: Extract PDF text. Use when the user mentions PDFs.
user-invocable: true
---

PDF instructions.
`)
	writeSkillFile(t, filepath.Join(workingDir, ".tau", "skills"), "code-review", `---
name: code-review
description: Review code. Use when the user asks for a review.
---

Review instructions.
`)

	manager := NewManager()
	defer manager.Close()

	subscription, err := manager.Subscribe(1)
	require.NoError(t, err)
	defer subscription.Unsubscribe()

	snapshot, err := manager.Refresh(DiscoveryConfig{WorkingDir: workingDir, DisabledSkills: []string{"code-review"}})
	require.NoError(t, err)
	require.Len(t, snapshot.AllSkills, 2)
	require.Len(t, snapshot.ActiveSkills, 1)
	require.Equal(t, "pdf-processing", snapshot.ActiveSkills[0].Name)

	event, ok := <-subscription.Channel()
	require.True(t, ok)
	require.Len(t, event.ActiveSkills, 1)
}
