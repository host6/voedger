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

// testCheckpointStorage is a thread-safe, last-write-wins implementation of
// the recovery storage contract used by command recovery tests.
type testCheckpointStorage struct {
	mu         sync.Mutex
	partitions map[[2]uint64]checkpoints.PartitionCheckpoint
	workspaces map[[2]uint64]checkpoints.WorkspaceCheckpoint
	writes     []checkpointStorageCallKind
}

func newTestCheckpointStorage() *testCheckpointStorage {
	return &testCheckpointStorage{
		partitions: map[[2]uint64]checkpoints.PartitionCheckpoint{},
		workspaces: map[[2]uint64]checkpoints.WorkspaceCheckpoint{},
	}
}

func (s *testCheckpointStorage) GetPartitionCheckpoint(appID istructs.ClusterAppID, partitionID istructs.PartitionID) (checkpoints.PartitionCheckpoint, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	checkpoint, ok := s.partitions[[2]uint64{uint64(appID), uint64(partitionID)}]
	return checkpoint, ok, nil
}

func (s *testCheckpointStorage) PutPartitionCheckpoint(appID istructs.ClusterAppID, partitionID istructs.PartitionID, checkpoint checkpoints.PartitionCheckpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := [2]uint64{uint64(appID), uint64(partitionID)}
	s.partitions[key] = checkpoint
	s.writes = append(s.writes, checkpointStorageCallPartition)
	return nil
}

func (s *testCheckpointStorage) GetWorkspaceCheckpoint(appID istructs.ClusterAppID, wsid istructs.WSID) (checkpoints.WorkspaceCheckpoint, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	checkpoint, ok := s.workspaces[[2]uint64{uint64(appID), uint64(wsid)}]
	return checkpoint, ok, nil
}

func (s *testCheckpointStorage) PutWorkspaceCheckpoint(appID istructs.ClusterAppID, wsid istructs.WSID, checkpoint checkpoints.WorkspaceCheckpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := [2]uint64{uint64(appID), uint64(wsid)}
	s.workspaces[key] = checkpoint
	s.writes = append(s.writes, checkpointStorageCallWorkspace)
	return nil
}

func (s *testCheckpointStorage) forcePartition(appID istructs.ClusterAppID, partitionID istructs.PartitionID, checkpoint checkpoints.PartitionCheckpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.partitions[[2]uint64{uint64(appID), uint64(partitionID)}] = checkpoint
}

func (s *testCheckpointStorage) forceWorkspace(appID istructs.ClusterAppID, wsid istructs.WSID, checkpoint checkpoints.WorkspaceCheckpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workspaces[[2]uint64{uint64(appID), uint64(wsid)}] = checkpoint
}

func (s *testCheckpointStorage) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.partitions = map[[2]uint64]checkpoints.PartitionCheckpoint{}
	s.workspaces = map[[2]uint64]checkpoints.WorkspaceCheckpoint{}
	s.writes = nil
}

func (s *testCheckpointStorage) resetWrites() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes = nil
}

func (s *testCheckpointStorage) writtenKinds() []checkpointStorageCallKind {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]checkpointStorageCallKind(nil), s.writes...)
}
