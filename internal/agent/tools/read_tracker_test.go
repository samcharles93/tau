package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/samcharles93/tau/internal/agent/tools"
)

// Read-before-write enforcement: CheckRead must fail until MarkRead records
// the same file (tau-v1g).
func TestReadTracker_CheckReadEnforcesReadBeforeWrite(t *testing.T) {
	rt := tools.NewReadTracker()
	cwd := t.TempDir()

	err := rt.CheckRead(cwd, "foo.txt")
	require.Error(t, err)
	require.Contains(t, err.Error(), `file "foo.txt" has not been read in this session`)

	rt.MarkRead(cwd, "foo.txt")
	require.NoError(t, rt.CheckRead(cwd, "foo.txt"))
}

// Path normalization: MarkRead and CheckRead resolve their argument to the
// same absolute key, so equivalent spellings of one file are interchangeable.
func TestReadTracker_PathNormalization(t *testing.T) {
	cwd := t.TempDir()
	abs := filepath.Join(cwd, "foo.txt")
	nested := filepath.Join("sub", "dir", "f.txt")

	tests := []struct {
		name      string
		mark      string
		check     string
		wantError bool
	}{
		{"identical relative", "foo.txt", "foo.txt", false},
		{"dot prefix normalized", "./foo.txt", "foo.txt", false},
		{"redundant segments cleaned", "sub/../foo.txt", "foo.txt", false},
		{"double slash cleaned", "dir//f.txt", "dir/f.txt", false},
		{"absolute marked, relative checked", abs, "foo.txt", false},
		{"relative marked, absolute checked", "foo.txt", abs, false},
		{"leading @ stripped", "@foo.txt", "foo.txt", false},
		{"nested path dot prefix", "./" + nested, nested, false},
		{"empty path is the cwd itself", "", "", false},
		{"empty path vs file differs", "", "foo.txt", true},
		{"case is significant", "Foo.txt", "foo.txt", true},
		{"different file not covered", "foo.txt", "bar.txt", true},
		{"directory is not its file", "dir", "dir/f.txt", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := tools.NewReadTracker()
			rt.MarkRead(cwd, tt.mark)
			err := rt.CheckRead(cwd, tt.check)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Per-cwd scoping: a read recorded under one working directory must not
// satisfy a check under another, unless both resolve to the same absolute
// path.
func TestReadTracker_PerCWDScoping(t *testing.T) {
	cwdA := t.TempDir()
	cwdB := t.TempDir()
	nested := filepath.Join(cwdA, "nested")
	require.NoError(t, os.Mkdir(nested, 0o755))

	rt := tools.NewReadTracker()
	rt.MarkRead(cwdA, "foo.txt")

	require.NoError(t, rt.CheckRead(cwdA, "foo.txt"))
	require.Error(t, rt.CheckRead(cwdB, "foo.txt"),
		"the same relative path under a different cwd must not count as read")

	rt.MarkRead(cwdA, filepath.Join("nested", "bar.txt"))
	require.NoError(t, rt.CheckRead(nested, "bar.txt"),
		"a relative path under a subdirectory must resolve to the same absolute file")
	require.Error(t, rt.CheckRead(cwdB, "bar.txt"))
}

// The CheckRead error message carries the display path relative to the cwd.
func TestReadTracker_CheckReadErrorUsesRelativePath(t *testing.T) {
	rt := tools.NewReadTracker()
	cwd := t.TempDir()
	nested := filepath.Join("sub", "dir", "f.txt")

	err := rt.CheckRead(cwd, nested)
	require.Error(t, err)
	require.Contains(t, err.Error(),
		fmt.Sprintf("file %q has not been read in this session", nested))

	// A path outside the cwd is shown with .. segments, not as absolute.
	err = rt.CheckRead(cwd, "../outside.txt")
	require.Error(t, err)
	require.Contains(t, err.Error(), `"../outside.txt"`)
	require.NotContains(t, err.Error(), cwd)

	// An empty path resolves to the cwd itself; filepath.Rel renders that as ".".
	err = rt.CheckRead(cwd, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), `file "." has not been read`)
}

// Concurrent MarkRead/CheckRead must be race-free (run with -race) and every
// marked path must be checkable afterwards.
func TestReadTracker_ConcurrentMarkAndCheck(t *testing.T) {
	rt := tools.NewReadTracker()
	cwd := t.TempDir()
	paths := []string{"a.txt", "b.txt", filepath.Join("sub", "c.txt"), "./d.txt", "@e.txt"}

	var wg sync.WaitGroup
	for i := range 64 {
		wg.Go(func() {
			p := paths[i%len(paths)]
			rt.MarkRead(cwd, p)
			// Whether this check passes depends on scheduling; it must
			// simply never race or panic.
			_ = rt.CheckRead(cwd, p)
		})
	}
	wg.Wait()

	for _, p := range paths {
		require.NoError(t, rt.CheckRead(cwd, p), "path %q marked concurrently must be readable", p)
	}
	require.Error(t, rt.CheckRead(cwd, "never-marked.txt"))
}

// Tools receive a nil *ReadTracker when read tracking is disabled; the
// read-before-write guard must then be skipped, not panic. With a live
// tracker the same write is blocked until the file is read.
func TestReadTracker_NilTrackerSkipsGuardInWriteTool(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "f.txt")
	require.NoError(t, os.WriteFile(target, []byte("old\n"), 0o644))

	write := func(tool tools.Tool, content string) tools.Result {
		t.Helper()
		params := fmt.Sprintf(`{"path":"f.txt","content":%q,"overwrite":true}`, content)
		res, err := tool.Execute(context.Background(), json.RawMessage(params), nil)
		require.NoError(t, err)
		return res
	}

	// Nil tracker: the existing, unread file is writable (guard skipped).
	res := write(tools.NewWriteTool(tmp, tools.NewMutationQueue(), nil), "new\n")
	require.False(t, res.IsError, "nil ReadTracker must skip the read-before-write check, got: %s", res.Content)

	// Live tracker: same write is blocked because the file was never read.
	rt := tools.NewReadTracker()
	tool := tools.NewWriteTool(tmp, tools.NewMutationQueue(), rt)
	res = write(tool, "blocked\n")
	require.True(t, res.IsError)
	require.Contains(t, res.Content, "has not been read in this session")

	// After MarkRead the write goes through and lands on disk.
	rt.MarkRead(tmp, "f.txt")
	res = write(tool, "new3\n")
	require.False(t, res.IsError, "got: %s", res.Content)
	content, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "new3\n", string(content))
}
