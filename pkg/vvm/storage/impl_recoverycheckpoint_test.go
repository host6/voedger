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
		require.Equal([]byte{0, 0, 0, 1}, recoveryCheckpointCCols)

		partition, ok, err := checkpoints.GetPartitionCheckpoint(appID, partitionID)
		require.NoError(err)
		require.False(ok)
		require.Zero(partition)

		workspace, ok, err := checkpoints.GetWorkspaceCheckpoint(appID, wsid)
		require.NoError(err)
		require.False(ok)
		require.Zero(workspace)
	})

	t.Run("valid checkpoints round trip as JSON", func(t *testing.T) {
		partition := recoverycheckpoints.PartitionCheckpoint{NextPLogOffset: 42}
		workspace := recoverycheckpoints.WorkspaceCheckpoint{NextWLogOffset: 43, NextRecordID: 44}

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

		assertCheckpointJSON(t, sysVVMStorage, partitionCheckpointPKeyForTest(appID, partitionID), map[string]any{
			"nextPLogOffset": float64(42),
		})
		assertCheckpointJSON(t, sysVVMStorage, workspaceCheckpointPKeyForTest(appID, wsid), map[string]any{
			"nextWLogOffset": float64(43),
			"nextRecordID":   float64(44),
		})
	})

	t.Run("keys isolate applications partitions and workspaces", func(t *testing.T) {
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
				recoverycheckpoints.PartitionCheckpoint{NextPLogOffset: tc.offset}))
		}
		for _, tc := range partitionCases {
			actual, ok, err := checkpoints.GetPartitionCheckpoint(tc.appID, tc.partitionID)
			require.NoError(err)
			require.True(ok)
			require.Equal(tc.offset, actual.NextPLogOffset)
		}

		workspaceCases := []struct {
			appID istructs.ClusterAppID
			wsid  istructs.WSID
			next  istructs.RecordID
		}{
			{appID: 201, wsid: 1, next: 21},
			{appID: 202, wsid: 1, next: 22},
			{appID: 201, wsid: 2, next: 23},
		}
		for _, tc := range workspaceCases {
			require.NoError(checkpoints.PutWorkspaceCheckpoint(tc.appID, tc.wsid,
				recoverycheckpoints.WorkspaceCheckpoint{NextWLogOffset: istructs.Offset(tc.next), NextRecordID: tc.next}))
		}
		for _, tc := range workspaceCases {
			actual, ok, err := checkpoints.GetWorkspaceCheckpoint(tc.appID, tc.wsid)
			require.NoError(err)
			require.True(ok)
			require.Equal(tc.next, actual.NextRecordID)
		}
	})

	t.Run("malformed checkpoints return errors", func(t *testing.T) {
		malformedPartitionID := istructs.PartitionID(77)
		malformedWSID := istructs.WSID(7701)
		require.NoError(sysVVMStorage.Put(partitionCheckpointPKeyForTest(appID, malformedPartitionID),
			recoveryCheckpointCCols, []byte("not-json")))
		require.NoError(sysVVMStorage.Put(workspaceCheckpointPKeyForTest(appID, malformedWSID),
			recoveryCheckpointCCols, []byte(`{"nextWLogOffset":`)))

		_, ok, err := checkpoints.GetPartitionCheckpoint(appID, malformedPartitionID)
		require.Error(err)
		require.False(ok)

		_, ok, err = checkpoints.GetWorkspaceCheckpoint(appID, malformedWSID)
		require.Error(err)
		require.False(ok)
	})

	t.Run("unknown JSON fields are ignored", func(t *testing.T) {
		futurePartitionID := istructs.PartitionID(78)
		futureWSID := istructs.WSID(7801)
		require.NoError(sysVVMStorage.Put(partitionCheckpointPKeyForTest(appID, futurePartitionID), recoveryCheckpointCCols,
			[]byte(`{"nextPLogOffset":91,"futureField":{"version":2}}`)))
		require.NoError(sysVVMStorage.Put(workspaceCheckpointPKeyForTest(appID, futureWSID), recoveryCheckpointCCols,
			[]byte(`{"nextWLogOffset":92,"nextRecordID":93,"futureField":[1,2]}`)))

		partition, ok, err := checkpoints.GetPartitionCheckpoint(appID, futurePartitionID)
		require.NoError(err)
		require.True(ok)
		require.Equal(recoverycheckpoints.PartitionCheckpoint{NextPLogOffset: 91}, partition)
		workspace, ok, err := checkpoints.GetWorkspaceCheckpoint(appID, futureWSID)
		require.NoError(err)
		require.True(ok)
		require.Equal(recoverycheckpoints.WorkspaceCheckpoint{NextWLogOffset: 92, NextRecordID: 93}, workspace)
	})
}

func TestRecoveryCheckpointStorageOverwritesWithProvidedValues(t *testing.T) {
	require := require.New(t)
	_, checkpoints := newRecoveryCheckpointStorageForTest(t)

	const (
		appID       = istructs.ClusterAppID(301)
		partitionID = istructs.PartitionID(3)
		wsid        = istructs.WSID(3001)
	)

	require.NoError(checkpoints.PutPartitionCheckpoint(appID, partitionID,
		recoverycheckpoints.PartitionCheckpoint{NextPLogOffset: 200}))
	require.NoError(checkpoints.PutWorkspaceCheckpoint(appID, wsid,
		recoverycheckpoints.WorkspaceCheckpoint{NextWLogOffset: 50, NextRecordID: 500}))

	partition := recoverycheckpoints.PartitionCheckpoint{NextPLogOffset: 100}
	workspace := recoverycheckpoints.WorkspaceCheckpoint{NextWLogOffset: 40, NextRecordID: 400}
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

func TestRecoveryCheckpointStorageCoexistsWithLegacyCells(t *testing.T) {
	require := require.New(t)
	sysVVMStorage, checkpoints := newRecoveryCheckpointStorageForTest(t)

	const (
		appID       = istructs.ClusterAppID(401)
		partitionID = istructs.PartitionID(4)
		wsid        = istructs.WSID(4001)
	)
	partitionPKey := partitionCheckpointPKeyForTest(appID, partitionID)
	workspacePKey := workspaceCheckpointPKeyForTest(appID, wsid)
	legacyPartitionCCols := binary.BigEndian.AppendUint32(nil, PLogOffsetCC)
	legacyWorkspaceCCols := binary.BigEndian.AppendUint16(nil, 1)
	legacyPartitionValue := binary.BigEndian.AppendUint64(nil, 71)
	legacyWorkspaceValue := binary.BigEndian.AppendUint64(nil, 72)
	require.NoError(sysVVMStorage.Put(partitionPKey, legacyPartitionCCols, legacyPartitionValue))
	require.NoError(sysVVMStorage.Put(workspacePKey, legacyWorkspaceCCols, legacyWorkspaceValue))

	require.NoError(checkpoints.PutPartitionCheckpoint(appID, partitionID,
		recoverycheckpoints.PartitionCheckpoint{NextPLogOffset: 81}))
	require.NoError(checkpoints.PutWorkspaceCheckpoint(appID, wsid,
		recoverycheckpoints.WorkspaceCheckpoint{NextWLogOffset: 82, NextRecordID: 83}))

	actualLegacyPartition := []byte{}
	ok, err := sysVVMStorage.Get(partitionPKey, legacyPartitionCCols, &actualLegacyPartition)
	require.NoError(err)
	require.True(ok)
	require.Equal(legacyPartitionValue, actualLegacyPartition)

	actualLegacyWorkspace := []byte{}
	ok, err = sysVVMStorage.Get(workspacePKey, legacyWorkspaceCCols, &actualLegacyWorkspace)
	require.NoError(err)
	require.True(ok)
	require.Equal(legacyWorkspaceValue, actualLegacyWorkspace)
}

func partitionCheckpointPKeyForTest(appID istructs.ClusterAppID, partitionID istructs.PartitionID) []byte {
	pKey := binary.BigEndian.AppendUint32(nil, pKeyPrefix_SeqStorage_Part)
	pKey = binary.BigEndian.AppendUint32(pKey, appID)
	return binary.BigEndian.AppendUint16(pKey, uint16(partitionID))
}

func workspaceCheckpointPKeyForTest(appID istructs.ClusterAppID, wsid istructs.WSID) []byte {
	pKey := binary.BigEndian.AppendUint32(nil, pKeyPrefix_SeqStorage_WS)
	pKey = binary.BigEndian.AppendUint32(pKey, appID)
	return binary.BigEndian.AppendUint64(pKey, uint64(wsid))
}

func assertCheckpointJSON(t *testing.T, storage ISysVvmStorage, pKey []byte, expected map[string]any) {
	t.Helper()
	require := require.New(t)
	value := []byte{}
	require.Equal([]byte{0, 0, 0, 1}, recoveryCheckpointCCols)
	ok, err := storage.Get(pKey, recoveryCheckpointCCols, &value)
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
