/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package commandprocessor

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/voedger/voedger/pkg/bus"
	"github.com/voedger/voedger/pkg/istructs"
)

// logRead records one ReadPLog or ReadWLog call so tests can verify that
// recovery starts at the persisted checkpoint and uses the expected range.
type logRead struct {
	offset istructs.Offset
	count  int
}

// recoveryTestReads collects log-read observations made by recovery goroutines.
// Its lock lets a test safely inspect the observations while recovery is still
// running, without adding synchronization to production log readers.
type recoveryTestReads[K comparable] struct {
	mu    sync.Mutex
	byKey map[K][]logRead
}

func newRecoveryTestReads[K comparable]() *recoveryTestReads[K] {
	return &recoveryTestReads[K]{byKey: map[K][]logRead{}}
}

// record is installed as a command-processor hook and preserves the order in
// which recovery reads a particular partition or workspace log.
func (r *recoveryTestReads[K]) record(key K, offset istructs.Offset, count int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byKey[key] = append(r.byKey[key], logRead{offset: offset, count: count})
}

func (r *recoveryTestReads[K]) reset(key K) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byKey, key)
}

// values returns a snapshot rather than the stored slice, preventing test code
// from racing with or mutating observations appended by a recovery goroutine.
func (r *recoveryTestReads[K]) values(key K) []logRead {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]logRead(nil), r.byKey[key]...)
}

// recoveryAttempt is the test-side state of the latest scheduled recovery for
// one key. The optional gate keeps the goroutine in progress, while done lets a
// test wait for the manager to publish the final error deterministically.
type recoveryAttempt struct {
	done chan struct{}
	gate <-chan struct{}
	err  error
}

// recoveryTestAttempts provides deterministic control over asynchronous
// recoverManager attempts. Tests use it to hold an attempt open, inject a
// failure, wait for completion, and prove that duplicate or over-limit requests
// did not schedule another attempt. K allows the same control to serve both
// partition and workspace recovery.
type recoveryTestAttempts[K comparable] struct {
	mu           sync.Mutex
	attempts     map[K]*recoveryAttempt
	starts       map[K]int
	nextGates    map[K]<-chan struct{}
	nextFailures map[K]error
}

func newRecoveryTestAttempts[K comparable]() *recoveryTestAttempts[K] {
	return &recoveryTestAttempts[K]{
		attempts:     map[K]*recoveryAttempt{},
		starts:       map[K]int{},
		nextGates:    map[K]<-chan struct{}{},
		nextFailures: map[K]error{},
	}
}

func (c *recoveryTestAttempts[K]) hooks() recoveryHooks[K] {
	return recoveryHooks[K]{
		scheduled:        c.scheduled,
		beforeAttempt:    c.beforeAttempt,
		attemptCompleted: c.attemptCompleted,
	}
}

// scheduled records an attempt after recoverManager has reserved a worker slot
// but before it starts the goroutine. Consequently, starts counts actual
// scheduled work and excludes requests rejected by the concurrency limit.
func (c *recoveryTestAttempts[K]) scheduled(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.starts[key]++
	c.attempts[key] = &recoveryAttempt{
		done: make(chan struct{}),
		gate: c.nextGates[key],
		err:  c.nextFailures[key],
	}
	delete(c.nextGates, key)
	delete(c.nextFailures, key)
}

// beforeAttempt applies the gate and injected failure captured by scheduled.
// Waiting on ctx as well as the gate ensures service shutdown can always release
// a deliberately blocked test attempt.
func (c *recoveryTestAttempts[K]) beforeAttempt(ctx context.Context, key K) error {
	c.mu.Lock()
	attempt := c.attempts[key]
	c.mu.Unlock()
	if attempt.gate != nil {
		select {
		case <-attempt.gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return attempt.err
}

// attemptCompleted stores the manager's final, wrapped error and releases tests
// waiting for the recorded attempt to finish.
func (c *recoveryTestAttempts[K]) attemptCompleted(key K, err error) {
	c.mu.Lock()
	attempt := c.attempts[key]
	attempt.err = err
	close(attempt.done)
	c.mu.Unlock()
}

// blockNext arranges for the next scheduled attempt for key to remain in
// progress until the returned channel is closed. This makes in-progress and
// concurrency-limit behavior observable without timing assumptions.
func (c *recoveryTestAttempts[K]) blockNext(key K) chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	gate := make(chan struct{})
	c.nextGates[key] = gate
	return gate
}

// failNext arranges for the next scheduled attempt for key to fail before its
// real recovery function runs, allowing retry and retained-error behavior to be
// tested independently of storage failures.
func (c *recoveryTestAttempts[K]) failNext(key K, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextFailures[key] = err
}

// latest returns the most recently scheduled attempt for key. The retrying test
// sender uses its presence to ensure there is known recovery work to wait for;
// if no attempt exists, it preserves the original 503 response.
func (c *recoveryTestAttempts[K]) latest(key K) (*recoveryAttempt, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	attempt, ok := c.attempts[key]
	return attempt, ok
}

// wait blocks until the latest attempt completes and returns the same final
// error observed by recoverManager. A missing attempt is reported immediately
// so a test cannot silently wait for work that was never scheduled.
func (c *recoveryTestAttempts[K]) wait(ctx context.Context, key K) error {
	attempt, ok := c.latest(key)
	if !ok {
		return fmt.Errorf("recovery for %v was not started", key)
	}
	select {
	case <-attempt.done:
		return attempt.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// startCount reports how many attempts were actually scheduled for key. Tests
// use it to verify request deduplication, retries, and concurrency-limit rejects.
func (c *recoveryTestAttempts[K]) startCount(key K) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.starts[key]
}

// recoveryTestControl groups the four independent observations needed by
// command recovery tests: partition attempts, workspace attempts, PLog reads,
// and WLog reads. Keeping these concerns separate avoids workspace-only test
// state leaking into the partition recovery fixture.
type recoveryTestControl struct {
	partitions *recoveryTestAttempts[partitionKey]
	workspaces *recoveryTestAttempts[workspaceKey]
	pLogReads  *recoveryTestReads[partitionKey]
	wLogReads  *recoveryTestReads[workspaceKey]
}

func newRecoveryTestControl() *recoveryTestControl {
	return &recoveryTestControl{
		partitions: newRecoveryTestAttempts[partitionKey](),
		workspaces: newRecoveryTestAttempts[workspaceKey](),
		pLogReads:  newRecoveryTestReads[partitionKey](),
		wLogReads:  newRecoveryTestReads[workspaceKey](),
	}
}

func (c *recoveryTestControl) testHooks() (
	partitionHooks recoveryHooks[partitionKey],
	workspaceHooks recoveryHooks[workspaceKey],
	cmdProcHooks *commandProcessorHooks,
) {
	return c.partitions.hooks(), c.workspaces.hooks(), &commandProcessorHooks{
		pLogRead: c.pLogReads.record,
		wLogRead: c.wLogReads.record,
	}
}

// recoveryRetrySender hides mandatory lazy recovery from command tests whose
// subject is unrelated to recovery. For requests selected by keyForRequest, the
// fixture treats a 503 as recoverable only when that partition has a recorded
// attempt; rawRequestSender remains available to tests that need to assert 503s
// directly instead of applying this test-only assumption.
type recoveryRetrySender struct {
	raw           bus.IRequestSender
	control       *recoveryTestControl
	keyForRequest func(bus.Request) (partitionKey, bool)
}

// SendRequest forwards the request until recovery no longer returns 503. Before
// retrying it drains the response and waits for the known partition attempt, so
// ordinary command tests do not race the initial asynchronous recovery.
func (s *recoveryRetrySender) SendRequest(ctx context.Context, req bus.Request) (<-chan any, bus.ResponseMeta, *error, error) {
	for {
		responseCh, responseMeta, responseErr, err := s.raw.SendRequest(ctx, req)
		key, retryRecovery := s.keyForRequest(req)
		if err != nil || responseMeta.StatusCode != http.StatusServiceUnavailable || !retryRecovery {
			return responseCh, responseMeta, responseErr, err
		}
		if _, ok := s.control.partitions.latest(key); !ok {
			return responseCh, responseMeta, responseErr, err
		}
		for range responseCh {
		}
		if *responseErr != nil {
			return nil, bus.ResponseMeta{}, nil, *responseErr
		}
		if err := s.control.partitions.wait(ctx, key); err != nil {
			return nil, bus.ResponseMeta{}, nil, err
		}
	}
}
