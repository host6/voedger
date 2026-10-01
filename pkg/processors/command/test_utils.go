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

type commandStoreStage string

const (
	commandStoreStagePLog           commandStoreStage = "PLog"
	commandStoreStageApplyRecords   commandStoreStage = "apply records"
	commandStoreStageSyncProjectors commandStoreStage = "sync projectors"
	commandStoreStageWLog           commandStoreStage = "WLog"
)

type pLogRead struct {
	offset istructs.Offset
	count  int
}

// recoveryHooks provides deterministic synchronization points for package tests.
// Production command processors use nopHooks().
type recoveryHooks struct {
	scheduled                 func(partitionKey)
	beforeAttempt             func(context.Context, partitionKey) error
	attemptCompleted          func(partitionKey, error)
	workspaceScheduled        func(workspaceKey)
	beforeWorkspaceAttempt    func(context.Context, workspaceKey) error
	workspaceAttemptCompleted func(workspaceKey, error)
	pLogRead                  func(partitionKey, istructs.Offset, int)
	beforeCommandStoreStage   func(commandStoreStage) error
}

type partitionRecoveryHooks = recoveryHooks

type recoveryAttempt struct {
	done    chan struct{}
	started chan struct{}
	gate    <-chan struct{}
	err     error
	active  bool
}

type recoveryTestControl struct {
	mu sync.Mutex

	attempts     map[partitionKey]*recoveryAttempt
	starts       map[partitionKey]int
	nextGates    map[partitionKey]<-chan struct{}
	nextFailures map[partitionKey]error
	pLogReadLog  map[partitionKey][]pLogRead

	workspaceAttempts      map[workspaceKey]*recoveryAttempt
	workspaceStarts        map[workspaceKey]int
	workspaceAttemptStarts map[workspaceKey]int
	nextWorkspaceGates     map[workspaceKey]<-chan struct{}
	nextWorkspaceFailures  map[workspaceKey]error
	workspaceActive        map[partitionKey]int
	workspaceMaxActive     map[partitionKey]int

	stageFailures map[commandStoreStage]error
	changed       chan struct{}
}

func newRecoveryTestControl() *recoveryTestControl {
	return &recoveryTestControl{
		attempts:               map[partitionKey]*recoveryAttempt{},
		starts:                 map[partitionKey]int{},
		nextGates:              map[partitionKey]<-chan struct{}{},
		nextFailures:           map[partitionKey]error{},
		pLogReadLog:            map[partitionKey][]pLogRead{},
		workspaceAttempts:      map[workspaceKey]*recoveryAttempt{},
		workspaceStarts:        map[workspaceKey]int{},
		workspaceAttemptStarts: map[workspaceKey]int{},
		nextWorkspaceGates:     map[workspaceKey]<-chan struct{}{},
		nextWorkspaceFailures:  map[workspaceKey]error{},
		workspaceActive:        map[partitionKey]int{},
		workspaceMaxActive:     map[partitionKey]int{},
		stageFailures:          map[commandStoreStage]error{},
		changed:                make(chan struct{}),
	}
}

func (c *recoveryTestControl) testHooks() *recoveryHooks {
	return &recoveryHooks{
		scheduled:                 c.recoveryStarted,
		beforeAttempt:             c.beforeRecovery,
		attemptCompleted:          c.recoveryFinished,
		workspaceScheduled:        c.workspaceRecoveryScheduled,
		beforeWorkspaceAttempt:    c.beforeWorkspaceRecovery,
		workspaceAttemptCompleted: c.workspaceRecoveryFinished,
		pLogRead:                  c.recordPLogRead,
		beforeCommandStoreStage:   c.beforeStoreStage,
	}
}

func (c *recoveryTestControl) signalLocked() {
	close(c.changed)
	c.changed = make(chan struct{})
}

func (c *recoveryTestControl) recoveryStarted(key partitionKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.starts[key]++
	c.attempts[key] = &recoveryAttempt{
		done:    make(chan struct{}),
		started: make(chan struct{}),
		gate:    c.nextGates[key],
		err:     c.nextFailures[key],
	}
	delete(c.nextGates, key)
	delete(c.nextFailures, key)
}

func (c *recoveryTestControl) beforeRecovery(ctx context.Context, key partitionKey) error {
	c.mu.Lock()
	attempt := c.attempts[key]
	close(attempt.started)
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

func (c *recoveryTestControl) recoveryFinished(key partitionKey, err error) {
	c.mu.Lock()
	attempt := c.attempts[key]
	attempt.err = err
	close(attempt.done)
	c.signalLocked()
	c.mu.Unlock()
}

func (c *recoveryTestControl) blockNext(key partitionKey) chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	gate := make(chan struct{})
	c.nextGates[key] = gate
	return gate
}

func (c *recoveryTestControl) failNext(key partitionKey, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextFailures[key] = err
}

func (c *recoveryTestControl) latestAttempt(key partitionKey) (*recoveryAttempt, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	attempt, ok := c.attempts[key]
	return attempt, ok
}

func (c *recoveryTestControl) wait(ctx context.Context, key partitionKey) error {
	attempt, ok := c.latestAttempt(key)
	if !ok {
		return fmt.Errorf("recovery for %s partition %d was not started", key.appQName, key.partitionID)
	}
	select {
	case <-attempt.done:
		return attempt.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *recoveryTestControl) startCount(key partitionKey) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.starts[key]
}

func (c *recoveryTestControl) workspaceRecoveryScheduled(key workspaceKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.workspaceStarts[key]++
	c.workspaceAttempts[key] = &recoveryAttempt{
		done:    make(chan struct{}),
		started: make(chan struct{}),
		gate:    c.nextWorkspaceGates[key],
		err:     c.nextWorkspaceFailures[key],
	}
	delete(c.nextWorkspaceGates, key)
	delete(c.nextWorkspaceFailures, key)
	c.signalLocked()
}

func (c *recoveryTestControl) beforeWorkspaceRecovery(ctx context.Context, key workspaceKey) error {
	c.mu.Lock()
	attempt := c.workspaceAttempts[key]
	c.workspaceAttemptStarts[key]++
	attempt.active = true
	c.workspaceActive[key.partitionKey]++
	if c.workspaceActive[key.partitionKey] > c.workspaceMaxActive[key.partitionKey] {
		c.workspaceMaxActive[key.partitionKey] = c.workspaceActive[key.partitionKey]
	}
	close(attempt.started)
	c.signalLocked()
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

func (c *recoveryTestControl) workspaceRecoveryFinished(key workspaceKey, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	attempt := c.workspaceAttempts[key]
	if attempt == nil {
		return
	}
	if attempt.active {
		attempt.active = false
		c.workspaceActive[key.partitionKey]--
	}
	attempt.err = err
	close(attempt.done)
	c.signalLocked()
}

func (c *recoveryTestControl) blockNextWorkspace(key workspaceKey) chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	gate := make(chan struct{})
	c.nextWorkspaceGates[key] = gate
	return gate
}

func (c *recoveryTestControl) failNextWorkspace(key workspaceKey, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextWorkspaceFailures[key] = err
}

func (c *recoveryTestControl) waitWorkspaceStarted(ctx context.Context, key workspaceKey) {
	for {
		c.mu.Lock()
		attempt := c.workspaceAttempts[key]
		changed := c.changed
		var started <-chan struct{}
		if attempt != nil {
			started = attempt.started
		}
		c.mu.Unlock()
		if started != nil {
			select {
			case <-started:
				return
			case <-ctx.Done():
				return
			}
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return
		}
	}
}

func (c *recoveryTestControl) waitWorkspace(ctx context.Context, key workspaceKey) error {
	for {
		c.mu.Lock()
		attempt := c.workspaceAttempts[key]
		changed := c.changed
		c.mu.Unlock()
		if attempt != nil {
			select {
			case <-attempt.done:
				return attempt.err
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (c *recoveryTestControl) workspaceStartCount(key workspaceKey) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.workspaceStarts[key]
}

func (c *recoveryTestControl) workspaceAttemptCount(key workspaceKey) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.workspaceAttemptStarts[key]
}

func (c *recoveryTestControl) waitWorkspaceActive(ctx context.Context, key partitionKey, expected int) {
	for {
		c.mu.Lock()
		active := c.workspaceActive[key]
		changed := c.changed
		c.mu.Unlock()
		if active >= expected {
			return
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return
		}
	}
}

func (c *recoveryTestControl) maxActiveWorkspaces(key partitionKey) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.workspaceMaxActive[key]
}

func (c *recoveryTestControl) recordPLogRead(key partitionKey, offset istructs.Offset, count int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pLogReadLog[key] = append(c.pLogReadLog[key], pLogRead{offset: offset, count: count})
}

func (c *recoveryTestControl) resetPLogReads(key partitionKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pLogReadLog, key)
}

func (c *recoveryTestControl) pLogReads(key partitionKey) []pLogRead {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]pLogRead(nil), c.pLogReadLog[key]...)
}

func (c *recoveryTestControl) beforeStoreStage(stage commandStoreStage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	err := c.stageFailures[stage]
	delete(c.stageFailures, stage)
	return err
}

func (c *recoveryTestControl) failNextCommandStoreStage(stage commandStoreStage, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stageFailures[stage] = err
}

type recoveryRetrySender struct {
	raw           bus.IRequestSender
	control       *recoveryTestControl
	keyForRequest func(bus.Request) (partitionKey, bool)
}

func (s *recoveryRetrySender) SendRequest(ctx context.Context, req bus.Request) (<-chan any, bus.ResponseMeta, *error, error) {
	for {
		responseCh, responseMeta, responseErr, err := s.raw.SendRequest(ctx, req)
		key, retryRecovery := s.keyForRequest(req)
		if err != nil || responseMeta.StatusCode != http.StatusServiceUnavailable || !retryRecovery {
			return responseCh, responseMeta, responseErr, err
		}
		if _, ok := s.control.latestAttempt(key); !ok {
			return responseCh, responseMeta, responseErr, err
		}
		for range responseCh {
		}
		if *responseErr != nil {
			return nil, bus.ResponseMeta{}, nil, *responseErr
		}
		if err := s.control.wait(ctx, key); err != nil {
			return nil, bus.ResponseMeta{}, nil, err
		}
	}
}
