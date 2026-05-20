package commands

import (
	"os"
	"path/filepath"
	"testing"

	"bitbucket.srv.westpac.com.au/m055731/aim/internal/skills"
	"github.com/stretchr/testify/require"
)

func TestLoadFromSource_NonExistentDir(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "does-not-exist")

	cmds, err := loadFromSource(commandSource{path: dir, prefix: userCommandPrefix})
	require.NoError(t, err)
	require.Empty(t, cmds)

	// directory must NOT have been created
	_, statErr := os.Stat(dir)
	require.True(t, os.IsNotExist(statErr))
}

func TestLoadFromSource_ExistingDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hello.md"), []byte("say hello"), 0o644))

	cmds, err := loadFromSource(commandSource{path: dir, prefix: userCommandPrefix})
	require.NoError(t, err)
	require.Len(t, cmds, 1)
	require.Equal(t, "user:hello", cmds[0].ID)
	require.Equal(t, "say hello", cmds[0].Content)
}

func TestLoadAll_MixedSources(t *testing.T) {
	t.Parallel()

	existing := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(existing, "cmd.md"), []byte("content"), 0o644))

	missing := filepath.Join(t.TempDir(), "nope")

	cmds, err := loadAll([]commandSource{
		{path: existing, prefix: userCommandPrefix},
		{path: missing, prefix: projectCommandPrefix},
	})
	require.NoError(t, err)
	require.Len(t, cmds, 1)
	require.Equal(t, "user:cmd", cmds[0].ID)
}

func TestLoadSkillCommands(t *testing.T) {
	configDir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("AIM_CONFIG_DIR", configDir)
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	skillDir := filepath.Join(configDir, "skills", "pdf-processing")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, skills.SkillFileName), []byte(`---
name: pdf-processing
description: Extract PDF text. Use when the user mentions PDFs.
user-invocable: true
---

Use the PDF workflow.
`), 0o644))

	cmds := LoadSkillCommands()
	require.Len(t, cmds, 1)
	require.Equal(t, "user:pdf-processing", cmds[0].ID)
	require.NotNil(t, cmds[0].Skill)
	require.Equal(t, "Use the PDF workflow.", cmds[0].Content)
}

func TestLoadProjectSkillCommands(t *testing.T) {
	t.Parallel()

	workingDir := t.TempDir()
	skillDir := filepath.Join(workingDir, ".aim", "skills", "code-review")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, skills.SkillFileName), []byte(`---
name: code-review
description: Review code. Use when the user asks for a review.
user-invocable: true
---

Use the review workflow.
`), 0o644))

	cmds := LoadProjectSkillCommands(workingDir)
	require.Len(t, cmds, 1)
	require.Equal(t, "project:code-review", cmds[0].ID)
	require.NotNil(t, cmds[0].Skill)
	require.Equal(t, "Use the review workflow.", cmds[0].Content)
}
