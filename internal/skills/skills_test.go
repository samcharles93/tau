package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestValidateSkill_LongInstructions(t *testing.T) {
	tempDir := t.TempDir()
	skillDir := filepath.Join(tempDir, "test-skill")
	err := os.MkdirAll(filepath.Join(skillDir, "scripts"), 0o755)
	require.NoError(t, err)

	// Create a SKILL.md with instructions exceeding MaxInstructionsLength
	longInstructions := strings.Repeat("a", MaxInstructionsLength+1)
	skillContent := fmt.Sprintf(`---
name: test-long-instructions
description: Test skill with overly long instructions
compatibility: tau
---

# Test Long Instructions

%s`, longInstructions)

	err = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644)
	require.NoError(t, err)

	// Validate the skill
	skill, diagnostics, err := Parse(filepath.Join(skillDir, "SKILL.md"))
	require.Error(t, err)
	require.Nil(t, skill)

	// Check for the expected error (there may be additional warnings)
	require.GreaterOrEqual(t, len(diagnostics), 1)
	found := false
	for _, diag := range diagnostics {
		if diag.Severity == SeverityError && strings.Contains(diag.Message, fmt.Sprintf("instructions exceed maximum length of %d characters", MaxInstructionsLength)) {
			found = true
			break
		}
	}
	require.True(t, found, "Expected error about instructions length not found in diagnostics")
}

func TestQuoteColonValues(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter string
		want        string
	}{
		{
			name: "unquoted colon value is quoted",
			frontmatter: `name: a
description: Handles X: Y`,
			want: `name: a
description: "Handles X: Y"`,
		},
		{
			name: "value without colon is untouched",
			frontmatter: `name: a
description: plain value`,
			want: `name: a
description: plain value`,
		},
		{
			name:        "empty value (nested key) is untouched",
			frontmatter: "name: a\nmetadata:",
			want:        "name: a\nmetadata:",
		},
		{
			name:        "already double-quoted value is untouched",
			frontmatter: `description: "already: quoted"`,
			want:        `description: "already: quoted"`,
		},
		{
			name:        "single-quoted value with escaped quote is untouched",
			frontmatter: `description: 'it''s: fine'`,
			want:        `description: 'it''s: fine'`,
		},
		{
			name:        "double quotes inside value are escaped",
			frontmatter: `description: say "hi": now`,
			want:        `description: "say \"hi\": now"`,
		},
		{
			name:        "metadata value with colon is left alone",
			frontmatter: `metadata: a: b`,
			want:        `metadata: a: b`,
		},
		{
			name: "comment lines are untouched",
			frontmatter: `# a comment: with colon
name: a`,
			want: `# a comment: with colon
name: a`,
		},
		{
			name: "literal block scalar continuations are not quoted",
			frontmatter: `name: blocky
description: |
  Line one
  see http://example.com:8080 for details
when_to_use: Use when X: Y`,
			want: `name: blocky
description: |
  Line one
  see http://example.com:8080 for details
when_to_use: "Use when X: Y"`,
		},
		{
			name: "folded block scalar with chomping indicator",
			frontmatter: `name: a
description: >-
  folded: one
  two: 2
compatibility: tau: linux`,
			want: `name: a
description: >-
  folded: one
  two: 2
compatibility: "tau: linux"`,
		},
		{
			name: "block scalar with indentation indicator",
			frontmatter: `name: a
description: |2
   indented: content
other: x: y`,
			want: `name: a
description: |2
   indented: content
other: "x: y"`,
		},
		{
			name: "comment-looking line inside block scalar is content",
			frontmatter: `name: a
description: |
  # not a comment: really
  text
other: x: y`,
			want: `name: a
description: |
  # not a comment: really
  text
other: "x: y"`,
		},
		{
			name: "nested mapping after block scalar still gets quoted",
			frontmatter: `name: a
description: |
  text: here
metadata:
  note: v: x`,
			want: `name: a
description: |
  text: here
metadata:
  note: "v: x"`,
		},
		{
			name: "block scalar under a sequence item",
			frontmatter: `name: a
examples:
  - |
    item: text: here
other: x: y`,
			want: `name: a
examples:
  - |
    item: text: here
other: "x: y"`,
		},
		{
			name: "multi-line double-quoted scalar continuation is untouched",
			frontmatter: `name: a
description: "first
  second: part"
other: x: y`,
			want: `name: a
description: "first
  second: part"
other: "x: y"`,
		},
		{
			name: "plain multi-line scalar continuation is untouched",
			frontmatter: `name: a
description: first line
  second: part
other: x`,
			want: `name: a
description: first line
  second: part
other: x`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, quoteColonValues(tt.frontmatter))
		})
	}
}

func TestUnmarshalFrontmatter_BlockScalarRoundTrip(t *testing.T) {
	tests := []struct {
		name            string
		frontmatter     string
		wantDescription string
	}{
		{
			name: "literal block scalar with colon continuations",
			frontmatter: `name: blocky
description: |
  Line one
  see http://example.com:8080 for details
when_to_use: Use when X: Y`,
			wantDescription: "Line one\nsee http://example.com:8080 for details\n",
		},
		{
			name: "folded block scalar with chomping",
			frontmatter: `name: foldy
description: >-
  folded one
  two: 2
compatibility: tau: linux`,
			wantDescription: "folded one two: 2",
		},
		{
			name: "literal block scalar with strip chomping",
			frontmatter: `name: chompy
description: |-
  literal: keep
  me: here
note: a: b`,
			wantDescription: "literal: keep\nme: here",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var skill Skill
			require.NoError(t, UnmarshalFrontmatter(tt.frontmatter, &skill))
			require.Equal(t, tt.wantDescription, skill.Description)

			// The normalized frontmatter must itself be valid YAML that
			// round-trips to the same value.
			normalized := quoteColonValues(tt.frontmatter)
			var reparsed Skill
			require.NoError(t, yaml.Unmarshal([]byte(normalized), &reparsed))
			require.Equal(t, tt.wantDescription, reparsed.Description)
		})
	}
}

func TestParse_SkillFileWithBlockScalarFrontmatter(t *testing.T) {
	tempDir := t.TempDir()
	skillDir := filepath.Join(tempDir, "blocky")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))

	skillContent := `---
name: blocky
description: |
  Line one
  see http://example.com:8080 for details
when_to_use: Use when X: Y
---

Body instructions.
`
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644))

	skill, diagnostics, err := Parse(filepath.Join(skillDir, "SKILL.md"))
	require.NoError(t, err)
	require.False(t, HasErrors(diagnostics))
	require.NotNil(t, skill)
	require.Equal(t, "blocky", skill.Name)
	require.Equal(t, "Line one\nsee http://example.com:8080 for details\n", skill.Description)
	require.Equal(t, "Body instructions.", skill.Instructions)
}
