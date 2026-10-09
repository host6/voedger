/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package sys_it

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/extensionpoints"
	"github.com/voedger/voedger/pkg/goutils/logger"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/istructsmem"
	"github.com/voedger/voedger/pkg/processors/actualizers"
	"github.com/voedger/voedger/pkg/sys/checkpoints"
	it "github.com/voedger/voedger/pkg/vit"
	sys_test_template "github.com/voedger/voedger/pkg/vit/testdata"
	builtinapps "github.com/voedger/voedger/pkg/vvm/builtin"
	vvmstorage "github.com/voedger/voedger/pkg/vvm/storage"
)

const (
	docWithNested = `{"cuds": [
		{"fields":{"sys.ID": 1,"sys.QName": "app1pkg.Root", "FldRoot": 2}},
		{"fields":{"sys.ID": 2,"sys.QName": "app1pkg.Nested", "sys.ParentID":1,"sys.Container": "Nested","FldNested":3}},
		{"fields":{"sys.ID": 3,"sys.QName": "app1pkg.Third", "Fld1": 42,"sys.ParentID":2,"sys.Container": "Third"}}
	]}`
	singleDoc = `{"cuds":[
		{"fields":{"sys.ID":1,"sys.QName":"app1pkg.Root","FldRoot":3}}
	]}`
	singletonCUD = `{"cuds":[
		{"fields":{"sys.ID":1,"sys.QName":"app1pkg.Config","Fld1":"singleton tail"}}
	]}`
	appWorkspaceDoc = `{"cuds":[
		{"fields":{"sys.ID":1,"sys.QName":"app1pkg.DocBLOB"}}
	]}`
)

type expectedWorkspaceSequence struct {
	wsid                           istructs.WSID
	lastWLogOffsetWithNewRecordIDs istructs.Offset
	nextWLogOffset                 istructs.Offset
	nextRecordID                   istructs.RecordID
}

func TestWorkspaceSequencesAfterRecovery(t *testing.T) {
	appQName := istructs.AppQName_test1_app1
	cfg := it.NewOwnVITConfig(
		it.WithApp(appQName, provideRecoveryTestApp,
			it.WithWorkspaceTemplate(it.QNameApp1_TestWSKind, "test_template", sys_test_template.TestTemplateFS),
			it.WithUserLogin("login", "pwd"),
			it.WithChildWorkspace(it.QNameApp1_TestWSKind, "test_ws_1", "test_template", "", "login", map[string]interface{}{"IntFld": 42}),
			it.WithChildWorkspace(it.QNameApp1_TestWSKind, "test_ws_2", "test_template", "", "login", map[string]interface{}{"IntFld": 42}),
		),
	)

	var ws1, ws2 expectedWorkspaceSequence
	appID := istructs.ClusterApps[appQName]

	it.TestRestartPreservingStorageWithHooks(t, &cfg, it.RestartPreservingStorageHooks{
		FirstRun: func(t *testing.T, vit *it.VIT) {
			require := require.New(t)
			workspace1 := vit.WS(appQName, "test_ws_1")
			workspace2 := vit.WS(appQName, "test_ws_2")

			partition1, err := vit.IAppPartitions.AppWorkspacePartitionID(appQName, workspace1.WSID)
			require.NoError(err)
			partition2, err := vit.IAppPartitions.AppWorkspacePartitionID(appQName, workspace2.WSID)
			require.NoError(err)
			require.Equal(partition1, partition2)

			ws1Response := vit.PostWS(workspace1, "c.sys.CUD", docWithNested)
			require.Len(ws1Response.NewIDs, 3)
			ws1 = expectedWorkspaceSequence{
				wsid:                           workspace1.WSID,
				lastWLogOffsetWithNewRecordIDs: ws1Response.CurrentWLogOffset,
				nextRecordID:                   ws1Response.NewIDs["3"] + 1,
			}

			singletonResponse := vit.PostWS(workspace1, "c.sys.CUD", singletonCUD)
			require.Equal(ws1Response.CurrentWLogOffset+1, singletonResponse.CurrentWLogOffset)
			ws1.nextWLogOffset = singletonResponse.CurrentWLogOffset + 1

			// This must remain the partition's final event before restart. Recovery of
			// workspace 1 must not adopt workspace 2's independently advanced sequences.
			ws2Response := vit.PostWS(workspace2, "c.sys.CUD", docWithNested)
			for range 2 {
				ws2Response = vit.PostWS(workspace2, "c.sys.CUD", docWithNested)
			}
			require.Len(ws2Response.NewIDs, 3)
			require.Greater(ws2Response.CurrentWLogOffset+1, ws1.nextWLogOffset)
			require.Greater(ws2Response.NewIDs["3"]+1, ws1.nextRecordID)
			ws2 = expectedWorkspaceSequence{
				wsid:                           workspace2.WSID,
				lastWLogOffsetWithNewRecordIDs: ws2Response.CurrentWLogOffset,
				nextWLogOffset:                 ws2Response.CurrentWLogOffset + 1,
				nextRecordID:                   ws2Response.NewIDs["3"] + 1,
			}
		},
		AfterFirstStop: func(t *testing.T, storage it.RestartStorage) {
			requireWorkspaceCheckpoint(t, storage, appID, ws1)
			requireWorkspaceCheckpoint(t, storage, appID, ws2)
		},
		SecondRun: func(t *testing.T, vit *it.VIT) {
			workspace1 := vit.WS(appQName, "test_ws_1")
			workspace2 := vit.WS(appQName, "test_ws_2")
			require.Equal(t, ws1.wsid, workspace1.WSID)
			require.Equal(t, ws2.wsid, workspace2.WSID)

			// insert a doc and check NewID and CurrentWLogOffset from the response
			checkNextWorkspaceSequence(t, vit, workspace1, ws1, singleDoc)
			checkNextWorkspaceSequence(t, vit, workspace2, ws2, singleDoc)
		},
	})
}

func TestSequencesRecoveryOnInvalidCheckpoints(t *testing.T) {
	require := require.New(t)
	const malformedOffset istructs.Offset = 1_000_000

	tests := []struct {
		name                      string
		checkpointKind            storedCheckpointKind
		expectInvalidPartitionLog bool
		expectInvalidWorkspaceLog bool
	}{
		{
			name:                      "offsets beyond the log tails",
			checkpointKind:            storedCheckpointBeyondTail,
			expectInvalidPartitionLog: true,
			expectInvalidWorkspaceLog: true,
		},
		{
			name:           "zero offsets",
			checkpointKind: storedCheckpointZero,
		},
		{
			name:                      "WLog offset of a non-ID-bearing event",
			checkpointKind:            storedCheckpointNonIDBearingWLogEvent,
			expectInvalidWorkspaceLog: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			appQName := istructs.AppQName_test1_app1
			cfg := it.NewOwnVITConfig(
				it.WithApp(appQName, provideRecoveryTestApp),
			)
			appID := istructs.ClusterApps[appQName]
			logCap := logger.StartCapture(t, logger.LogLevelVerbose)

			var (
				partitionID                 istructs.PartitionID
				expected                    expectedWorkspaceSequence
				nonIDBearingWLogEventOffset istructs.Offset
				storedPartitionOffset       istructs.Offset
				storedWorkspaceOffset       istructs.Offset
			)
			it.TestRestartPreservingStorageWithHooks(t, &cfg, it.RestartPreservingStorageHooks{
				FirstRun: func(t *testing.T, vit *it.VIT) {
					workspace := &it.AppWorkspace{
						WSID:  istructs.NewWSID(istructs.CurrentClusterID(), istructs.FirstBaseAppWSID),
						Owner: vit.GetSystemPrincipal(appQName),
					}
					var err error
					partitionID, err = vit.IAppPartitions.AppWorkspacePartitionID(appQName, workspace.WSID)
					require.NoError(err)

					respWithNewID := vit.PostWS(workspace, "c.sys.CUD", appWorkspaceDoc)
					require.Len(respWithNewID.NewIDs, 1)
					respWithoutNewID := vit.PostWS(workspace, "c.sys.CUD", fmt.Sprintf(
						`{"cuds":[{"sys.ID":%d,"fields":{}}]}`,
						respWithNewID.NewIDs["1"]))
					require.Empty(respWithoutNewID.NewIDs)
					require.Equal(respWithNewID.CurrentWLogOffset+1, respWithoutNewID.CurrentWLogOffset)
					previousInsertResponse := vit.PostWS(workspace, "c.sys.CUD", appWorkspaceDoc)
					require.Len(previousInsertResponse.NewIDs, 1)
					waitForRecoveryCheckpointProjector(t, vit, appQName, partitionID)

					nonIDBearingWLogEventOffset = respWithoutNewID.CurrentWLogOffset
					expected = expectedWorkspaceSequence{
						wsid:           workspace.WSID,
						nextWLogOffset: previousInsertResponse.CurrentWLogOffset + 1,
						nextRecordID:   previousInsertResponse.NewIDs["1"] + 1,
					}
				},
				AfterFirstStop: func(t *testing.T, storage it.RestartStorage) {
					// malform the checkpoints storage
					checkpointStorage := vvmstorage.NewRecoveryCheckpointStorage(storage.AppStorage(t, istructs.AppQName_sys_vvm))
					storedPartitionOffset, storedWorkspaceOffset = storedCheckpointOffsets(test.checkpointKind, malformedOffset, nonIDBearingWLogEventOffset)
					require.NoError(checkpointStorage.PutPartitionCheckpoint(appID, partitionID, checkpoints.PartitionCheckpoint{LastPLogOffset: storedPartitionOffset}))
					require.NoError(checkpointStorage.PutWorkspaceCheckpoint(appID, expected.wsid, checkpoints.WorkspaceCheckpoint{LastWLogOffsetWithNewRecordIDs: storedWorkspaceOffset}))
					logCap.Reset()
				},
				SecondRun: func(t *testing.T, vit *it.VIT) {
					workspace := &it.AppWorkspace{
						WSID:  expected.wsid,
						Owner: vit.GetSystemPrincipal(appQName),
					}

					checkNextWorkspaceSequence(t, vit, workspace, expected, appWorkspaceDoc)
					checkInvalidCheckpointLogs(t, logCap, test.expectInvalidPartitionLog, test.expectInvalidWorkspaceLog, storedPartitionOffset, storedWorkspaceOffset)
				},
			})
		})
	}
}

func waitForRecoveryCheckpointProjector(t *testing.T, vit *it.VIT, appQName appdef.AppQName,
	partitionID istructs.PartitionID) {
	t.Helper()
	appStructs, err := vit.IAppStructsProvider.BuiltIn(appQName)
	require.NoError(t, err)
	lastPLogOffset := istructs.NullOffset
	err = appStructs.Events().ReadPLog(t.Context(), partitionID, istructs.FirstOffset, istructs.ReadToTheEnd,
		func(plogOffset istructs.Offset, event istructs.IPLogEvent) error {
			defer event.Release()
			lastPLogOffset = plogOffset
			return nil
		})
	require.NoError(t, err)
	require.GreaterOrEqual(t, lastPLogOffset, istructs.FirstOffset)

	var actualizerOffset istructs.Offset
	require.Eventually(t, func() bool {
		actualizerOffset, err = actualizers.ActualizerOffset(
			appStructs, partitionID, checkpoints.QNameProjectorRecoveryCheckpoint)
		return err == nil && actualizerOffset >= lastPLogOffset
	}, 5*time.Second, 10*time.Millisecond,
		"recovery checkpoint projector offset %d did not reach PLog tail %d", actualizerOffset, lastPLogOffset)
	require.NoError(t, err)
}

type storedCheckpointKind int

const (
	storedCheckpointBeyondTail storedCheckpointKind = iota
	storedCheckpointZero
	storedCheckpointNonIDBearingWLogEvent
)

func storedCheckpointOffsets(kind storedCheckpointKind, malformedOffset, nonIDBearingWLogEventOffset istructs.Offset) (partitionOffset, workspaceOffset istructs.Offset) {
	switch kind {
	case storedCheckpointBeyondTail:
		return malformedOffset, malformedOffset
	case storedCheckpointZero:
		return istructs.NullOffset, istructs.NullOffset
	case storedCheckpointNonIDBearingWLogEvent:
		return istructs.NullOffset, nonIDBearingWLogEventOffset
	default:
		panic(fmt.Sprintf("unexpected stored checkpoint kind %d", kind))
	}
}

func checkInvalidCheckpointLogs(t *testing.T, logCap logger.ILogCaptor, expectPartitionError,
	expectWorkspaceError bool, storedPartitionOffset, storedWorkspaceOffset istructs.Offset) {
	t.Helper()
	if expectPartitionError {
		logCap.HasLine("level=ERROR", "stage=cp.partition_recovery.checkpoint.invalid",
			fmt.Sprintf("stored PLog offset %d", storedPartitionOffset))
	} else {
		logCap.NotContains("cp.partition_recovery.checkpoint.invalid")
	}
	if expectWorkspaceError {
		logCap.HasLine("level=ERROR", "stage=cp.workspace_recovery.checkpoint.invalid",
			fmt.Sprintf("stored WLog offset %d", storedWorkspaceOffset))
	} else {
		logCap.NotContains("cp.workspace_recovery.checkpoint.invalid")
	}
}

func provideRecoveryTestApp(apis builtinapps.APIs, cfg *istructsmem.AppConfigType,
	ep extensionpoints.IExtensionPoint) builtinapps.Def {
	def := it.ProvideApp1(apis, cfg, ep)
	def.NumParts = 1
	return def
}

func checkNextWorkspaceSequence(t *testing.T, vit *it.VIT, workspace *it.AppWorkspace,
	expected expectedWorkspaceSequence, insertBody string) {
	t.Helper()
	require := require.New(t)

	// insert a test doc and check its conuters
	response := vit.PostWS(workspace, "c.sys.CUD", insertBody)
	require.Len(response.NewIDs, 1)
	require.Equal(expected.nextRecordID, response.NewIDs["1"], "unexpected NewID in response at WLog offset %d", response.CurrentWLogOffset)
	require.Equal(expected.nextWLogOffset, response.CurrentWLogOffset, "unexpected WLog offset for NewID %d", response.NewIDs["1"])

	// check the CurrentWLogOffset is correct
	appStructs, err := vit.IAppStructsProvider.BuiltIn(workspace.AppQName())
	require.NoError(err)
	eventFound := false
	err = appStructs.Events().ReadWLog(t.Context(), workspace.WSID, response.CurrentWLogOffset, 1,
		func(wlogOffset istructs.Offset, event istructs.IWLogEvent) error {
			defer event.Release()
			eventFound = true
			require.Equal(response.CurrentWLogOffset, wlogOffset)
			cudCount := 0
			for cud := range event.CUDs {
				cudCount++
				require.Equal(response.NewIDs["1"], cud.ID())
			}
			require.Equal(1, cudCount)
			return nil
		})
	require.NoError(err)
	require.True(eventFound)
}

func requireWorkspaceCheckpoint(t *testing.T, storage it.RestartStorage, appID istructs.ClusterAppID,
	expected expectedWorkspaceSequence) {
	t.Helper()
	checkpointStorage := vvmstorage.NewRecoveryCheckpointStorage(
		storage.AppStorage(t, istructs.AppQName_sys_vvm))
	checkpoint, ok, err := checkpointStorage.GetWorkspaceCheckpoint(appID, expected.wsid)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, expected.lastWLogOffsetWithNewRecordIDs, checkpoint.LastWLogOffsetWithNewRecordIDs)
}
