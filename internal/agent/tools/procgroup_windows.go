//go:build windows

package tools

import (
	"os/exec"
	"syscall"
)

// setProcessGroup configures cmd to start in a new process group on
// Windows (see docs/specs/agents/02-spawning-and-lifecycle.md, Process
// group management / Platform behavior).
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= syscall.CREATE_NEW_PROCESS_GROUP
}

// signalProcessGroup best-effort terminates cmd's direct process on
// Windows. Windows has no POSIX-style negative-pid group signal; the spec's
// documented fallback is TerminateProcess (GenerateConsoleCtrlEvent would
// require additional job-object/console plumbing this doesn't set up), so
// descendants spawned by the child are not guaranteed to receive this —
// a known platform limitation, not an oversight.
func signalProcessGroup(cmd *exec.Cmd, _ syscall.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
