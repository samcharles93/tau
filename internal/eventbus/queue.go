// Package eventbus provides an in-process, type-safe event bus connecting
// publishers of typed events with subscribers interested in those events.
//
// The core design is adapted from Tailscale's util/eventbus package
// (BSD-3-Clause, Tailscale Inc & contributors).
package eventbus

import "slices"

// queue is an ordered queue of values up to capacity, if capacity is
// non-zero. Otherwise it is unbounded.
type queue[T any] struct {
	vals     []T
	start    int
	capacity int // zero means unbounded
}

// canAppend reports whether a value can be appended to q.vals without
// shifting values around.
func (q *queue[T]) canAppend() bool {
	return q.capacity == 0 || cap(q.vals) < q.capacity || len(q.vals) < cap(q.vals)
}

// Full reports whether the queue is at capacity and cannot accept more
// values without a shift.
func (q *queue[T]) Full() bool {
	return q.start == 0 && !q.canAppend()
}

// Empty reports whether the queue contains no values.
func (q *queue[T]) Empty() bool {
	return q.start == len(q.vals)
}

// Add adds v to the end of the queue. It panics if the queue is full.
func (q *queue[T]) Add(v T) {
	if !q.canAppend() {
		if q.start == 0 {
			panic("Add on a full queue")
		}

		// Slide remaining values back to the start of the array.
		n := copy(q.vals, q.vals[q.start:])
		toClear := len(q.vals) - n
		clear(q.vals[len(q.vals)-toClear:])
		q.vals = q.vals[:n]
		q.start = 0
	}

	q.vals = append(q.vals, v)
}

// Peek returns the first value in the queue, without removing it.
func (q *queue[T]) Peek() T {
	if q.Empty() {
		var zero T
		return zero
	}

	return q.vals[q.start]
}

// Drop discards the first value in the queue, if any.
func (q *queue[T]) Drop() {
	if q.Empty() {
		return
	}

	var zero T
	q.vals[q.start] = zero
	q.start++
	if q.Empty() {
		q.start = 0
		q.vals = q.vals[:0]
	}
}

// Snapshot returns a copy of the queue's contents.
func (q *queue[T]) Snapshot() []T {
	return slices.Clone(q.vals[q.start:])
}
