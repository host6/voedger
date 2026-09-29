/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package commandprocessor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/goutils/testingu"
	"github.com/voedger/voedger/pkg/goutils/timeu"
	"github.com/voedger/voedger/pkg/istructs"
)

func TestCheckpointProjectorsPerOperationAndCadence(t *testing.T) {
	require := require.New(t)
	mockTime := testingu.NewMockTime()
	timerArmed := make(chan struct{})
	mockTime.SetOnNextTimerArmed(func() { close(timerArmed) })
	storage := newControlledCheckpointStorage()
	workspacePersisted := make(chan checkpointSnapshot, 100)
	partitionPersisted := make(chan checkpointSnapshot, 2)
	retryScheduled := make(chan checkpointSnapshot, 1)
	projectors := newCheckpointProjectorsForTest(context.Background(), storage, mockTime,
		checkpointProjectorHooks{
			workspacePersisted: func(snapshot checkpointSnapshot) { workspacePersisted <- snapshot },
			partitionPersisted: func(snapshot checkpointSnapshot) { partitionPersisted <- snapshot },
			retryScheduled:     func(snapshot checkpointSnapshot, _ error) { retryScheduled <- snapshot },
		})
	<-timerArmed

	for event := 1; event <= 100; event++ {
		snapshot := checkpointSnapshot{
			clusterAppID: 11,
			partitionID:  2,
			wsid:         istructs.WSID(event),
			partition:    PartitionCheckpoint{NextPLogOffset: istructs.Offset(event + 1)},
			workspace: WorkspaceCheckpoint{
				NextWLogOffset: istructs.Offset(event + 10),
				NextRecordID:   istructs.RecordID(event + 100),
			},
		}
		require.True(projectors.enqueue(snapshot))

		call := nextCheckpointStorageCall(t, storage.calls)
		require.Equal(checkpointStorageCallWorkspace, call.kind)
		require.Equal(snapshot.clusterAppID, call.clusterAppID)
		require.Equal(snapshot.wsid, call.wsid)
		require.Equal(snapshot.workspace, call.workspace)
		call.complete(nil)
		require.Equal(snapshot, <-workspacePersisted)

		if event < 100 {
			assertNoCheckpointStorageCall(t, storage.calls)
		}
	}

	partitionCall := nextCheckpointStorageCall(t, storage.calls)
	require.Equal(checkpointStorageCallPartition, partitionCall.kind)
	require.Equal(PartitionCheckpoint{NextPLogOffset: 101}, partitionCall.partition)
	mockTime.FireNextTimerImmediately()
	partitionCall.complete(errors.New("injected partition checkpoint failure"))
	require.Equal(istructs.Offset(101), (<-retryScheduled).partition.NextPLogOffset)
	partitionRetryCall := nextCheckpointStorageCall(t, storage.calls)
	require.Equal(checkpointStorageCallPartition, partitionRetryCall.kind)
	require.Equal(partitionCall.partition, partitionRetryCall.partition)
	partitionRetryCall.complete(nil)
	require.Equal(istructs.Offset(101), (<-partitionPersisted).partition.NextPLogOffset)

	shutdownCheckpointProjectors(t, projectors, storage.calls)
}

func TestCheckpointProjectorsTimeCadence(t *testing.T) {
	require := require.New(t)
	mockTime := testingu.NewMockTime()
	timerArmed := make(chan struct{})
	mockTime.SetOnNextTimerArmed(func() { close(timerArmed) })
	storage := newControlledCheckpointStorage()
	workspacePersisted := make(chan checkpointSnapshot, 1)
	projectors := newCheckpointProjectorsForTest(context.Background(), storage, mockTime,
		checkpointProjectorHooks{
			workspacePersisted: func(snapshot checkpointSnapshot) { workspacePersisted <- snapshot },
		})
	<-timerArmed

	snapshot := checkpointSnapshot{
		clusterAppID: 12,
		partitionID:  3,
		wsid:         1201,
		partition:    PartitionCheckpoint{NextPLogOffset: 22},
		workspace:    WorkspaceCheckpoint{NextWLogOffset: 32, NextRecordID: 42},
	}
	require.True(projectors.enqueue(snapshot))
	workspaceCall := nextCheckpointStorageCall(t, storage.calls)
	require.Equal(checkpointStorageCallWorkspace, workspaceCall.kind)
	workspaceCall.complete(nil)
	<-workspacePersisted
	assertNoCheckpointStorageCall(t, storage.calls)

	mockTime.Add(time.Minute)
	partitionCall := nextCheckpointStorageCall(t, storage.calls)
	require.Equal(checkpointStorageCallPartition, partitionCall.kind)
	require.Equal(snapshot.partition, partitionCall.partition)
	partitionCall.complete(nil)

	shutdownCheckpointProjectors(t, projectors, storage.calls)
}

func TestCheckpointProjectorsOrderingRetryAndFinalFlush(t *testing.T) {
	require := require.New(t)
	mockTime := testingu.NewMockTime()
	storage := newControlledCheckpointStorage()
	retryScheduled := make(chan checkpointSnapshot, 1)
	workspacePersisted := make(chan checkpointSnapshot, 100)
	projectors := newCheckpointProjectorsForTest(context.Background(), storage, mockTime,
		checkpointProjectorHooks{
			retryScheduled:     func(snapshot checkpointSnapshot, _ error) { retryScheduled <- snapshot },
			workspacePersisted: func(snapshot checkpointSnapshot) { workspacePersisted <- snapshot },
		})

	// The 100th covered event makes partition persistence due. Keep its
	// workspace write gated to prove that the partition barrier cannot pass it.
	for event := 1; event <= 100; event++ {
		snapshot := checkpointSnapshot{
			clusterAppID: 13,
			partitionID:  4,
			wsid:         1301,
			partition:    PartitionCheckpoint{NextPLogOffset: istructs.Offset(event + 1)},
			workspace:    WorkspaceCheckpoint{NextWLogOffset: istructs.Offset(event + 1), NextRecordID: istructs.RecordID(event + 100)},
		}
		require.True(projectors.enqueue(snapshot))
		call := nextCheckpointStorageCall(t, storage.calls)
		require.Equal(checkpointStorageCallWorkspace, call.kind)
		if event < 100 {
			call.complete(nil)
			<-workspacePersisted
			continue
		}

		assertNoCheckpointStorageCall(t, storage.calls)
		injectedErr := errors.New("injected workspace checkpoint failure")
		mockTime.FireNextTimerImmediately()
		call.complete(injectedErr)
		require.Equal(snapshot, <-retryScheduled)
		assertNoCheckpointStorageCall(t, storage.calls)

		retryCall := nextCheckpointStorageCall(t, storage.calls)
		require.Equal(checkpointStorageCallWorkspace, retryCall.kind)
		require.Equal(snapshot.workspace, retryCall.workspace)
		retryCall.complete(nil)
		require.Equal(snapshot, <-workspacePersisted)
	}

	partitionCall := nextCheckpointStorageCall(t, storage.calls)
	require.Equal(checkpointStorageCallPartition, partitionCall.kind)
	require.Equal(PartitionCheckpoint{NextPLogOffset: 101}, partitionCall.partition)
	partitionCall.complete(nil)

	// One more operation is below both periodic thresholds. Orderly shutdown
	// must stop admission, persist its workspace, and then flush the partition.
	finalSnapshot := checkpointSnapshot{
		clusterAppID: 13,
		partitionID:  4,
		wsid:         1301,
		partition:    PartitionCheckpoint{NextPLogOffset: 102},
		workspace:    WorkspaceCheckpoint{NextWLogOffset: 102, NextRecordID: 202},
	}
	require.True(projectors.enqueue(finalSnapshot))
	finalWorkspaceCall := nextCheckpointStorageCall(t, storage.calls)
	require.Equal(checkpointStorageCallWorkspace, finalWorkspaceCall.kind)
	finalWorkspaceCall.complete(nil)
	require.Equal(finalSnapshot, <-workspacePersisted)

	shutdownDone := make(chan struct{})
	go func() {
		projectors.shutdown()
		close(shutdownDone)
	}()
	finalPartitionCall := nextCheckpointStorageCall(t, storage.calls)
	require.Equal(checkpointStorageCallPartition, finalPartitionCall.kind)
	require.Equal(finalSnapshot.partition, finalPartitionCall.partition)
	finalPartitionCall.complete(nil)
	<-shutdownDone
	require.False(projectors.enqueue(finalSnapshot))
}

func TestCheckpointProjectorsOverlappingWritersDoNotRegress(t *testing.T) {
	require := require.New(t)
	storage := newMonotonicCheckpointStorage()

	newProjector := func(persisted chan checkpointSnapshot) *checkpointProjectors {
		return newCheckpointProjectorsForTest(context.Background(), storage, testingu.NewMockTime(),
			checkpointProjectorHooks{
				workspacePersisted: func(snapshot checkpointSnapshot) { persisted <- snapshot },
			})
	}
	newerPersisted := make(chan checkpointSnapshot, 1)
	stalePersisted := make(chan checkpointSnapshot, 1)
	newerWriter := newProjector(newerPersisted)
	staleWriter := newProjector(stalePersisted)

	newer := checkpointSnapshot{
		clusterAppID: 14,
		partitionID:  5,
		wsid:         1401,
		partition:    PartitionCheckpoint{NextPLogOffset: 300},
		workspace:    WorkspaceCheckpoint{NextWLogOffset: 400, NextRecordID: 500},
	}
	stale := checkpointSnapshot{
		clusterAppID: 14,
		partitionID:  5,
		wsid:         1401,
		partition:    PartitionCheckpoint{NextPLogOffset: 299},
		workspace:    WorkspaceCheckpoint{NextWLogOffset: 399, NextRecordID: 499},
	}
	require.True(newerWriter.enqueue(newer))
	require.Equal(newer, <-newerPersisted)
	require.True(staleWriter.enqueue(stale))
	require.Equal(stale, <-stalePersisted)
	newerWriter.shutdown()
	staleWriter.shutdown()

	partition, ok, err := storage.GetPartitionCheckpoint(newer.clusterAppID, newer.partitionID)
	require.NoError(err)
	require.True(ok)
	require.Equal(newer.partition, partition)
	workspace, ok, err := storage.GetWorkspaceCheckpoint(newer.clusterAppID, newer.wsid)
	require.NoError(err)
	require.True(ok)
	require.Equal(newer.workspace, workspace)
}

type checkpointStorageCallKind string

const (
	checkpointStorageCallPartition checkpointStorageCallKind = "partition"
	checkpointStorageCallWorkspace checkpointStorageCallKind = "workspace"
)

type checkpointStorageCall struct {
	kind         checkpointStorageCallKind
	clusterAppID istructs.ClusterAppID
	partitionID  istructs.PartitionID
	wsid         istructs.WSID
	partition    PartitionCheckpoint
	workspace    WorkspaceCheckpoint
	result       chan error
}

func (c checkpointStorageCall) complete(err error) {
	c.result <- err
}

type controlledCheckpointStorage struct {
	calls chan checkpointStorageCall
}

func newControlledCheckpointStorage() *controlledCheckpointStorage {
	return &controlledCheckpointStorage{calls: make(chan checkpointStorageCall, 1)}
}

func newCheckpointProjectorsForTest(ctx context.Context, storage IRecoveryCheckpointStorage, tm timeu.ITime,
	hooks checkpointProjectorHooks) *checkpointProjectors {
	return newCheckpointProjectors(ctx, checkpointProjectorsConfig{
		storage:                storage,
		time:                   tm,
		partitionEventCount:    100,
		partitionFlushInterval: time.Minute,
		retryInterval:          time.Second,
		hooks:                  hooks,
	})
}

func (s *controlledCheckpointStorage) GetPartitionCheckpoint(istructs.ClusterAppID, istructs.PartitionID) (PartitionCheckpoint, bool, error) {
	return PartitionCheckpoint{}, false, nil
}

func (s *controlledCheckpointStorage) PutPartitionCheckpoint(appID istructs.ClusterAppID, partitionID istructs.PartitionID, checkpoint PartitionCheckpoint) error {
	result := make(chan error)
	s.calls <- checkpointStorageCall{
		kind:         checkpointStorageCallPartition,
		clusterAppID: appID,
		partitionID:  partitionID,
		partition:    checkpoint,
		result:       result,
	}
	return <-result
}

func (s *controlledCheckpointStorage) GetWorkspaceCheckpoint(istructs.ClusterAppID, istructs.WSID) (WorkspaceCheckpoint, bool, error) {
	return WorkspaceCheckpoint{}, false, nil
}

func (s *controlledCheckpointStorage) PutWorkspaceCheckpoint(appID istructs.ClusterAppID, wsid istructs.WSID, checkpoint WorkspaceCheckpoint) error {
	result := make(chan error)
	s.calls <- checkpointStorageCall{
		kind:         checkpointStorageCallWorkspace,
		clusterAppID: appID,
		wsid:         wsid,
		workspace:    checkpoint,
		result:       result,
	}
	return <-result
}

type monotonicCheckpointStorage struct {
	mu         sync.Mutex
	partitions map[[2]uint64]PartitionCheckpoint
	workspaces map[[2]uint64]WorkspaceCheckpoint
	writes     []checkpointStorageCallKind
}

func newMonotonicCheckpointStorage() *monotonicCheckpointStorage {
	return &monotonicCheckpointStorage{
		partitions: map[[2]uint64]PartitionCheckpoint{},
		workspaces: map[[2]uint64]WorkspaceCheckpoint{},
	}
}

func (s *monotonicCheckpointStorage) GetPartitionCheckpoint(appID istructs.ClusterAppID, partitionID istructs.PartitionID) (PartitionCheckpoint, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	checkpoint, ok := s.partitions[[2]uint64{uint64(appID), uint64(partitionID)}]
	return checkpoint, ok, nil
}

func (s *monotonicCheckpointStorage) PutPartitionCheckpoint(appID istructs.ClusterAppID, partitionID istructs.PartitionID, checkpoint PartitionCheckpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := [2]uint64{uint64(appID), uint64(partitionID)}
	current := s.partitions[key]
	if checkpoint.NextPLogOffset > current.NextPLogOffset {
		current.NextPLogOffset = checkpoint.NextPLogOffset
	}
	s.partitions[key] = current
	s.writes = append(s.writes, checkpointStorageCallPartition)
	return nil
}

func (s *monotonicCheckpointStorage) GetWorkspaceCheckpoint(appID istructs.ClusterAppID, wsid istructs.WSID) (WorkspaceCheckpoint, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	checkpoint, ok := s.workspaces[[2]uint64{uint64(appID), uint64(wsid)}]
	return checkpoint, ok, nil
}

func (s *monotonicCheckpointStorage) PutWorkspaceCheckpoint(appID istructs.ClusterAppID, wsid istructs.WSID, checkpoint WorkspaceCheckpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := [2]uint64{uint64(appID), uint64(wsid)}
	current := s.workspaces[key]
	if checkpoint.NextWLogOffset > current.NextWLogOffset {
		current.NextWLogOffset = checkpoint.NextWLogOffset
	}
	if checkpoint.NextRecordID > current.NextRecordID {
		current.NextRecordID = checkpoint.NextRecordID
	}
	s.workspaces[key] = current
	s.writes = append(s.writes, checkpointStorageCallWorkspace)
	return nil
}

func (s *monotonicCheckpointStorage) forcePartition(appID istructs.ClusterAppID, partitionID istructs.PartitionID, checkpoint PartitionCheckpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.partitions[[2]uint64{uint64(appID), uint64(partitionID)}] = checkpoint
}

func (s *monotonicCheckpointStorage) forceWorkspace(appID istructs.ClusterAppID, wsid istructs.WSID, checkpoint WorkspaceCheckpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workspaces[[2]uint64{uint64(appID), uint64(wsid)}] = checkpoint
}

func (s *monotonicCheckpointStorage) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.partitions = map[[2]uint64]PartitionCheckpoint{}
	s.workspaces = map[[2]uint64]WorkspaceCheckpoint{}
	s.writes = nil
}

func (s *monotonicCheckpointStorage) resetWrites() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes = nil
}

func (s *monotonicCheckpointStorage) writtenKinds() []checkpointStorageCallKind {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]checkpointStorageCallKind(nil), s.writes...)
}

func nextCheckpointStorageCall(t *testing.T, calls <-chan checkpointStorageCall) checkpointStorageCall {
	t.Helper()
	return <-calls
}

func assertNoCheckpointStorageCall(t *testing.T, calls <-chan checkpointStorageCall) {
	t.Helper()
	select {
	case call := <-calls:
		t.Fatalf("unexpected %s checkpoint write", call.kind)
	default:
	}
}

func shutdownCheckpointProjectors(t *testing.T, projectors *checkpointProjectors, calls <-chan checkpointStorageCall) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		projectors.shutdown()
		close(done)
	}()
	select {
	case call := <-calls:
		if call.kind != checkpointStorageCallPartition {
			t.Fatalf("shutdown wrote %s checkpoint before partition checkpoint", call.kind)
		}
		call.complete(nil)
		<-done
	case <-done:
	}
}
