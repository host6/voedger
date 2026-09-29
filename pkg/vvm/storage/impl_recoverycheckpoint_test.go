/*
 * Copyright (c) 2026-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package storage

import (
	"encoding/binary"
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/goutils/testingu"
	"github.com/voedger/voedger/pkg/istorage/mem"
	"github.com/voedger/voedger/pkg/istorage/provider"
	"github.com/voedger/voedger/pkg/istructs"
	commandprocessor "github.com/voedger/voedger/pkg/processors/command"
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

	t.Run("valid checkpoints round trip as JSON", func(t *testing.T) {
		partition := commandprocessor.PartitionCheckpoint{NextPLogOffset: 42}
		workspace := commandprocessor.WorkspaceCheckpoint{NextWLogOffset: 43, NextRecordID: 44}

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
				commandprocessor.PartitionCheckpoint{NextPLogOffset: tc.offset}))
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
				commandprocessor.WorkspaceCheckpoint{NextWLogOffset: istructs.Offset(tc.next), NextRecordID: tc.next}))
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
		require.NoError(sysVVMStorage.Put(partitionCheckpointPKeyForTest(appID, malformedPartitionID), nil, []byte("not-json")))
		require.NoError(sysVVMStorage.Put(workspaceCheckpointPKeyForTest(appID, malformedWSID), nil, []byte(`{"nextWLogOffset":`)))

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
		require.NoError(sysVVMStorage.Put(partitionCheckpointPKeyForTest(appID, futurePartitionID), nil,
			[]byte(`{"nextPLogOffset":91,"futureField":{"version":2}}`)))
		require.NoError(sysVVMStorage.Put(workspaceCheckpointPKeyForTest(appID, futureWSID), nil,
			[]byte(`{"nextWLogOffset":92,"nextRecordID":93,"futureField":[1,2]}`)))

		partition, ok, err := checkpoints.GetPartitionCheckpoint(appID, futurePartitionID)
		require.NoError(err)
		require.True(ok)
		require.Equal(commandprocessor.PartitionCheckpoint{NextPLogOffset: 91}, partition)
		workspace, ok, err := checkpoints.GetWorkspaceCheckpoint(appID, futureWSID)
		require.NoError(err)
		require.True(ok)
		require.Equal(commandprocessor.WorkspaceCheckpoint{NextWLogOffset: 92, NextRecordID: 93}, workspace)
	})
}

func TestRecoveryCheckpointStorageMonotonicWrites(t *testing.T) {
	require := require.New(t)
	_, checkpoints := newRecoveryCheckpointStorageForTest(t)

	const (
		appID       = istructs.ClusterAppID(301)
		partitionID = istructs.PartitionID(3)
		wsid        = istructs.WSID(3001)
	)

	partitionOffsets := []istructs.Offset{150, 100, 175, 125, 200, 50}
	workspaceValues := []commandprocessor.WorkspaceCheckpoint{
		{NextWLogOffset: 40, NextRecordID: 400},
		{NextWLogOffset: 50, NextRecordID: 300},
		{NextWLogOffset: 30, NextRecordID: 500},
		{NextWLogOffset: 45, NextRecordID: 450},
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(partitionOffsets)+len(workspaceValues))
	for _, offset := range partitionOffsets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- checkpoints.PutPartitionCheckpoint(appID, partitionID,
				commandprocessor.PartitionCheckpoint{NextPLogOffset: offset})
		}()
	}
	for _, checkpoint := range workspaceValues {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- checkpoints.PutWorkspaceCheckpoint(appID, wsid, checkpoint)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(err)
	}

	partition, ok, err := checkpoints.GetPartitionCheckpoint(appID, partitionID)
	require.NoError(err)
	require.True(ok)
	require.Equal(istructs.Offset(200), partition.NextPLogOffset)

	workspace, ok, err := checkpoints.GetWorkspaceCheckpoint(appID, wsid)
	require.NoError(err)
	require.True(ok)
	require.Equal(istructs.Offset(50), workspace.NextWLogOffset)
	require.Equal(istructs.RecordID(500), workspace.NextRecordID)

	// Simulate an overlapping VVM finishing after a newer writer. Each field is
	// merged by maximum, so stale snapshots cannot regress shared progress.
	require.NoError(checkpoints.PutPartitionCheckpoint(appID, partitionID,
		commandprocessor.PartitionCheckpoint{NextPLogOffset: 199}))
	require.NoError(checkpoints.PutWorkspaceCheckpoint(appID, wsid,
		commandprocessor.WorkspaceCheckpoint{NextWLogOffset: 49, NextRecordID: 499}))

	partition, ok, err = checkpoints.GetPartitionCheckpoint(appID, partitionID)
	require.NoError(err)
	require.True(ok)
	require.Equal(istructs.Offset(200), partition.NextPLogOffset)
	workspace, ok, err = checkpoints.GetWorkspaceCheckpoint(appID, wsid)
	require.NoError(err)
	require.True(ok)
	require.Equal(commandprocessor.WorkspaceCheckpoint{NextWLogOffset: 50, NextRecordID: 500}, workspace)
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
		commandprocessor.PartitionCheckpoint{NextPLogOffset: 81}))
	require.NoError(checkpoints.PutWorkspaceCheckpoint(appID, wsid,
		commandprocessor.WorkspaceCheckpoint{NextWLogOffset: 82, NextRecordID: 83}))

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
	ok, err := storage.Get(pKey, nil, &value)
	require.NoError(err)
	require.True(ok)
	actual := map[string]any{}
	require.NoError(json.Unmarshal(value, &actual))
	require.Equal(expected, actual)
}

func newRecoveryCheckpointStorageForTest(t *testing.T) (ISysVvmStorage, commandprocessor.IRecoveryCheckpointStorage) {
	t.Helper()
	appStorageProvider := provider.Provide(mem.Provide(testingu.MockTime))
	sysVVMStorage, err := appStorageProvider.AppStorage(istructs.AppQName_sys_vvm)
	require.NoError(t, err)
	return sysVVMStorage, NewRecoveryCheckpointStorage(sysVVMStorage)
}
