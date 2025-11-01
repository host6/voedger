/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package in10nmem

import (
	"context"
	"sync"
)

type NotifyQueue[T any] struct {
	mu     sync.Mutex
	cond   *sync.Cond
	items  []T
	closed bool
}

func NewQueueWithContext[T any](ctx context.Context) (q *NotifyQueue[T]) {
	q = New[T]()
	go func() {
		<-ctx.Done()
		q.Close() // Close is idempotent; safe under races.
	}()
	return q
}

func New[T any]() *NotifyQueue[T] {
	q := &NotifyQueue[T]{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// Push is non-blocking (aside from brief mutex hold).
func (q *NotifyQueue[T]) Push(ev T) {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.items = append(q.items, ev)
	// Signal while holding the lock to avoid races with the Waiter’s predicate check.
	q.cond.Signal()
	q.mu.Unlock()
}

// PopBatch waits until there is at least one item (or the queue is closed),
// then returns the current batch and clears the queue in O(1) by swapping slices.
func (q *NotifyQueue[T]) PopBatch() (batch []T, ok bool) {
	q.mu.Lock()
	for len(q.items) == 0 && !q.closed {
		q.cond.Wait() // releases lock, sleeps, then re-acquires
	}
	if q.closed && len(q.items) == 0 {
		q.mu.Unlock()
		return nil, false
	}
	batch = q.items
	q.items = nil // allow GC & O(1) drain
	q.mu.Unlock()
	return batch, true
}

// Close wakes any waiters and makes future Push no-ops.
func (q *NotifyQueue[T]) Close() {
	q.mu.Lock()
	q.closed = true
	q.cond.Broadcast()
	q.mu.Unlock()
}
