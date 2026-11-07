//go:build !windows

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestShellTimeoutKillsWholeProcessGroup guards tau-iwh: on timeout the shell
// tool must signal the command's entire process group, not just the shell
// process - otherwise background children (sleep, servers, build tools)
// survive the timeout as orphans.
func TestShellTimeoutKillsWholeProcessGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	// Spawn a long-running background child, record its pid, then wait so the
	// shell itself stays alive until the tool's timeout fires. The child's
	// output is redirected so it does not inherit the shell's stdout/stderr
	// pipes - otherwise cmd.Wait blocks on the pipes until the orphan exits
	// and the test cannot observe the orphan.
	cmd := fmt.Sprintf("sleep 300 >/dev/null 2>&1 & echo $! > %s; wait", pidFile)

	tool := NewShellTool(dir, nil)
	params, err := json.Marshal(ShellParams{Command: cmd, Timeout: 1})
	require.NoError(t, err)

	res, err := tool.Execute(context.Background(), params, NonInteractiveBridge{})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Equal(t, "timeout", res.ErrorKind)

	data, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	require.NoError(t, err)

	// The group SIGKILL is delivered when the context expires; poll briefly
	// for the child to actually disappear.
	deadline := time.Now().Add(3 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if err == syscall.ESRCH {
			return // child gone: the process-group kill worked
		}
		if time.Now().After(deadline) {
			t.Fatalf("background child process %d still alive after shell timeout (orphaned): kill(pid,0)=%v", pid, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
