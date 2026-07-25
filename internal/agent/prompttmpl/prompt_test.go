package prompttmpl

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRenderSpecUsesSharedTemplateContract(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)

	data := NewData("/work/a<&b", "gpt-test", "session-1", now)
	got := RenderSpec("task", strings.Join([]string{
		"cwd={{.WorkingDir}}",
		"platform={{.Platform}}",
		"shell={{.Shell}}",
		"date={{.Date}}",
		"git={{.IsGitRepo}}",
		"model={{.ModelName}}",
		"session={{.SessionID}}",
		"escaped={{xml .WorkingDir}}",
	}, "\n"), data)

	require.Contains(t, got, "cwd=/work/a<&b")
	require.Contains(t, got, "platform="+runtime.GOOS)
	require.Contains(t, got, "shell=zsh")
	require.Contains(t, got, "date=2026-07-24")
	require.Contains(t, got, "git=false")
	require.Contains(t, got, "model=gpt-test")
	require.Contains(t, got, "session=session-1")
	require.Contains(t, got, "escaped=/work/a&lt;&amp;b")
}

func TestRenderSpecPreservesSourceOnTemplateErrors(t *testing.T) {
	source := "{{.MissingField}}"
	got := RenderSpec("broken", source, Data{})
	require.Contains(t, got, "<!-- prompt template error:")
	require.True(t, strings.HasSuffix(got, source))
}

func TestNewDataIncludesWorkspaceTreeForEverySpecBody(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docs", "agent.md"), []byte("context"), 0o600))

	got := RenderSpec("task", "workspace:\n{{.WorkspaceTree}}", NewData(dir, "", "", time.Now()))
	require.Contains(t, got, "docs/")
	require.Contains(t, got, "agent.md")
	require.NotContains(t, got, "prompt template error")
}
