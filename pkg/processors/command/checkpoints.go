/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package commandprocessor

import (
	"context"
	"runtime"
	"sync"
	"time"

	"github.com/voedger/voedger/pkg/goutils/logger"
	"github.com/voedger/voedger/pkg/goutils/timeu"
	"github.com/voedger/voedger/pkg/istructs"
)

const (
	defaultPartitionCheckpointEventCount    = 100
	defaultPartitionCheckpointFlushInterval = time.Minute
	defaultCheckpointRetryInterval          = time.Second
)

type PartitionCheckpoint struct {
	NextPLogOffset istructs.Offset `json:"nextPLogOffset"`
}

type WorkspaceCheckpoint struct {
	NextWLogOffset istructs.Offset   `json:"nextWLogOffset"`
	NextRecordID   istructs.RecordID `json:"nextRecordID"`
}

type IRecoveryCheckpointStorage interface {
	GetPartitionCheckpoint(appID istructs.ClusterAppID, partitionID istructs.PartitionID) (PartitionCheckpoint, bool, error)
	PutPartitionCheckpoint(appID istructs.ClusterAppID, partitionID istructs.PartitionID, checkpoint PartitionCheckpoint) error
	GetWorkspaceCheckpoint(appID istructs.ClusterAppID, wsid istructs.WSID) (WorkspaceCheckpoint, bool, error)
	PutWorkspaceCheckpoint(appID istructs.ClusterAppID, wsid istructs.WSID, checkpoint WorkspaceCheckpoint) error
}

// checkpointSnapshot captures the workspace and partition recovery positions
// produced by a command for asynchronous persistence to the correct targets.
type checkpointSnapshot struct {
	clusterAppID istructs.ClusterAppID
	partitionID  istructs.PartitionID
	wsid         istructs.WSID
	partition    PartitionCheckpoint
	workspace    WorkspaceCheckpoint
}

// checkpointProjectorHooks provides callbacks for observing successful
// checkpoint persistence and scheduled retries.
type checkpointProjectorHooks struct {
	workspacePersisted func(checkpointSnapshot)
	partitionPersisted func(checkpointSnapshot)
	retryScheduled     func(checkpointSnapshot, error)
}

type checkpointProjectorsConfig struct {
	storage                IRecoveryCheckpointStorage
	time                   timeu.ITime
	partitionEventCount    int
	partitionFlushInterval time.Duration
	retryInterval          time.Duration
	hooks                  checkpointProjectorHooks
}

type checkpointPartitionKey struct {
	clusterAppID istructs.ClusterAppID
	partitionID  istructs.PartitionID
}

type checkpointPartitionState struct {
	latest           checkpointSnapshot
	eventsSinceFlush int
	dirty            bool
}

type checkpointProjectors struct {
	ctx    context.Context
	config checkpointProjectorsConfig

	mu        sync.Mutex
	accepting bool
	queue     []checkpointSnapshot
	wake      chan struct{}
	stopping  chan struct{}
	stopOnce  sync.Once
	done      chan struct{}
}

func newCheckpointProjectors(ctx context.Context, config checkpointProjectorsConfig) *checkpointProjectors {
	if config.partitionEventCount == 0 {
		config.partitionEventCount = defaultPartitionCheckpointEventCount
	}
	if config.partitionFlushInterval == 0 {
		config.partitionFlushInterval = defaultPartitionCheckpointFlushInterval
	}
	if config.retryInterval == 0 {
		config.retryInterval = defaultCheckpointRetryInterval
	}
	config.hooks = normalizedCheckpointProjectorHooks(config.hooks)
	projectors := &checkpointProjectors{
		ctx:       ctx,
		config:    config,
		accepting: true,
		wake:      make(chan struct{}, 1),
		stopping:  make(chan struct{}),
		done:      make(chan struct{}),
	}
	go projectors.run()
	return projectors
}

func normalizedCheckpointProjectorHooks(hooks checkpointProjectorHooks) checkpointProjectorHooks {
	if hooks.workspacePersisted == nil {
		hooks.workspacePersisted = func(checkpointSnapshot) {}
	}
	if hooks.partitionPersisted == nil {
		hooks.partitionPersisted = func(checkpointSnapshot) {}
	}
	if hooks.retryScheduled == nil {
		hooks.retryScheduled = func(checkpointSnapshot, error) {}
	}
	return hooks
}

func (p *checkpointProjectors) enqueue(snapshot checkpointSnapshot) bool {
	p.mu.Lock()
	if !p.accepting {
		p.mu.Unlock()
		return false
	}
	p.queue = append(p.queue, snapshot)
	p.mu.Unlock()
	p.signal()
	return true
}

func (p *checkpointProjectors) shutdown() {
	p.stopOnce.Do(func() {
		p.mu.Lock()
		p.accepting = false
		p.mu.Unlock()
		close(p.stopping)
		p.signal()
	})
	<-p.done
}

func (p *checkpointProjectors) signal() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *checkpointProjectors) run() {
	defer close(p.done)
	partitions := map[checkpointPartitionKey]*checkpointPartitionState{}
	timer := p.config.time.NewTimerChan(p.config.partitionFlushInterval)
	for {
		if p.drainQueue(partitions) {
			p.flushAll(partitions, true)
			return
		}
		select {
		case <-p.wake:
		case <-timer:
			p.flushAll(partitions, false)
			timer = p.config.time.NewTimerChan(p.config.partitionFlushInterval)
		case <-p.stopping:
		}
	}
}

// drainQueue returns true after admission has stopped and every admitted
// immutable snapshot has been processed.
func (p *checkpointProjectors) drainQueue(partitions map[checkpointPartitionKey]*checkpointPartitionState) bool {
	for {
		p.mu.Lock()
		if len(p.queue) == 0 {
			stopping := !p.accepting
			p.mu.Unlock()
			return stopping
		}
		snapshot := p.queue[0]
		p.queue[0] = checkpointSnapshot{}
		p.queue = p.queue[1:]
		p.mu.Unlock()

		if !p.persistWorkspace(snapshot) {
			continue
		}
		key := checkpointPartitionKey{clusterAppID: snapshot.clusterAppID, partitionID: snapshot.partitionID}
		state := partitions[key]
		if state == nil {
			state = &checkpointPartitionState{}
			partitions[key] = state
		}
		state.latest = snapshot
		state.eventsSinceFlush++
		state.dirty = true
		if state.eventsSinceFlush >= p.config.partitionEventCount {
			p.flushPartition(state, false)
		}
	}
}

func (p *checkpointProjectors) persistWorkspace(snapshot checkpointSnapshot) bool {
	return p.withRetry(snapshot, func() error {
		err := p.config.storage.PutWorkspaceCheckpoint(snapshot.clusterAppID, snapshot.wsid, snapshot.workspace)
		if err == nil {
			p.config.hooks.workspacePersisted(snapshot)
		}
		return err
	})
}

func (p *checkpointProjectors) flushAll(partitions map[checkpointPartitionKey]*checkpointPartitionState, final bool) {
	for _, state := range partitions {
		p.flushPartition(state, final)
	}
}

func (p *checkpointProjectors) flushPartition(state *checkpointPartitionState, final bool) {
	if !state.dirty {
		return
	}
	snapshot := state.latest
	put := func() error {
		err := p.config.storage.PutPartitionCheckpoint(snapshot.clusterAppID, snapshot.partitionID, snapshot.partition)
		if err == nil {
			p.config.hooks.partitionPersisted(snapshot)
		}
		return err
	}
	var persisted bool
	if final {
		err := put()
		if err != nil {
			logger.Error("final partition recovery checkpoint flush failed: ", err)
		}
		persisted = err == nil
	} else {
		persisted = p.withRetry(snapshot, put)
	}
	if persisted {
		state.dirty = false
		state.eventsSinceFlush = 0
	}
}

func (p *checkpointProjectors) withRetry(snapshot checkpointSnapshot, put func() error) bool {
	for {
		err := put()
		if err == nil {
			return true
		}
		logger.Error("recovery checkpoint persistence failed: ", err)
		p.config.hooks.retryScheduled(snapshot, err)
		retry := p.config.time.NewTimerChan(p.config.retryInterval)
		select {
		case <-retry:
			// Keep retry scheduling observable before the next storage attempt.
			runtime.Gosched()
		case <-p.stopping:
			return put() == nil
		case <-p.ctx.Done():
			return put() == nil
		}
	}
}
