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

func TestToPromptIndex_CompactFormat(t *testing.T) {
	t.Parallel()

	index := ToPromptIndex([]*Skill{
		{Name: "go-dev", Description: "Develop Go code", SkillFilePath: "/skills/go-dev/SKILL.md"},
		{Name: "web-search", Description: "Search the web", SkillFilePath: "/skills/web-search/SKILL.md"},
	})

	require.Contains(t, index, "<available_skills>")
	require.Contains(t, index, "- go-dev: Develop Go code (/skills/go-dev/SKILL.md)")
	require.Contains(t, index, "- web-search: Search the web (/skills/web-search/SKILL.md)")
	require.NotContains(t, index, "<skill>")
}

func TestToPromptIndex_IncludesCompatibility(t *testing.T) {
	t.Parallel()

	index := ToPromptIndex([]*Skill{
		{Name: "cuda-kernels", Description: "Write CUDA kernels", SkillFilePath: "/skills/cuda/SKILL.md", Compatibility: "Requires CUDA toolkit 12.x and an NVIDIA GPU"},
		{Name: "no-compat", Description: "No restrictions", SkillFilePath: "/skills/no/SKILL.md"},
	})

	// Entry with compatibility should include the [compat: ...] suffix.
	require.Contains(t, index, "- cuda-kernels: Write CUDA kernels (/skills/cuda/SKILL.md) [compat: Requires CUDA toolkit 12.x and an NVIDIA GPU]")
	// Entry without compatibility should not have the suffix.
	require.Contains(t, index, "- no-compat: No restrictions (/skills/no/SKILL.md)")
	// Only one [compat:] tag should appear.
	require.Equal(t, 1, strings.Count(index, "[compat:"))
}

func TestToPromptIndex_SkipsDisabled(t *testing.T) {
	t.Parallel()

	index := ToPromptIndex([]*Skill{
		{Name: "visible", Description: "I am seen", SkillFilePath: "/skills/visible/SKILL.md"},
		{Name: "hidden", Description: "I am not", SkillFilePath: "/skills/hidden/SKILL.md", DisableModelInvocation: true},
	})

	require.Contains(t, index, "visible")
	require.NotContains(t, index, "hidden")
}

func TestToPromptIndex_Empty(t *testing.T) {
	t.Parallel()

	require.Empty(t, ToPromptIndex(nil))
	require.Empty(t, ToPromptIndex([]*Skill{}))
}

func TestToPromptIndex_IncludesBundledResources(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-skill")
	require.NoError(t, os.MkdirAll(filepath.Join(skillDir, "scripts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "scripts", "run.sh"), []byte("#!/bin/bash"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "scripts", "validate.py"), []byte("# python"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(skillDir, "references"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "references", "guide.md"), []byte("# guide"), 0o644))
	// assets/ is empty, should not appear.
	require.NoError(t, os.MkdirAll(filepath.Join(skillDir, "assets"), 0o755))

	index := ToPromptIndex([]*Skill{
		{Name: "my-skill", Description: "Has scripts", SkillFilePath: filepath.Join(skillDir, "SKILL.md"), Path: skillDir},
	})

	require.Contains(t, index, "[bundled: scripts:2, references:1]")
	require.NotContains(t, index, "assets")
}

// Test that if bundled resources are present but empty, the [bundled:] section is omitted entirely rather than showing empty categories.
func TestToPromptIndex_OmitsBundledWhenEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	skillDir := filepath.Join(dir, "lean-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))

	index := ToPromptIndex([]*Skill{
		{Name: "lean-skill", Description: "Just instructions", SkillFilePath: filepath.Join(skillDir, "SKILL.md"), Path: skillDir},
	})

	require.NotContains(t, index, "[bundled:")
}

// Test that the discovery process correctly identifies skill files regardless of the case of "SKILL.md".
func TestDiscover_CaseInsensitiveSkillMD(t *testing.T) {
	t.Parallel()

	cases := []string{"SKILL.md", "Skill.md", "sKill.md", "skill.md"}
	for _, filename := range cases {
		t.Run(filename, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			skillDir := filepath.Join(root, "test-skill")
			require.NoError(t, os.MkdirAll(skillDir, 0o755))
			path := filepath.Join(skillDir, filename)
			require.NoError(t, os.WriteFile(path, []byte("---\nname: test-skill\ndescription: A test\n---\n"), 0o644))

			skills, diagnostics := Discover([]Source{{Root: root, Scope: ScopeUser, Priority: userNativePriority}})
			require.False(t, HasErrors(diagnostics))
			require.Len(t, skills, 1)
			require.Equal(t, "test-skill", skills[0].Name)
		})
	}
}

// Test that files with names that contain "skill.md" but aren't exactly "SKILL.md" (case-insensitive) are not mistakenly identified as skill files.
func TestDiscover_RejectsSimilarFilenames(t *testing.T) {
	t.Parallel()

	// Names that *contain* "skill.md" but aren't the skill file must
	// be rejected (anchored regex).
	bad := []string{"not-skill.md", "skill.md.bak", "my-skill.md", "skill.mdx"}
	for _, filename := range bad {
		t.Run(filename, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			skillDir := filepath.Join(root, "real-skill")
			require.NoError(t, os.MkdirAll(skillDir, 0o755))
			path := filepath.Join(skillDir, filename)
			require.NoError(t, os.WriteFile(path, []byte("---\nname: not-a-skill\ndescription: bad\n---\n"), 0o644))

			skills, _ := Discover([]Source{{Root: root, Scope: ScopeUser, Priority: userNativePriority}})
			require.Empty(t, skills, "expected %q not to be detected as a skill file", filename)
		})
	}
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
