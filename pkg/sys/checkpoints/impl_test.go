/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package checkpoints

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/istructsmem"
)

func TestRecoveryCheckpointProjectorWritesPLogOffsetEachHundredthEvent(t *testing.T) {
	require := require.New(t)
	storage := &testCheckpointStorage{}
	projector := recoveryCheckpointProjector(storage)
	state := &testState{appStructs: &testAppStructs{clusterAppID: 7}}
	event := &testPLogEvent{
		partition:  4,
		pLogOffset: 99,
	}

	require.NoError(projector(event, state, nil))
	require.Empty(storage.calls)

	event.pLogOffset = 100
	require.NoError(projector(event, state, nil))
	require.Equal([]string{"partition"}, storage.calls)
	require.Equal(istructs.ClusterAppID(7), storage.appID)
	require.Equal(istructs.PartitionID(4), storage.partitionID)
	require.Equal(PartitionCheckpoint{LastPLogOffset: 100}, storage.partition)

	event.pLogOffset = 101
	require.NoError(projector(event, state, nil))
	require.Equal([]string{"partition"}, storage.calls)

	event.pLogOffset = 200
	require.NoError(projector(event, state, nil))
	require.Equal([]string{"partition", "partition"}, storage.calls)
	require.Equal(PartitionCheckpoint{LastPLogOffset: 200}, storage.partition)
}

func TestRecoveryCheckpointProjectorWritesOnlyIDBearingWLogOffsets(t *testing.T) {
	require := require.New(t)
	storage := &testCheckpointStorage{}
	projector := recoveryCheckpointProjector(storage)
	state := &testState{appStructs: &testAppStructs{clusterAppID: 7}}
	event := &testPLogEvent{
		pLogOffset: 1,
		wsid:       5,
		wLogOffset: 20,
		cuds: []testCUDRow{
			{id: istructs.FirstSingletonID, isNew: true},
			{id: istructs.FirstUserRecordID, isNew: false},
		},
	}

	require.NoError(projector(event, state, nil))
	require.Empty(storage.calls)

	event.cuds = append(event.cuds, testCUDRow{id: istructs.FirstUserRecordID, isNew: true})
	require.NoError(projector(event, state, nil))
	require.Equal([]string{"workspace"}, storage.calls)
	require.Equal(istructs.ClusterAppID(7), storage.appID)
	require.Equal(istructs.WSID(5), storage.wsid)
	require.Equal(WorkspaceCheckpoint{LastWLogOffsetWithNewRecordIDs: 20}, storage.workspace)

	event.pLogOffset = 2
	event.wsid = 6
	event.wLogOffset = 21
	event.cuds = nil
	require.NoError(projector(event, state, nil))
	require.Equal([]string{"workspace"}, storage.calls)

	event.pLogOffset = 3
	event.wLogOffset = 22
	event.cuds = []testCUDRow{{id: istructs.FirstUserRecordID + 1, isNew: true}}
	require.NoError(projector(event, state, nil))
	require.Equal([]string{"workspace", "workspace"}, storage.calls)
	require.Equal(istructs.WSID(6), storage.wsid)
	require.Equal(WorkspaceCheckpoint{LastWLogOffsetWithNewRecordIDs: 22}, storage.workspace)
}

func TestRecoveryCheckpointProjectorReturnsStorageErrors(t *testing.T) {
	require := require.New(t)
	partitionErr := errors.New("injected partition checkpoint failure")
	workspaceErr := errors.New("injected workspace checkpoint failure")
	storage := &testCheckpointStorage{partitionErr: partitionErr, workspaceErr: workspaceErr}
	state := &testState{appStructs: &testAppStructs{clusterAppID: 7}}
	event := &testPLogEvent{pLogOffset: 100}

	require.ErrorIs(recoveryCheckpointProjector(storage)(event, state, nil), partitionErr)

	event.pLogOffset = 101
	event.wLogOffset = 10
	event.cuds = []testCUDRow{{id: istructs.FirstUserRecordID, isNew: true}}
	require.ErrorIs(recoveryCheckpointProjector(storage)(event, state, nil), workspaceErr)
}

func TestProvideRegistersOneStandardAsyncProjector(t *testing.T) {
	require := require.New(t)
	resources := istructsmem.NewStatelessResources()
	storage := &testCheckpointStorage{}
	Provide(resources, storage)

	found := map[appdef.QName]bool{}
	resources.Projectors(func(path string, projector istructs.Projector) bool {
		require.Equal(appdef.SysPackagePath, path)
		found[projector.Name] = true
		return true
	})
	require.Equal(map[appdef.QName]bool{QNameProjectorRecoveryCheckpoint: true}, found)
}

type testCheckpointStorage struct {
	calls        []string
	workspaceErr error
	partitionErr error
	appID        istructs.ClusterAppID
	partitionID  istructs.PartitionID
	wsid         istructs.WSID
	partition    PartitionCheckpoint
	workspace    WorkspaceCheckpoint
}

func (*testCheckpointStorage) GetPartitionCheckpoint(istructs.ClusterAppID, istructs.PartitionID) (PartitionCheckpoint, bool, error) {
	return PartitionCheckpoint{}, false, nil
}

func (s *testCheckpointStorage) PutPartitionCheckpoint(appID istructs.ClusterAppID, partitionID istructs.PartitionID, checkpoint PartitionCheckpoint) error {
	s.calls = append(s.calls, "partition")
	s.appID = appID
	s.partitionID = partitionID
	s.partition = checkpoint
	return s.partitionErr
}

func (*testCheckpointStorage) GetWorkspaceCheckpoint(istructs.ClusterAppID, istructs.WSID) (WorkspaceCheckpoint, bool, error) {
	return WorkspaceCheckpoint{}, false, nil
}

func (s *testCheckpointStorage) PutWorkspaceCheckpoint(appID istructs.ClusterAppID, wsid istructs.WSID, checkpoint WorkspaceCheckpoint) error {
	s.calls = append(s.calls, "workspace")
	s.appID = appID
	s.wsid = wsid
	s.workspace = checkpoint
	return s.workspaceErr
}

type testState struct {
	istructs.IState
	appStructs istructs.IAppStructs
}

func (s *testState) AppStructs() istructs.IAppStructs { return s.appStructs }

type testAppStructs struct {
	istructs.IAppStructs
	clusterAppID istructs.ClusterAppID
}

func (s *testAppStructs) ClusterAppID() istructs.ClusterAppID { return s.clusterAppID }

type testPLogEvent struct {
	istructs.IPLogEvent
	partition  istructs.PartitionID
	pLogOffset istructs.Offset
	wsid       istructs.WSID
	wLogOffset istructs.Offset
	cuds       []testCUDRow
}

func (e *testPLogEvent) HandlingPartition() istructs.PartitionID { return e.partition }
func (e *testPLogEvent) PLogOffset() istructs.Offset             { return e.pLogOffset }
func (e *testPLogEvent) Workspace() istructs.WSID                { return e.wsid }
func (e *testPLogEvent) WLogOffset() istructs.Offset             { return e.wLogOffset }
func (e *testPLogEvent) CUDs(cb func(istructs.ICUDRow) bool) {
	for i := range e.cuds {
		if !cb(&e.cuds[i]) {
			return
		}
	}
}

type testCUDRow struct {
	istructs.ICUDRow
	id    istructs.RecordID
	isNew bool
}

func (r *testCUDRow) ID() istructs.RecordID { return r.id }
func (r *testCUDRow) IsNew() bool           { return r.isNew }
