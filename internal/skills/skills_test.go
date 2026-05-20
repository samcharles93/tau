package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSkill(t *testing.T) {
	t.Parallel()

	skillFile := writeSkillFile(t, t.TempDir(), "pdf-processing", `---
name: pdf-processing
description: Extract PDF text and forms. Use when the user mentions PDFs.
user-invocable: true
allowed-tools: Read Bash(pdftotext:*)
---

Follow the PDF workflow.
`)

	skill, diagnostics, err := Parse(skillFile)
	require.NoError(t, err)
	require.False(t, HasErrors(diagnostics))
	require.Equal(t, "pdf-processing", skill.Name)
	require.True(t, skill.UserInvocable)
	require.Equal(t, "Follow the PDF workflow.", skill.Instructions)
	require.Equal(t, filepath.Dir(skillFile), skill.Path)
}

func TestDiscoverHonorsPriority(t *testing.T) {
	t.Parallel()

	userRoot := t.TempDir()
	projectRoot := t.TempDir()
	writeSkillFile(t, userRoot, "code-review", `---
name: code-review
description: Review code for bugs. Use when the user asks for review.
---

User version.
`)
	writeSkillFile(t, projectRoot, "code-review", `---
name: code-review
description: Review code for bugs. Use when the user asks for review.
---

Project version.
`)

	skillSet, diagnostics := Discover([]Source{
		{Root: userRoot, Scope: ScopeUser, Priority: userInteropPriority},
		{Root: projectRoot, Scope: ScopeProject, Priority: projectNativePriority},
	})

	require.Len(t, skillSet, 1)
	require.Equal(t, "Project version.", skillSet[0].Instructions)
	require.True(t, containsDiagnosticMessage(diagnostics, "shadowed by"))
}

func TestDiscoverRejectsMissingDescription(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSkillFile(t, root, "bad-skill", `---
name: bad-skill
---

This should not load.
`)

	skillSet, diagnostics := Discover([]Source{{Root: root, Scope: ScopeUser, Priority: userNativePriority}})
	require.Empty(t, skillSet)
	require.True(t, containsDiagnosticMessage(diagnostics, "description is required"))
}

func TestToPromptXMLSkipsDisabledModelInvocation(t *testing.T) {
	t.Parallel()

	xml := ToPromptXML([]*Skill{
		{Name: "pdf-processing", Description: "PDF help", SkillFilePath: "/tmp/pdf-processing/SKILL.md"},
		{Name: "secret-skill", Description: "Hidden", SkillFilePath: "/tmp/secret-skill/SKILL.md", DisableModelInvocation: true},
	})

	require.Contains(t, xml, "pdf-processing")
	require.NotContains(t, xml, "secret-skill")
}

func writeSkillFile(t *testing.T, root, name, content string) string {
	t.Helper()

	directory := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(directory, 0o755))
	path := filepath.Join(directory, SkillFileName)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func containsDiagnosticMessage(diagnostics []Diagnostic, fragment string) bool {
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, fragment) {
			return true
		}
	}
	return false
}
