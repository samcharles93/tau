//go:build windows

package procid

import (
	"log/slog"

	"golang.org/x/sys/windows"
)

// stillActive is the STILL_ACTIVE sentinel (259) GetExitCodeProcess
// returns for a running process. Not exported by golang.org/x/sys/windows.
const stillActive = 259

// CaptureProcessStartNS has no implementation on this platform (see
// docs/specs/agents/04-storage-and-sessions.md, Orphan sweep: the Windows
// GetProcessTimes path is documented but not implemented here — it can't
// be verified without a Windows build/test target, and a wrong syscall
// implementation is worse than an honest PID-only fallback). Returns 0
// (unavailable); the sweep falls back to PID-only + the stale-age bound.
func CaptureProcessStartNS(_ int) int64 {
	return 0
}

// CheckPIDIdentity falls back to a PID-only liveness check (open the
// process handle; ERROR_INVALID_PARAMETER means no such pid), per the
// spec's documented "Unsupported platform" behavior. wantStartNS is
// ignored since no identity was ever captured on this platform.
func CheckPIDIdentity(pid int, _ int64) PIDCheck {
	slog.Warn("orphan sweep: process-start identity unsupported on this platform, falling back to PID-only check", "pid", pid)
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if err == windows.ERROR_INVALID_PARAMETER {
			return PIDCheckDead
		}
		return PIDCheckIndeterminate
	}
	defer windows.CloseHandle(h)

	var exitCode uint32
	if err := windows.GetExitCodeProcess(h, &exitCode); err != nil {
		return PIDCheckIndeterminate
	}
	if exitCode != stillActive {
		return PIDCheckDead
	}
	return PIDCheckAlive
}
