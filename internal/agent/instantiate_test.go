package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/procid"
	"github.com/samcharles93/tau/internal/store"
	"github.com/stretchr/testify/require"
)

func newSweepTestStore(t *testing.T) *store.SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	s, err := store.NewSQLiteStore(context.Background(), filepath.Join(dir, "test.db"), dir)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func saveOpenInstance(t *testing.T, s store.SessionStore, id string, pid int, processStartNS int64, startedAt time.Time) {
	t.Helper()
	require.NoError(t, s.SaveAgentInstance(context.Background(), store.AgentInstance{
		ID:               id,
		SpecName:         "tau",
		SpecScope:        "builtin",
		SpecHash:         "h",
		SpecSnapshot:     `{}`,
		ResolvedProvider: "openai",
		ResolvedModel:    "gpt-5",
		PID:              pid,
		ProcessStartNS:   processStartNS,
		StartedAt:        startedAt,
	}))
}

// --- G10: orphan sweep ---

// TestSweepOrphanedInstances_StaleAgeClosesRegardlessOfPID: the stale-age
// bound is unconditional per docs/specs/agents/04-storage-and-sessions.md
// (Orphan sweep: Stale-age bound) - even a row whose pid is alive (our own
// test process) must be closed once started_at exceeds the bound.
func TestSweepOrphanedInstances_StaleAgeClosesRegardlessOfPID(t *testing.T) {
	s := newSweepTestStore(t)
	saveOpenInstance(t, s, "tau#stale01", os.Getpid(), procid.CaptureProcessStartNS(os.Getpid()), time.Now().Add(-48*time.Hour))

	require.NoError(t, SweepOrphanedInstances(context.Background(), s, -1, 24*time.Hour))

	got, err := s.GetAgentInstance(context.Background(), "tau#stale01")
	require.NoError(t, err)
	if got.EndedAt.IsZero() {
		t.Fatal("stale row was not closed")
	}
	if got.ExitStatus != "failed" {
		t.Errorf("ExitStatus = %q, want failed", got.ExitStatus)
	}
	if got.FailureReason != "orphan sweep: stale age exceeded" {
		t.Errorf("FailureReason = %q", got.FailureReason)
	}
}

// TestSweepOrphanedInstances_NoPIDRecordedCloses covers the crash-before-
// SetAgentInstancePID case: pid == 0 closes immediately, no PID check.
func TestSweepOrphanedInstances_NoPIDRecordedCloses(t *testing.T) {
	s := newSweepTestStore(t)
	saveOpenInstance(t, s, "tau#nopid01", 0, 0, time.Now())

	require.NoError(t, SweepOrphanedInstances(context.Background(), s, -1, 24*time.Hour))

	got, err := s.GetAgentInstance(context.Background(), "tau#nopid01")
	require.NoError(t, err)
	if got.EndedAt.IsZero() {
		t.Fatal("pid==0 row was not closed")
	}
	if got.FailureReason != "orphan sweep: no pid recorded" {
		t.Errorf("FailureReason = %q", got.FailureReason)
	}
}

// TestSweepOrphanedInstances_DeadPIDCloses covers a pid that no longer
// exists (the owning process exited without closing its row - e.g. it was
// SIGKILLed).
func TestSweepOrphanedInstances_DeadPIDCloses(t *testing.T) {
	if _, err := os.Stat("/proc"); err != nil {
		t.Skip("requires /proc (Linux)")
	}
	s := newSweepTestStore(t)

	// Spawn and immediately reap a short-lived process to get a pid that is
	// guaranteed not to be running anymore, without relying on pid-space
	// guesswork.
	cmd := exec.CommandContext(context.Background(), "true")
	require.NoError(t, cmd.Run())
	deadPID := cmd.Process.Pid

	saveOpenInstance(t, s, "tau#dead01", deadPID, 0, time.Now())

	require.NoError(t, SweepOrphanedInstances(context.Background(), s, -1, 24*time.Hour))

	got, err := s.GetAgentInstance(context.Background(), "tau#dead01")
	require.NoError(t, err)
	if got.EndedAt.IsZero() {
		t.Fatal("dead-pid row was not closed")
	}
	if got.FailureReason != "orphan sweep: pid not found or identity mismatch" {
		t.Errorf("FailureReason = %q", got.FailureReason)
	}
}

// TestSweepOrphanedInstances_AlivePIDWithMatchingIdentitySkipped proves a
// genuinely live process (this test binary itself) with correctly recorded
// process-start identity is left open.
func TestSweepOrphanedInstances_AlivePIDWithMatchingIdentitySkipped(t *testing.T) {
	if _, err := os.Stat("/proc"); err != nil {
		t.Skip("requires /proc (Linux)")
	}
	s := newSweepTestStore(t)
	selfPID := os.Getpid()
	startNS := procid.CaptureProcessStartNS(selfPID)
	if startNS == 0 {
		t.Skip("could not capture own process-start identity")
	}
	// A different "ownPID" so the sweep doesn't skip via the self-exclusion
	// path - it must reach the actual PID-identity check.
	saveOpenInstance(t, s, "tau#alive01", selfPID, startNS, time.Now())

	require.NoError(t, SweepOrphanedInstances(context.Background(), s, -1, 24*time.Hour))

	got, err := s.GetAgentInstance(context.Background(), "tau#alive01")
	require.NoError(t, err)
	if !got.EndedAt.IsZero() {
		t.Fatalf("alive row with matching identity was closed: %+v", got)
	}
}

// TestSweepOrphanedInstances_RecycledPIDDifferentIdentityCloses proves a
// live pid whose recorded process-start identity does NOT match the
// current process (simulating PID recycling) is treated as dead, not
// alive - the whole point of persisting process_start_ns.
func TestSweepOrphanedInstances_RecycledPIDDifferentIdentityCloses(t *testing.T) {
	if _, err := os.Stat("/proc"); err != nil {
		t.Skip("requires /proc (Linux)")
	}
	s := newSweepTestStore(t)
	selfPID := os.Getpid()
	realStartNS := procid.CaptureProcessStartNS(selfPID)
	if realStartNS == 0 {
		t.Skip("could not capture own process-start identity")
	}
	// Record a bogus process-start identity for our own (very much alive)
	// pid - this is exactly what a recycled pid looks like: same pid,
	// different actual start time than what's on file.
	saveOpenInstance(t, s, "tau#recycled01", selfPID, realStartNS+1, time.Now())

	require.NoError(t, SweepOrphanedInstances(context.Background(), s, -1, 24*time.Hour))

	got, err := s.GetAgentInstance(context.Background(), "tau#recycled01")
	require.NoError(t, err)
	if got.EndedAt.IsZero() {
		t.Fatal("recycled-pid row (identity mismatch) was not closed")
	}
}

// TestSweepOrphanedInstances_OwnPIDNeverClosed proves the caller's own row
// (matched by pid) is never swept, even if it happens to be stale by age -
// self-exclusion must win over every other rule to avoid a process closing
// its own still-in-use row.
func TestSweepOrphanedInstances_OwnPIDNeverClosed(t *testing.T) {
	s := newSweepTestStore(t)
	ownPID := os.Getpid()
	saveOpenInstance(t, s, "tau#own01", ownPID, 0, time.Now().Add(-48*time.Hour))

	require.NoError(t, SweepOrphanedInstances(context.Background(), s, ownPID, 24*time.Hour))

	got, err := s.GetAgentInstance(context.Background(), "tau#own01")
	require.NoError(t, err)
	if !got.EndedAt.IsZero() {
		t.Fatal("own row was swept despite matching ownPID")
	}
}

// TestSweepOrphanedInstances_ZeroStaleAgeUsesDefault proves staleAge<=0
// (unset config) falls back to the 24h default rather than treating
// everything as immediately stale.
func TestSweepOrphanedInstances_ZeroStaleAgeUsesDefault(t *testing.T) {
	s := newSweepTestStore(t)
	saveOpenInstance(t, s, "tau#recent01", os.Getpid(), procid.CaptureProcessStartNS(os.Getpid()), time.Now().Add(-time.Minute))

	require.NoError(t, SweepOrphanedInstances(context.Background(), s, -1, 0))

	got, err := s.GetAgentInstance(context.Background(), "tau#recent01")
	require.NoError(t, err)
	if got.EndedAt.IsZero() == false {
		t.Fatal("recent row was closed under the default stale-age bound")
	}
}
