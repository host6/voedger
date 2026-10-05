/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package storage

import (
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/goutils/testingu"
	"github.com/voedger/voedger/pkg/istorage/mem"
	"github.com/voedger/voedger/pkg/istorage/provider"
	"github.com/voedger/voedger/pkg/istructs"
	recoverycheckpoints "github.com/voedger/voedger/pkg/sys/checkpoints"
)

func TestRecoveryCheckpointStorage(t *testing.T) {
	require := require.New(t)
	sysVVMStorage, checkpoints := newRecoveryCheckpointStorageForTest(t)

	const (
		appID       = istructs.ClusterAppID(101)
		partitionID = istructs.PartitionID(7)
		wsid        = istructs.WSID(7001)
	)

	t.Run("missing checkpoints are absent", func(t *testing.T) {
		partition, ok, err := checkpoints.GetPartitionCheckpoint(appID, partitionID)
		require.NoError(err)
		require.False(ok)
		require.Zero(partition)

		workspace, ok, err := checkpoints.GetWorkspaceCheckpoint(appID, wsid)
		require.NoError(err)
		require.False(ok)
		require.Zero(workspace)
	})

	t.Run("values contain their typed handled offset", func(t *testing.T) {
		partition := recoverycheckpoints.PartitionCheckpoint{LastHandledPLogOffset: 42}
		workspace := recoverycheckpoints.WorkspaceCheckpoint{LastHandledWLogOffset: 43}

		require.NoError(checkpoints.PutPartitionCheckpoint(appID, partitionID, partition))
		require.NoError(checkpoints.PutWorkspaceCheckpoint(appID, wsid, workspace))

		actualPartition, ok, err := checkpoints.GetPartitionCheckpoint(appID, partitionID)
		require.NoError(err)
		require.True(ok)
		require.Equal(partition, actualPartition)

		actualWorkspace, ok, err := checkpoints.GetWorkspaceCheckpoint(appID, wsid)
		require.NoError(err)
		require.True(ok)
		require.Equal(workspace, actualWorkspace)

		assertCheckpointJSON(t, sysVVMStorage, partitionCheckpointPKeyForTest(appID),
			partitionCheckpointCColsForTest(partitionID), map[string]any{"lastHandledPLogOffset": float64(42)})
		assertCheckpointJSON(t, sysVVMStorage, workspaceCheckpointPKeyForTest(appID),
			workspaceCheckpointCColsForTest(wsid), map[string]any{"lastHandledWLogOffset": float64(43)})
	})

	t.Run("keys isolate applications partitions and workspace clustering columns", func(t *testing.T) {
		partitionCases := []struct {
			appID       istructs.ClusterAppID
			partitionID istructs.PartitionID
			offset      istructs.Offset
		}{
			{appID: 201, partitionID: 1, offset: 11},
			{appID: 202, partitionID: 1, offset: 12},
			{appID: 201, partitionID: 2, offset: 13},
		}
		for _, tc := range partitionCases {
			require.NoError(checkpoints.PutPartitionCheckpoint(tc.appID, tc.partitionID,
				recoverycheckpoints.PartitionCheckpoint{LastHandledPLogOffset: tc.offset}))
		}
		for _, tc := range partitionCases {
			actual, ok, err := checkpoints.GetPartitionCheckpoint(tc.appID, tc.partitionID)
			require.NoError(err)
			require.True(ok)
			require.Equal(tc.offset, actual.LastHandledPLogOffset)
		}

		workspaceCases := []struct {
			appID  istructs.ClusterAppID
			wsid   istructs.WSID
			offset istructs.Offset
		}{
			{appID: 201, wsid: 1, offset: 21},
			{appID: 202, wsid: 1, offset: 22},
			{appID: 201, wsid: 2, offset: 23},
		}
		for _, tc := range workspaceCases {
			require.NoError(checkpoints.PutWorkspaceCheckpoint(tc.appID, tc.wsid,
				recoverycheckpoints.WorkspaceCheckpoint{LastHandledWLogOffset: tc.offset}))
		}
		for _, tc := range workspaceCases {
			actual, ok, err := checkpoints.GetWorkspaceCheckpoint(tc.appID, tc.wsid)
			require.NoError(err)
			require.True(ok)
			require.Equal(tc.offset, actual.LastHandledWLogOffset)
		}
	})

	t.Run("malformed or incomplete values return errors", func(t *testing.T) {
		malformedPartitionID := istructs.PartitionID(77)
		malformedWSID := istructs.WSID(7701)
		require.NoError(sysVVMStorage.Put(partitionCheckpointPKeyForTest(appID),
			partitionCheckpointCColsForTest(malformedPartitionID), []byte("not-json")))
		require.NoError(sysVVMStorage.Put(workspaceCheckpointPKeyForTest(appID),
			workspaceCheckpointCColsForTest(malformedWSID), []byte(`{"other":1}`)))

		_, ok, err := checkpoints.GetPartitionCheckpoint(appID, malformedPartitionID)
		require.Error(err)
		require.False(ok)

		_, ok, err = checkpoints.GetWorkspaceCheckpoint(appID, malformedWSID)
		require.Error(err)
		require.False(ok)
	})
}

func TestRecoveryCheckpointStorageUsesLastWriteWins(t *testing.T) {
	require := require.New(t)
	_, checkpoints := newRecoveryCheckpointStorageForTest(t)

	const (
		appID       = istructs.ClusterAppID(301)
		partitionID = istructs.PartitionID(3)
		wsid        = istructs.WSID(3001)
	)

	require.NoError(checkpoints.PutPartitionCheckpoint(appID, partitionID,
		recoverycheckpoints.PartitionCheckpoint{LastHandledPLogOffset: 200}))
	require.NoError(checkpoints.PutWorkspaceCheckpoint(appID, wsid,
		recoverycheckpoints.WorkspaceCheckpoint{LastHandledWLogOffset: 50}))

	partition := recoverycheckpoints.PartitionCheckpoint{LastHandledPLogOffset: 100}
	workspace := recoverycheckpoints.WorkspaceCheckpoint{LastHandledWLogOffset: 40}
	require.NoError(checkpoints.PutPartitionCheckpoint(appID, partitionID, partition))
	require.NoError(checkpoints.PutWorkspaceCheckpoint(appID, wsid, workspace))

	actualPartition, ok, err := checkpoints.GetPartitionCheckpoint(appID, partitionID)
	require.NoError(err)
	require.True(ok)
	require.Equal(partition, actualPartition)

	actualWorkspace, ok, err := checkpoints.GetWorkspaceCheckpoint(appID, wsid)
	require.NoError(err)
	require.True(ok)
	require.Equal(workspace, actualWorkspace)
}

func partitionCheckpointPKeyForTest(appID istructs.ClusterAppID) []byte {
	pKey := binary.BigEndian.AppendUint32(nil, pKeyPrefix_SeqStorage_Part)
	return binary.BigEndian.AppendUint32(pKey, appID)
}

func partitionCheckpointCColsForTest(partitionID istructs.PartitionID) []byte {
	return binary.BigEndian.AppendUint16(nil, uint16(partitionID))
}

func workspaceCheckpointPKeyForTest(appID istructs.ClusterAppID) []byte {
	pKey := binary.BigEndian.AppendUint32(nil, pKeyPrefix_SeqStorage_WS)
	return binary.BigEndian.AppendUint32(pKey, appID)
}

func workspaceCheckpointCColsForTest(wsid istructs.WSID) []byte {
	return binary.BigEndian.AppendUint64(nil, uint64(wsid))
}

func assertCheckpointJSON(t *testing.T, storage ISysVvmStorage, pKey, cCols []byte, expected map[string]any) {
	t.Helper()
	require := require.New(t)
	value := []byte{}
	ok, err := storage.Get(pKey, cCols, &value)
	require.NoError(err)
	require.True(ok)
	actual := map[string]any{}
	require.NoError(json.Unmarshal(value, &actual))
	require.Equal(expected, actual)
}

func newRecoveryCheckpointStorageForTest(t *testing.T) (ISysVvmStorage, recoverycheckpoints.IRecoveryCheckpointStorage) {
	t.Helper()
	appStorageProvider := provider.Provide(mem.Provide(testingu.MockTime))
	sysVVMStorage, err := appStorageProvider.AppStorage(istructs.AppQName_sys_vvm)
	require.NoError(t, err)
	return sysVVMStorage, NewRecoveryCheckpointStorage(sysVVMStorage)
}
