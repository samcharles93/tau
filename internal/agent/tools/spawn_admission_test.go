package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/samcharles93/tau/internal/config"
)

// --- G5: concurrency and resource ceilings ---
// Saturation test requirements per docs/specs/agents/
// 02-spawning-and-lifecycle.md (Concurrency and resource ceilings). Each
// test uses its own *spawnAdmission instance rather than the process-wide
// globalSpawnAdmission, so tests don't interfere with each other.

func agentsCfgFor(maxActive, maxTotal, maxQueued int) config.AgentsConfig {
	return config.AgentsConfig{
		MaxActiveChildren: maxActive,
		MaxTotalChildren:  maxTotal,
		MaxQueuedSpawns:   maxQueued,
	}
}

// TestSpawnAdmission_FifthQueuesWhenActiveAtMax: "5 agent calls when
// max_active=4 -> 4 start immediately, 1 queued".
func TestSpawnAdmission_FifthQueuesWhenActiveAtMax(t *testing.T) {
	s := newSpawnAdmission()
	cfg := agentsCfgFor(4, 16, 8)

	var releases []func()
	for i := range 4 {
		release, err := s.acquire(context.Background(), "parent", cfg, time.Time{})
		if err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
		releases = append(releases, release)
	}

	fifthDone := make(chan struct{})
	go func() {
		release, err := s.acquire(context.Background(), "parent", cfg, time.Time{})
		if err != nil {
			t.Errorf("5th acquire: %v", err)
			return
		}
		release()
		close(fifthDone)
	}()

	select {
	case <-fifthDone:
		t.Fatal("5th acquire admitted immediately - expected it to queue behind the 4 active")
	case <-time.After(50 * time.Millisecond):
	}

	// Free one active slot - the queued 5th should now be admitted.
	releases[0]()

	select {
	case <-fifthDone:
	case <-time.After(2 * time.Second):
		t.Fatal("queued 5th spawn was never admitted after a slot freed")
	}

	for _, r := range releases[1:] {
		r()
	}
}

// TestSpawnAdmission_QueueFullRejectsThirteenth: "13 agent calls when
// max_active=4, queue=8 -> 4 active, 8 queued, 1 rejected".
func TestSpawnAdmission_QueueFullRejectsThirteenth(t *testing.T) {
	s := newSpawnAdmission()
	cfg := agentsCfgFor(4, 16, 8)

	var active []func()
	for i := range 4 {
		release, err := s.acquire(context.Background(), "parent", cfg, time.Time{})
		if err != nil {
			t.Fatalf("active acquire %d: %v", i, err)
		}
		active = append(active, release)
	}

	// 8 more queue behind the 4 active.
	type queuedCall struct {
		release func()
		err     error
	}
	results := make(chan queuedCall, 8)
	for range 8 {
		go func() {
			release, err := s.acquire(context.Background(), "parent", cfg, time.Time{})
			results <- queuedCall{release, err}
		}()
	}
	// Give the goroutines time to reach the queue before we probe the 13th.
	time.Sleep(50 * time.Millisecond)

	// The 13th call must be rejected synchronously (queue is full), not block.
	rejectedCh := make(chan error, 1)
	go func() {
		_, err := s.acquire(context.Background(), "parent", cfg, time.Time{})
		rejectedCh <- err
	}()
	select {
	case err := <-rejectedCh:
		if err == nil {
			t.Fatal("13th acquire succeeded, want rejection (queue full)")
		}
		var rej *spawnRejected
		if !errors.As(err, &rej) || rej.code != "queue_full" {
			t.Fatalf("13th acquire error = %v, want code=queue_full", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("13th acquire did not return promptly - should reject immediately, not queue")
	}

	// Drain: release the 4 active so the 8 queued can all complete.
	for _, r := range active {
		r()
	}
	for i := range 8 {
		select {
		case qc := <-results:
			if qc.err != nil {
				t.Errorf("queued call %d failed: %v", i, qc.err)
				continue
			}
			qc.release()
		case <-time.After(5 * time.Second):
			t.Fatalf("queued call %d never admitted", i)
		}
	}
}

// TestSpawnAdmission_QueuedSpawnTimesOut: "Queued spawn exceeds timeout ->
// Removed from queue, rejected with 'timed out in spawn queue'".
func TestSpawnAdmission_QueuedSpawnTimesOut(t *testing.T) {
	s := newSpawnAdmission()
	cfg := agentsCfgFor(1, 16, 8)

	release, err := s.acquire(context.Background(), "parent", cfg, time.Time{})
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer release()

	deadline := time.Now().Add(30 * time.Millisecond)
	start := time.Now()
	_, err = s.acquire(context.Background(), "parent", cfg, deadline)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("queued acquire past its deadline succeeded, want timeout rejection")
	}
	var rej *spawnRejected
	if !errors.As(err, &rej) || rej.code != "queue_timeout" {
		t.Fatalf("error = %v, want code=queue_timeout", err)
	}
	if !rej.queued {
		t.Errorf("queued = false, want true (this spawn did spend time in the queue)")
	}
	if elapsed > time.Second {
		t.Errorf("timeout took %v, want ~30ms", elapsed)
	}

	// The evicted waiter must not linger in the queue.
	s.mu.Lock()
	queueLen := len(s.queue)
	s.mu.Unlock()
	if queueLen != 0 {
		t.Errorf("queue still has %d entries after eviction, want 0", queueLen)
	}
}

// TestSpawnAdmission_CancellationDrainsQueue: "Parent turn cancelled with
// queued spawns -> All queued spawns removed cleanly".
func TestSpawnAdmission_CancellationDrainsQueue(t *testing.T) {
	s := newSpawnAdmission()
	cfg := agentsCfgFor(1, 16, 8)

	release, err := s.acquire(context.Background(), "parent", cfg, time.Time{})
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := s.acquire(ctx, "parent", cfg, time.Time{})
		errCh <- err
	}()
	time.Sleep(20 * time.Millisecond) // let it reach the queue

	cancel()

	select {
	case err := <-errCh:
		var rej *spawnRejected
		if !errors.As(err, &rej) || rej.code != "cancelled" {
			t.Fatalf("error = %v, want code=cancelled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("acquire did not return promptly after ctx cancellation")
	}

	s.mu.Lock()
	queueLen := len(s.queue)
	s.mu.Unlock()
	if queueLen != 0 {
		t.Errorf("queue still has %d entries after cancellation, want 0", queueLen)
	}
}

// TestSpawnAdmission_PerParentLimitsAreIndependent: "Root spawns 4
// children, each spawns 4 grandchildren -> Allowed (per-parent limits, not
// global)". Different parentInstanceIDs each get their own active budget.
func TestSpawnAdmission_PerParentLimitsAreIndependent(t *testing.T) {
	s := newSpawnAdmission()
	cfg := agentsCfgFor(4, 16, 8)

	var releases []func()
	for _, parent := range []string{"root", "child-a", "child-b"} {
		for i := range 4 {
			release, err := s.acquire(context.Background(), parent, cfg, time.Time{})
			if err != nil {
				t.Fatalf("acquire for %s #%d: %v", parent, i, err)
			}
			releases = append(releases, release)
		}
	}
	for _, r := range releases {
		r()
	}
}

// TestSpawnAdmission_TotalLimitRejectsImmediately: "17 agent calls when
// max_total=16 -> 16th admitted, 17th rejected immediately". Rejection must
// not queue - queueing does not help when the process itself is at
// capacity.
func TestSpawnAdmission_TotalLimitRejectsImmediately(t *testing.T) {
	s := newSpawnAdmission()
	// max_active high enough that the per-parent ceiling never binds; each
	// call uses a distinct parent so only max_total can be the limiter.
	cfg := agentsCfgFor(100, 16, 8)

	var releases []func()
	for i := range 16 {
		release, err := s.acquire(context.Background(), "parent", cfg, time.Time{})
		if err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
		releases = append(releases, release)
	}

	start := time.Now()
	_, err := s.acquire(context.Background(), "parent", cfg, time.Time{})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("17th acquire succeeded, want rejection (total limit)")
	}
	var rej *spawnRejected
	if !errors.As(err, &rej) || rej.code != "total_limit" {
		t.Fatalf("error = %v, want code=total_limit", err)
	}
	if rej.queued {
		t.Error("17th acquire was queued, want immediate rejection")
	}
	if elapsed > time.Second {
		t.Errorf("17th acquire took %v, want immediate", elapsed)
	}

	for _, r := range releases {
		r()
	}
}

// TestComputeSpawnDeadline_EarliestOfTimeoutAndBudget verifies the deadline
// derivation picks the earliest of the resolved spec timeout, an explicit
// budget.timeout override, and an explicit absolute budget.deadline.
func TestComputeSpawnDeadline_EarliestOfTimeoutAndBudget(t *testing.T) {
	before := time.Now()

	// No timeout, no budget -> zero deadline (no queue timeout).
	got, err := computeSpawnDeadline(agentToolArgs{}, 0)
	if err != nil {
		t.Fatalf("no timeout/budget: unexpected error %v", err)
	}
	if !got.IsZero() {
		t.Errorf("no timeout/budget: deadline = %v, want zero", got)
	}

	// Spec timeout only.
	got, err = computeSpawnDeadline(agentToolArgs{}, 5*time.Minute)
	if err != nil {
		t.Fatalf("spec timeout only: unexpected error %v", err)
	}
	if got.Before(before.Add(5*time.Minute)) || got.After(time.Now().Add(5*time.Minute)) {
		t.Errorf("timeout-only deadline = %v, want ~5m from now", got)
	}

	// budget.timeout shorter than the spec timeout wins.
	args := agentToolArgs{Budget: &agentBudgetArgs{Timeout: "1m"}}
	got, err = computeSpawnDeadline(args, 5*time.Minute)
	if err != nil {
		t.Fatalf("budget.timeout: unexpected error %v", err)
	}
	if got.After(time.Now().Add(90 * time.Second)) {
		t.Errorf("budget.timeout should win over longer spec timeout: got %v", got)
	}

	// budget.deadline (absolute) earlier than the spec timeout wins.
	args = agentToolArgs{Budget: &agentBudgetArgs{Deadline: time.Now().Add(30 * time.Second).Format(time.RFC3339)}}
	got, err = computeSpawnDeadline(args, 5*time.Minute)
	if err != nil {
		t.Fatalf("budget.deadline: unexpected error %v", err)
	}
	if got.After(time.Now().Add(90 * time.Second)) {
		t.Errorf("budget.deadline should win over longer spec timeout: got %v", got)
	}
}

// TestComputeSpawnDeadline_MalformedBudgetIsLoud (tau-kk4): a malformed or
// out-of-range budget.timeout/budget.deadline must return an error at
// admission time (rejecting the spawn before a slot or process is wasted),
// not be silently dropped from the queue deadline. Valid ranges and clamps
// follow the validation rules in docs/specs/agents/02-spawning-and-lifecycle.md.
func TestComputeSpawnDeadline_MalformedBudgetIsLoud(t *testing.T) {
	future := time.Now().Add(time.Hour).Format(time.RFC3339)
	past := time.Now().Add(-time.Hour).Format(time.RFC3339)

	tests := []struct {
		name    string
		budget  *agentBudgetArgs
		wantErr string // substring; empty means no error expected
	}{
		{name: "malformed timeout", budget: &agentBudgetArgs{Timeout: "5mm"}, wantErr: `invalid budget.timeout "5mm"`},
		{name: "timeout below 1s rejected", budget: &agentBudgetArgs{Timeout: "500ms"}, wantErr: "must be >= 1s"},
		{name: "timeout above 24h clamped not rejected", budget: &agentBudgetArgs{Timeout: "25h"}},
		{name: "malformed deadline", budget: &agentBudgetArgs{Deadline: "next friday"}, wantErr: `invalid budget.deadline "next friday"`},
		{name: "past deadline rejected", budget: &agentBudgetArgs{Deadline: past}, wantErr: "must be in the future"},
		{name: "valid timeout and deadline", budget: &agentBudgetArgs{Timeout: "10m", Deadline: future}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := computeSpawnDeadline(agentToolArgs{Budget: tt.budget}, 0)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got.IsZero() {
					t.Fatal("valid budget produced a zero deadline, want a queue deadline")
				}
				return
			}
			if err == nil {
				t.Fatalf("deadline = %v, want error containing %q", got, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}

	// The 24h clamp: a 25h timeout must yield a deadline ~24h out (and win
	// over a longer spec timeout), per "> 24h: cap to 24h".
	got, err := computeSpawnDeadline(agentToolArgs{Budget: &agentBudgetArgs{Timeout: "25h"}}, 48*time.Hour)
	if err != nil {
		t.Fatalf("25h timeout: unexpected error %v", err)
	}
	lower := time.Now().Add(24*time.Hour - time.Minute)
	upper := time.Now().Add(24*time.Hour + time.Minute)
	if got.Before(lower) || got.After(upper) {
		t.Errorf("25h timeout clamped deadline = %v, want ~24h from now (%v..%v)", got, lower, upper)
	}
}
