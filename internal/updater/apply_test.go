package updater

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyUpdateReplacesBinary(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "tau")
	require.NoError(t, os.WriteFile(target, []byte("old-binary"), 0o755))

	newBinary := []byte("new-binary-content")
	require.NoError(t, applyUpdate(newBinary, target))

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, newBinary, got)

	// No staging or backup files may be left behind.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "only the target binary should remain in the directory")
}

func TestApplyUpdateMakesBinaryExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not meaningful on Windows")
	}
	t.Parallel()

	target := filepath.Join(t.TempDir(), "tau")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o644))

	require.NoError(t, applyUpdate([]byte("new"), target))

	info, err := os.Stat(target)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o755), info.Mode().Perm(), "replaced binary must be executable")
}

func TestApplyUpdateMissingParentDirFailsCleanly(t *testing.T) {
	t.Parallel()

	target := filepath.Join(t.TempDir(), "missing", "tau")
	err := applyUpdate([]byte("new"), target)
	require.Error(t, err, "staging write into a missing directory must fail")
	require.NoFileExists(t, target)
}

func TestApplyUpdateRenameFailureLeavesTargetAndCleansStaging(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "tau")
	// A directory at the target path forces the final rename to fail.
	require.NoError(t, os.Mkdir(target, 0o755))

	err := applyUpdate([]byte("new"), target)
	require.Error(t, err, "renaming a file over a directory must fail")

	require.DirExists(t, target, "the target directory must be untouched")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "the staging file must have been cleaned up")
}
