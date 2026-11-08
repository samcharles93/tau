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
	"sync"
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

// logRecordingBridge is a UIBridge that records Log chunks, for asserting
// the shell tool still forwards output when a bridge is supplied.
type logRecordingBridge struct {
	NonInteractiveBridge
	mu   sync.Mutex
	logs []string
}

func (b *logRecordingBridge) Log(chunk string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.logs = append(b.logs, chunk)
}

func (b *logRecordingBridge) joined() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Join(b.logs, "")
}

// TestShellExecutorNilBridge (tau-sn6): the shell tool must tolerate a nil
// UIBridge - run the command and return its output instead of panicking in
// bridgeWriter.Write. Every other builtin tool already ignores a nil bridge.
func TestShellExecutorNilBridge(t *testing.T) {
	dir := t.TempDir()
	executor := makeShellExecutor(dir, nil)
	params, err := json.Marshal(ShellParams{Command: "echo tau-sn6-nil-bridge"})
	require.NoError(t, err)

	res, err := executor(context.Background(), params, nil)
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Content, "tau-sn6-nil-bridge")
}

// TestShellExecutorForwardsToBridge: with a non-nil bridge, stdout/stderr
// chunks are still forwarded via Log (behaviour unchanged by the tau-sn6
// nil guard).
func TestShellExecutorForwardsToBridge(t *testing.T) {
	dir := t.TempDir()
	executor := makeShellExecutor(dir, nil)
	params, err := json.Marshal(ShellParams{Command: "echo tau-sn6-fwd; echo err-line >&2"})
	require.NoError(t, err)

	bridge := &logRecordingBridge{}
	res, err := executor(context.Background(), params, bridge)
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Content, "tau-sn6-fwd")
	require.Contains(t, res.Content, "err-line")

	forwarded := bridge.joined()
	require.Contains(t, forwarded, "tau-sn6-fwd")
	require.Contains(t, forwarded, "err-line")
}
