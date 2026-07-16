//go:build !linux && !windows

package procid

import (
	"errors"
	"log/slog"
	"syscall"
)

// CaptureProcessStartNS has no implementation on this platform (see
// docs/specs/agents/04-storage-and-sessions.md, Orphan sweep: the macOS
// KERN_PROC_PID path is documented but not implemented here - it can't be
// verified without a macOS build/test target, and a wrong syscall
// implementation is worse than an honest PID-only fallback). Returns 0
// (unavailable); the sweep falls back to PID-only + the stale-age bound.
func CaptureProcessStartNS(_ int) int64 {
	return 0
}

// CheckPIDIdentity falls back to a PID-only liveness check via Signal(0),
// per the spec's documented "Unsupported platform" behavior. wantStartNS is
// ignored since no identity was ever captured on this platform.
func CheckPIDIdentity(pid int, _ int64) PIDCheck {
	slog.Warn("orphan sweep: process-start identity unsupported on this platform, falling back to PID-only check", "pid", pid)
	err := syscall.Kill(pid, syscall.Signal(0))
	if err == nil {
		return PIDCheckAlive
	}
	if errors.Is(err, syscall.ESRCH) {
		return PIDCheckDead
	}
	// EPERM (pid exists, owned by another user) or another transient
	// error - can't prove either way.
	return PIDCheckIndeterminate
}
