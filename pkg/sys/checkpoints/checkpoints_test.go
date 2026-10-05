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

func TestRecoveryCheckpointProjector(t *testing.T) {
	require := require.New(t)
	storage := &testCheckpointStorage{}
	projector := recoveryCheckpointProjector(storage)
	event := &testPLogEvent{
		partition:  4,
		pLogOffset: 10,
		wsid:       5,
		wLogOffset: 20,
	}
	state := &testState{appStructs: &testAppStructs{
		clusterAppID: 7,
	}}

	require.NoError(projector(event, state, nil))
	require.Equal([]string{"workspace", "partition"}, storage.calls)
	require.Equal(istructs.ClusterAppID(7), storage.appID)
	require.Equal(istructs.WSID(5), storage.wsid)
	require.Equal(WorkspaceCheckpoint{LastHandledWLogOffset: 20}, storage.workspace)
	require.Equal(istructs.PartitionID(4), storage.partitionID)
	require.Equal(PartitionCheckpoint{LastHandledPLogOffset: 10}, storage.partition)
}

func TestRecoveryCheckpointProjectorDoesNotAdvancePartitionWhenWorkspaceWriteFails(t *testing.T) {
	require := require.New(t)
	injectedErr := errors.New("injected workspace checkpoint failure")
	storage := &testCheckpointStorage{workspaceErr: injectedErr}
	projector := recoveryCheckpointProjector(storage)
	event := &testPLogEvent{
		partition:  4,
		pLogOffset: 10,
		wsid:       5,
		wLogOffset: 20,
	}
	state := &testState{appStructs: &testAppStructs{
		clusterAppID: 7,
	}}

	require.ErrorIs(projector(event, state, nil), injectedErr)
	require.Equal([]string{"workspace"}, storage.calls)
}

func TestProvideRegistersStandardAsyncProjector(t *testing.T) {
	require := require.New(t)
	resources := istructsmem.NewStatelessResources()
	storage := &testCheckpointStorage{}
	Provide(resources, storage)

	found := false
	resources.Projectors(func(path string, projector istructs.Projector) bool {
		require.Equal(appdef.SysPackagePath, path)
		require.Equal(QNameProjectorRecoveryCheckpoint, projector.Name)
		found = true
		return true
	})
	require.True(found)
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
}

func (e *testPLogEvent) HandlingPartition() istructs.PartitionID { return e.partition }
func (e *testPLogEvent) PLogOffset() istructs.Offset             { return e.pLogOffset }
func (e *testPLogEvent) Workspace() istructs.WSID                { return e.wsid }
func (e *testPLogEvent) WLogOffset() istructs.Offset             { return e.wLogOffset }
