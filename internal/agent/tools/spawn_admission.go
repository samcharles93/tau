package tools

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/samcharles93/tau/internal/config"
)

// spawnAdmission enforces the concurrency and resource ceilings from
// docs/specs/agents/02-spawning-and-lifecycle.md (Concurrency and resource
// ceilings): a per-parent-instance active-child limit, a process-wide
// active-child limit, and a per-parent FIFO spawn queue. One instance is
// shared process-wide across every agent tool call in this OS process,
// since max_total_children bounds the whole process, not any single call.
type spawnAdmission struct {
	mu          sync.Mutex
	totalActive int
	parents     map[string]int // parentInstanceID -> active count
	queue       []*spawnWaiter // global FIFO across all parents
}

type spawnWaiter struct {
	parentInstanceID string
	ready            chan struct{}
	removed          bool
}

func newSpawnAdmission() *spawnAdmission {
	return &spawnAdmission{parents: make(map[string]int)}
}

// globalSpawnAdmission is the process-wide limiter. max_total_children is
// defined as a ceiling on "all agents in this OS process"
// (02-spawning-and-lifecycle.md), so this must not be per-call state.
var globalSpawnAdmission = newSpawnAdmission()

// spawnRejected is returned by acquireSpawnSlot when a spawn cannot be
// admitted: the process-wide or per-parent active ceiling is saturated, the
// per-parent queue is full, or a queued spawn was evicted on timeout or
// cancellation. reason is the user-facing failure_reason text
// (Rejection visibility); code is the structured rejection_reason for
// observability (active_limit / total_limit / queue_full / queue_timeout /
// cancelled).
type spawnRejected struct {
	reason string
	code   string
	queued bool
}

func (e *spawnRejected) Error() string { return e.reason }

// acquireSpawnSlot blocks until a concurrency slot for parentInstanceID is
// admitted, ctx is cancelled, or deadline passes while queued - whichever
// comes first. deadline is the absolute time by which a queued spawn must
// be admitted or evicted; per spec, queue time consumes the child's own
// timeout/deadline budget, so callers pass the resolved timeout/deadline
// for the spawn being admitted. A zero deadline means "no queue timeout
// beyond ctx". On success, release must be called exactly once, when the
// child is done, to free the slot and admit the next queued spawn.
func acquireSpawnSlot(ctx context.Context, parentInstanceID string, agentsCfg config.AgentsConfig, deadline time.Time) (release func(), err error) {
	return globalSpawnAdmission.acquire(ctx, parentInstanceID, agentsCfg, deadline)
}

func (s *spawnAdmission) acquire(ctx context.Context, parentInstanceID string, agentsCfg config.AgentsConfig, deadline time.Time) (func(), error) {
	defaults := config.DefaultAgentsConfig()
	maxActive := agentsCfg.MaxActiveChildren
	if maxActive <= 0 {
		maxActive = defaults.MaxActiveChildren
	}
	maxTotal := agentsCfg.MaxTotalChildren
	if maxTotal <= 0 {
		maxTotal = defaults.MaxTotalChildren
	}
	maxQueued := agentsCfg.MaxQueuedSpawns
	if maxQueued <= 0 {
		maxQueued = defaults.MaxQueuedSpawns
	}

	s.mu.Lock()
	if s.totalActive >= maxTotal {
		s.mu.Unlock()
		return nil, &spawnRejected{
			reason: fmt.Sprintf("spawn rejected: process-wide active children at maximum (%d)", maxTotal),
			code:   "total_limit",
		}
	}
	if s.parents[parentInstanceID] < maxActive {
		s.parents[parentInstanceID]++
		s.totalActive++
		s.mu.Unlock()
		return s.releaseFunc(parentInstanceID, maxActive, maxTotal), nil
	}
	queuedForParent := 0
	for _, w := range s.queue {
		if !w.removed && w.parentInstanceID == parentInstanceID {
			queuedForParent++
		}
	}
	if queuedForParent >= maxQueued {
		s.mu.Unlock()
		return nil, &spawnRejected{
			reason: fmt.Sprintf("spawn rejected: per-parent active children at maximum (%d), spawn queue full (%d)", maxActive, maxQueued),
			code:   "queue_full",
		}
	}
	w := &spawnWaiter{parentInstanceID: parentInstanceID, ready: make(chan struct{})}
	s.queue = append(s.queue, w)
	s.mu.Unlock()

	var timeoutCh <-chan time.Time
	if !deadline.IsZero() {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			s.evict(w)
			return nil, &spawnRejected{
				reason: "spawn rejected: timed out in spawn queue",
				code:   "queue_timeout",
				queued: true,
			}
		}
		timer := time.NewTimer(remaining)
		defer timer.Stop()
		timeoutCh = timer.C
	}

	select {
	case <-w.ready:
		return s.releaseFunc(parentInstanceID, maxActive, maxTotal), nil
	case <-ctx.Done():
		s.evict(w)
		return nil, &spawnRejected{
			reason: "spawn rejected: cancelled while queued",
			code:   "cancelled",
			queued: true,
		}
	case <-timeoutCh:
		s.evict(w)
		return nil, &spawnRejected{
			reason: "spawn rejected: timed out in spawn queue",
			code:   "queue_timeout",
			queued: true,
		}
	}
}

// evict removes w from the queue if it hasn't already been admitted. A
// waiter that loses the race against releaseFunc (already admitted, ready
// closed) is left alone - its slot is real and must be released normally.
func (s *spawnAdmission) evict(w *spawnWaiter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if w.removed {
		return
	}
	w.removed = true
	for i, q := range s.queue {
		if q == w {
			s.queue = append(s.queue[:i], s.queue[i+1:]...)
			break
		}
	}
}

// releaseFunc returns a once-only function that frees parentInstanceID's
// slot and admits the next eligible queued waiter, in FIFO order across all
// parents so that a process-wide-limit release does not only benefit the
// releasing parent's own queue.
func (s *spawnAdmission) releaseFunc(parentInstanceID string, maxActive, maxTotal int) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.totalActive--
			if s.parents[parentInstanceID] > 0 {
				s.parents[parentInstanceID]--
			}
			if s.parents[parentInstanceID] == 0 {
				delete(s.parents, parentInstanceID)
			}
			for i, w := range s.queue {
				if w.removed {
					continue
				}
				if s.totalActive >= maxTotal {
					return
				}
				if s.parents[w.parentInstanceID] >= maxActive {
					continue
				}
				w.removed = true
				s.queue = append(s.queue[:i], s.queue[i+1:]...)
				s.parents[w.parentInstanceID]++
				s.totalActive++
				close(w.ready)
				return
			}
		})
	}
}

// computeSpawnDeadline derives the absolute time by which a queued spawn
// must be admitted, per docs/specs/agents/02-spawning-and-lifecycle.md
// (Queue behavior: "Queue time consumes the child's timeout/deadline
// budget. The clock starts when agent is called, not when the child
// process actually starts."). It's the earliest of the resolved spec
// timeout, an explicit budget.timeout override, and an explicit
// budget.deadline (absolute RFC 3339), relative to now. A zero result
// means no queue timeout beyond ctx cancellation.
//
// Budget fields are validated here, at admission time, per the spec's
// validation rules - a malformed or out-of-range budget.timeout/budget.deadline
// returns an error so the spawn is rejected before any slot is acquired or
// child process started, instead of being silently dropped from the queue
// deadline and only failing later in buildAgentBudget (tau-kk4).
func computeSpawnDeadline(args agentToolArgs, specTimeout time.Duration) (time.Time, error) {
	now := time.Now()
	var deadline time.Time
	consider := func(candidate time.Time) {
		if candidate.IsZero() {
			return
		}
		if deadline.IsZero() || candidate.Before(deadline) {
			deadline = candidate
		}
	}
	if specTimeout > 0 {
		consider(now.Add(specTimeout))
	}
	if args.Budget != nil {
		if err := considerBudget(args.Budget, now, consider); err != nil {
			return time.Time{}, err
		}
	}
	return deadline, nil
}

// considerBudget validates the budget's timeout/deadline fields per the
// spec's validation rules and offers them as deadline candidates. A
// malformed or out-of-range value returns an error (loud, tau-kk4) rather
// than being silently dropped.
func considerBudget(b *agentBudgetArgs, now time.Time, consider func(time.Time)) error {
	if b.Timeout != "" {
		d, err := parseBudgetTimeout(b.Timeout)
		if err != nil {
			return err
		}
		consider(now.Add(d))
	}
	if b.Deadline != "" {
		t, err := parseBudgetDeadline(b.Deadline)
		if err != nil {
			return err
		}
		consider(t)
	}
	return nil
}

// parseBudgetTimeout parses a budget.timeout duration per the validation
// rules in docs/specs/agents/02-spawning-and-lifecycle.md: valid range is
// 1s … 24h, values below 1s are rejected, values above 24h are capped to
// 24h. A malformed duration is an error, not a silently ignored field
// (tau-kk4). The messages mirror buildAgentBudget so both validation
// sites reject identically.
func parseBudgetTimeout(timeout string) (time.Duration, error) {
	d, err := time.ParseDuration(timeout)
	if err != nil {
		return 0, fmt.Errorf("invalid budget.timeout %q: %w", timeout, err)
	}
	if d < time.Second {
		return 0, fmt.Errorf("invalid budget.timeout %q: must be >= 1s", timeout)
	}
	if d > 24*time.Hour {
		d = 24 * time.Hour
	}
	return d, nil
}

// parseBudgetDeadline parses an RFC 3339 budget.deadline per the validation
// rules in docs/specs/agents/02-spawning-and-lifecycle.md: it must parse as
// RFC 3339 and be in the future at spawn time. A malformed or past deadline
// is an error, not a silently ignored field (tau-kk4).
func parseBudgetDeadline(deadline string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, deadline)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid budget.deadline %q: must be RFC 3339: %w", deadline, err)
	}
	if !t.After(time.Now()) {
		return time.Time{}, fmt.Errorf("invalid budget.deadline %q: must be in the future", deadline)
	}
	return t, nil
}
