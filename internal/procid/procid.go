// Package procid provides OS process-start identity checks used to
// distinguish a live spawned process from an unrelated process that
// recycled the same pid, per docs/specs/agents/04-storage-and-sessions.md
// (Orphan sweep: PID check with process-start identity). It has no
// dependency on internal/agent or internal/agent/tools so both - the
// root's own orphan sweep and the agent tool's child-spawn path - can
// share one implementation.
package procid

// PIDCheck is the tri-state result of checking whether a recorded pid still
// refers to the process that was spawned.
type PIDCheck int

const (
	// PIDCheckAlive means the process is running and (where identity can
	// be verified) its process-start identity matches what was recorded
	// at spawn time.
	PIDCheckAlive PIDCheck = iota
	// PIDCheckDead means the pid is not running, or is running but its
	// process-start identity does not match (the pid was recycled).
	PIDCheckDead
	// PIDCheckIndeterminate means the check could not be completed
	// (permission denied, a TOCTOU race, or a transient read failure).
	// Callers must not treat this as either alive or dead - the
	// stale-age bound is the backstop for rows that stay indeterminate
	// forever.
	PIDCheckIndeterminate
)

// CaptureProcessStartNS and CheckPIDIdentity are implemented per-platform:
// see procid_linux.go for the tested implementation (/proc/<pid>/stat) and
// procid_unix.go / procid_windows.go for the PID-only fallback used
// everywhere else.
