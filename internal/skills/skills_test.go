package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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
