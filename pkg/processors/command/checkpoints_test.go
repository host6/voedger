/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package commandprocessor

import (
	"sync"

	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/sys/checkpoints"
)

type checkpointStorageCallKind string

const (
	checkpointStorageCallPartition checkpointStorageCallKind = "partition"
	checkpointStorageCallWorkspace checkpointStorageCallKind = "workspace"
)

// monotonicCheckpointStorage is used by command recovery tests. Projector
// behavior is tested in pkg/sys/checkpoints; command tests only need a shared,
// monotonic implementation of the recovery storage contract.
type monotonicCheckpointStorage struct {
	mu         sync.Mutex
	partitions map[[2]uint64]checkpoints.PartitionCheckpoint
	workspaces map[[2]uint64]checkpoints.WorkspaceCheckpoint
	writes     []checkpointStorageCallKind
}

func newMonotonicCheckpointStorage() *monotonicCheckpointStorage {
	return &monotonicCheckpointStorage{
		partitions: map[[2]uint64]checkpoints.PartitionCheckpoint{},
		workspaces: map[[2]uint64]checkpoints.WorkspaceCheckpoint{},
	}
}

func (s *monotonicCheckpointStorage) GetPartitionCheckpoint(appID istructs.ClusterAppID, partitionID istructs.PartitionID) (checkpoints.PartitionCheckpoint, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	checkpoint, ok := s.partitions[[2]uint64{uint64(appID), uint64(partitionID)}]
	return checkpoint, ok, nil
}

func (s *monotonicCheckpointStorage) PutPartitionCheckpoint(appID istructs.ClusterAppID, partitionID istructs.PartitionID, checkpoint checkpoints.PartitionCheckpoint) error {
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

func (s *monotonicCheckpointStorage) GetWorkspaceCheckpoint(appID istructs.ClusterAppID, wsid istructs.WSID) (checkpoints.WorkspaceCheckpoint, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	checkpoint, ok := s.workspaces[[2]uint64{uint64(appID), uint64(wsid)}]
	return checkpoint, ok, nil
}

func (s *monotonicCheckpointStorage) PutWorkspaceCheckpoint(appID istructs.ClusterAppID, wsid istructs.WSID, checkpoint checkpoints.WorkspaceCheckpoint) error {
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

func (s *monotonicCheckpointStorage) forcePartition(appID istructs.ClusterAppID, partitionID istructs.PartitionID, checkpoint checkpoints.PartitionCheckpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.partitions[[2]uint64{uint64(appID), uint64(partitionID)}] = checkpoint
}

func (s *monotonicCheckpointStorage) forceWorkspace(appID istructs.ClusterAppID, wsid istructs.WSID, checkpoint checkpoints.WorkspaceCheckpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workspaces[[2]uint64{uint64(appID), uint64(wsid)}] = checkpoint
}

func (s *monotonicCheckpointStorage) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.partitions = map[[2]uint64]checkpoints.PartitionCheckpoint{}
	s.workspaces = map[[2]uint64]checkpoints.WorkspaceCheckpoint{}
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
