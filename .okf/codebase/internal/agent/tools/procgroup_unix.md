---
description: Source module internal/agent/tools/procgroup_unix.go (34 lines).
resource: internal/agent/tools/procgroup_unix.go
tags:
    - go
    - source
timestamp: "2026-07-21T18:36:12Z"
title: procgroup_unix.go
type: Module
---

# Module procgroup_unix.go

**Path**: `internal/agent/tools/procgroup_unix.go`  
**Lines**: 34

## Snippet Preview

```
//go:build !windows

package tools

import (
	"os/exec"
	"syscall"
)

// setProcessGroup configures cmd to start as the leader of a new process
// group, so the parent can signal the whole tree (see
// docs/specs/agents/02-spawning-and-lifecycle.md, Process group
// management). Must be called before cmd.Start().
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// signalProcessGroup sends sig to cmd's entire process group (shells,
// tools, provider subprocesses, grandchild agents - everything that
// inherited the group from setProcessGroup). Must be called after
// cmd.Start(). ESRCH (group already gone) is not an error worth surfacing.
func signalProcessGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	// A negative pid targets the whole process group (see kill(2)).
	if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil && err != syscall.ESRCH {
```
