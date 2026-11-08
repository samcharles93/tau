package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/store"
)

// saveOneMessageSession persists a session with a single user message.
func saveOneMessageSession(t *testing.T, s *store.SQLiteStore, id string) {
	t.Helper()
	state := chatSessionFixture(id, "model", "prov")
	state.Messages = []chat.ChatMessage{{Role: chat.ChatRoleUser, Content: "hi"}}
	require.NoError(t, s.Save(context.Background(), state, 0))
}

// assertNoTempFile asserts the atomic-write temp file was cleaned up.
func assertNoTempFile(t *testing.T, outputPath string) {
	t.Helper()
	_, statErr := os.Stat(outputPath + ".tmp")
	assert.True(t, os.IsNotExist(statErr), "temp file should be cleaned up")
}

// An empty session (zero messages) must export successfully as an empty file.
func TestExportSessionAsJSONL_EmptySession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	state := chatSessionFixture("empty-export", "model", "prov")
	require.NoError(t, s.Save(ctx, state, 0))

	outputPath := filepath.Join(t.TempDir(), "export.jsonl")
	require.NoError(t, store.ExportSessionAsJSONL(ctx, s, "empty-export", outputPath))

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Empty(t, data, "an empty session must export as an empty file")
	assertNoTempFile(t, outputPath)
}

// A session with many messages exports one newline-terminated JSON line per
// message, in order.
func TestExportSessionAsJSONL_ManyMessages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	const total = 200
	state := chatSessionFixture("many-export", "model", "prov")
	for i := range total {
		state.Messages = append(state.Messages, chat.ChatMessage{
			Role:    chat.ChatRoleUser,
			Content: fmt.Sprintf("message %d", i),
		})
	}
	require.NoError(t, s.Save(ctx, state, 0))

	outputPath := filepath.Join(t.TempDir(), "export.jsonl")
	require.NoError(t, store.ExportSessionAsJSONL(ctx, s, "many-export", outputPath))

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(string(data), "\n"), "every line must be newline-terminated")

	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	require.Len(t, lines, total)
	for i, line := range lines {
		var decoded map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &decoded), "line %d must be valid JSON", i)
		assert.Equal(t, "user", decoded["role"])
		assert.Equal(t, fmt.Sprintf("message %d", i), decoded["content"])
	}
	assertNoTempFile(t, outputPath)
}

// When the final rename fails (here: outputPath is an existing directory),
// the temp file must be removed and the original target left untouched.
func TestExportSessionAsJSONL_RenameFailureCleansTemp(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	saveOneMessageSession(t, s, "rename-export")

	outputPath := filepath.Join(t.TempDir(), "export.jsonl")
	require.NoError(t, os.Mkdir(outputPath, 0o755)) // rename(file, dir) fails

	err := store.ExportSessionAsJSONL(ctx, s, "rename-export", outputPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rename temp jsonl")

	_, statErr := os.Stat(outputPath + ".tmp")
	assert.True(t, os.IsNotExist(statErr), "temp file should be cleaned up after rename failure")

	info, statErr := os.Stat(outputPath)
	require.NoError(t, statErr)
	assert.True(t, info.IsDir(), "the pre-existing directory must be left untouched")
}

// When the temp file cannot be created because the directory is read-only,
// the export must fail with the create error and write nothing.
func TestExportSessionAsJSONL_ReadOnlyDir(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	saveOneMessageSession(t, s, "ro-export")

	roDir := t.TempDir()
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o700) }) // let t.TempDir clean up
	require.NoError(t, os.Chmod(roDir, 0o555))

	outputPath := filepath.Join(roDir, "export.jsonl")

	// Permission checks are not enforced for privileged users (e.g. root);
	// probe with a real create and skip when it unexpectedly succeeds.
	probe, probeErr := os.Create(outputPath)
	if probeErr == nil {
		_ = probe.Close()
		_ = os.Remove(outputPath)
		t.Log("file permission checks are not enforced for this user (e.g. root); nothing to assert")
		return
	}

	err := store.ExportSessionAsJSONL(ctx, s, "ro-export", outputPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create temp jsonl")

	_, statErr := os.Stat(outputPath)
	assert.True(t, os.IsNotExist(statErr), "no output file should be written into a read-only directory")
}

// A missing parent directory makes temp file creation fail (ENOENT) - the
// uid-independent variant of the create-failure path.
func TestExportSessionAsJSONL_MissingParentDir(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	saveOneMessageSession(t, s, "noparent-export")

	outputPath := filepath.Join(t.TempDir(), "no-such-dir", "export.jsonl")

	err := store.ExportSessionAsJSONL(ctx, s, "noparent-export", outputPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create temp jsonl")

	_, statErr := os.Stat(outputPath)
	assert.True(t, os.IsNotExist(statErr))
}

// A cancelled context must abort the export before streaming and leave no
// output or temp file behind.
func TestExportSessionAsJSONL_ContextCancelled(t *testing.T) {
	s := newTestStore(t)
	saveOneMessageSession(t, s, "cancel-export")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	outputPath := filepath.Join(t.TempDir(), "export.jsonl")
	err := store.ExportSessionAsJSONL(ctx, s, "cancel-export", outputPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")

	_, statErr := os.Stat(outputPath)
	assert.True(t, os.IsNotExist(statErr), "no output file should exist after cancellation")
	_, statErr = os.Stat(outputPath + ".tmp")
	assert.True(t, os.IsNotExist(statErr), "temp file should be cleaned up after cancellation")
}
