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
// admitted, ctx is cancelled, or deadline passes while queued — whichever
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
// closed) is left alone — its slot is real and must be released normally.
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
// process actually starts."). It's the earlier of the resolved spec
// timeout and an explicit budget.deadline, relative to now. A zero result
// means no queue timeout beyond ctx cancellation.
func computeSpawnDeadline(args agentToolArgs, timeout time.Duration) time.Time {
	now := time.Now()
	var deadline time.Time
	if timeout > 0 {
		deadline = now.Add(timeout)
	}
	if args.Budget != nil && args.Budget.Deadline != "" {
		if d, err := time.ParseDuration(args.Budget.Deadline); err == nil {
			budgetDeadline := now.Add(d)
			if deadline.IsZero() || budgetDeadline.Before(deadline) {
				deadline = budgetDeadline
			}
		}
	}
	return deadline
}
