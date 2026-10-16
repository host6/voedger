/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package sys_it

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/appdef"
	appdefsys "github.com/voedger/voedger/pkg/appdef/sys"
	"github.com/voedger/voedger/pkg/appparts"
	"github.com/voedger/voedger/pkg/checkpoints"
	"github.com/voedger/voedger/pkg/coreutils"
	"github.com/voedger/voedger/pkg/extensionpoints"
	"github.com/voedger/voedger/pkg/goutils/httpu"
	"github.com/voedger/voedger/pkg/goutils/logger"
	"github.com/voedger/voedger/pkg/goutils/timeu"
	"github.com/voedger/voedger/pkg/in10n"
	"github.com/voedger/voedger/pkg/istorage"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/istructsmem"
	"github.com/voedger/voedger/pkg/processors/actualizers"
	"github.com/voedger/voedger/pkg/sys/collection"
	it "github.com/voedger/voedger/pkg/vit"
	sys_test_template "github.com/voedger/voedger/pkg/vit/testdata"
	"github.com/voedger/voedger/pkg/vvm"
	builtinapps "github.com/voedger/voedger/pkg/vvm/builtin"
	vvmstorage "github.com/voedger/voedger/pkg/vvm/storage"
)

const (
	recoveryTestLoginName     = "recovery-test-login"
	recoveryTestLoginPassword = "recovery-test-password"
	recoveryTestWorkspaceName = "recovery-test-workspace"
	docWithNested             = `{"cuds": [
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
	nonIDBearingCommand = "c.sys.RegisterTempBLOB1d"
	cudBody1Doc         = `{"cuds":[
		{"fields":{"sys.ID":1,"sys.QName":"app1pkg.options"}}
	]}`
	fourAppWorkspaceDocs = `{"cuds":[
		{"fields":{"sys.ID":1,"sys.QName":"app1pkg.options"}},
		{"fields":{"sys.ID":2,"sys.QName":"app1pkg.options"}},
		{"fields":{"sys.ID":3,"sys.QName":"app1pkg.options"}},
		{"fields":{"sys.ID":4,"sys.QName":"app1pkg.options"}}
	]}`
)

type expectedWorkspaceSequence struct {
	wsid                istructs.WSID
	wlogIDBearingOffset istructs.Offset
	nextWLogOffset      istructs.Offset
	nextRecordID        istructs.RecordID
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

	var expectedWS1Numbers, expectedWS2Numbers expectedWorkspaceSequence
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
			expectedWS1Numbers = expectedWorkspaceSequence{
				wsid:                workspace1.WSID,
				wlogIDBearingOffset: ws1Response.CurrentWLogOffset,
				nextRecordID:        ws1Response.NewIDs["3"] + 1,
			}

			singletonResponse := vit.PostWS(workspace1, "c.sys.CUD", singletonCUD)
			require.Equal(ws1Response.CurrentWLogOffset+1, singletonResponse.CurrentWLogOffset)
			expectedWS1Numbers.nextWLogOffset = singletonResponse.CurrentWLogOffset + 1

			// This must remain the partition's final event before restart. Recovery of
			// workspace 1 must not adopt workspace 2's independently advanced sequences.
			ws2Response := vit.PostWS(workspace2, "c.sys.CUD", docWithNested)
			for range 2 {
				ws2Response = vit.PostWS(workspace2, "c.sys.CUD", docWithNested)
			}
			require.Len(ws2Response.NewIDs, 3)
			require.Greater(ws2Response.CurrentWLogOffset+1, expectedWS1Numbers.nextWLogOffset)
			require.Greater(ws2Response.NewIDs["3"]+1, expectedWS1Numbers.nextRecordID)
			expectedWS2Numbers = expectedWorkspaceSequence{
				wsid:                workspace2.WSID,
				wlogIDBearingOffset: ws2Response.CurrentWLogOffset,
				nextWLogOffset:      ws2Response.CurrentWLogOffset + 1,
				nextRecordID:        ws2Response.NewIDs["3"] + 1,
			}
		},
		AfterFirstStop: func(t *testing.T, storage it.RestartStorage) {
			checkpointStorage := vvmstorage.NewRecoveryCheckpointStorage(
				storage.AppStorage(t, istructs.AppQName_sys_vvm))
			requireStoredWorkspaceCheckpoint(t, checkpointStorage, appID, expectedWS1Numbers)
			requireStoredWorkspaceCheckpoint(t, checkpointStorage, appID, expectedWS2Numbers)
		},
		SecondRun: func(t *testing.T, vit *it.VIT) {
			workspace1 := vit.WS(appQName, "test_ws_1")
			workspace2 := vit.WS(appQName, "test_ws_2")
			require.Equal(t, expectedWS1Numbers.wsid, workspace1.WSID)
			require.Equal(t, expectedWS2Numbers.wsid, workspace2.WSID)

			// insert a doc and check NewID and CurrentWLogOffset from the response
			checkNextWorkspaceSequence(vit, workspace1, expectedWS1Numbers)
			checkNextWorkspaceSequence(vit, workspace2, expectedWS2Numbers)
		},
	})
}

func TestRecoveryFromCheckpoints(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	t.Run("recovery: scn: Initial recovery initializes checkpoints but not runtime sequence state", func(t *testing.T) {
		cfg := initialRecoveryVITConfig()
		appQName := istructs.AppQName_test1_app1
		appID := istructs.ClusterApps[appQName]
		logCap := logger.StartCapture(t, logger.LogLevelVerbose)
		var workspace *it.AppWorkspace
		var partitionID istructs.PartitionID

		it.TestRestartPreservingStorageWithHooks(t, &cfg, it.RestartPreservingStorageHooks{
			FirstRun: func(t *testing.T, vit *it.VIT) {
				owner := vit.GetPrincipal(appQName, recoveryTestLoginName)
				workspace = createWorkspacesInPartition(t, vit, owner, 1, 1)[0]
				var err error
				partitionID, err = vit.IAppPartitions.AppWorkspacePartitionID(appQName, workspace.WSID)
				require.NoError(t, err)
				vit.PostWS(workspace, "c.sys.CUD", cudBody1Doc)
			},
			AfterFirstStop: func(t *testing.T, storage it.RestartStorage) {
				checkpointAppStorage := storage.AppStorage(t, istructs.AppQName_sys_vvm)
				checkpointStorage := vvmstorage.NewRecoveryCheckpointStorage(checkpointAppStorage)
				removePartitionCheckpoint(t, checkpointStorage, checkpointAppStorage, appID, partitionID)
				deleteWorkspaceCheckpoint(t, checkpointStorage, checkpointAppStorage, appID, workspace.WSID)
				_, ok, err := checkpointStorage.GetPartitionCheckpoint(appID, partitionID)
				require.NoError(t, err)
				require.False(t, ok)
				logCap.Reset()
			},
			SecondRun: func(t *testing.T, vit *it.VIT) {
				// Given initial recovery has created checkpoints for partition 1 and workspace 10
				checkpointAppStorage, err := vit.IAppStorageProvider.AppStorage(istructs.AppQName_sys_vvm)
				require.NoError(t, err)
				checkpointStorage := vvmstorage.NewRecoveryCheckpointStorage(checkpointAppStorage)
				_, partitionCheckpointExists, err := checkpointStorage.GetPartitionCheckpoint(appID, partitionID)
				require.NoError(t, err)
				require.True(t, partitionCheckpointExists)
				_, workspaceCheckpointExists, err := checkpointStorage.GetWorkspaceCheckpoint(appID, workspace.WSID)
				require.NoError(t, err)
				require.True(t, workspaceCheckpointExists)
				// And the server has become ready
				require.NotNil(t, vit.VoedgerVM)

				// When VAOperator submits a command to workspace 10
				response := vit.PostWS(workspace, "c.sys.CUD", cudBody1Doc,
					httpu.WithNoRetryPolicy(), httpu.Expect503())
				// Then response status is "503 Service Unavailable" while partition recovery runs
				require.Equal(t, http.StatusServiceUnavailable, response.HTTPResp.StatusCode)
				logCap.EventuallyHasLine("stage=cp.partition_recovery.complete", fmt.Sprintf("partid=%d", partitionID))

				// When VAOperator retries the command after partition recovery completes
				response = vit.PostWS(workspace, "c.sys.CUD", cudBody1Doc,
					httpu.WithNoRetryPolicy(), httpu.Expect503())
				// Then response status is "503 Service Unavailable" while workspace recovery runs
				require.Equal(t, http.StatusServiceUnavailable, response.HTTPResp.StatusCode)
				logCap.EventuallyHasLine("stage=cp.workspace_recovery.complete", fmt.Sprintf("wsid=%d", workspace.WSID))

				// When VAOperator retries the command after workspace recovery completes
				// Then the command succeeds
				vit.PostWS(workspace, "c.sys.CUD", cudBody1Doc)
			},
		})
	})

	t.Run("recovery: scn: Initial recovery failures do not prevent server readiness: partition 1", func(t *testing.T) {
		// | failed_partitions      | checkpoint_state                    | deployed_partitions | target_partition |
		// | partition 1            | present for partitions 0 and 2 only | 0 and 2             | 1                |
		control := newInitialRecoveryReadFailure()
		cfg := initialRecoveryFailureVITConfig(control, true)
		appQName := istructs.AppQName_test1_app1
		failed := []istructs.PartitionID{1}
		logCap := logger.StartCapture(t, logger.LogLevelVerbose)
		var target *it.AppWorkspace
		var lastPLogOffset istructs.Offset

		// When VAOperator starts the VVM
		it.TestRestartPreservingStorageWithHooks(t, &cfg, it.RestartPreservingStorageHooks{
			FirstRun: func(t *testing.T, vit *it.VIT) {
				target, lastPLogOffset = prepareInitialRecoveryFailureHistory(t, vit, 1)
				// And asynchronous projectors and scheduled jobs are configured for every partition
				requireConfiguredInitialRecoveryWorkers(t, vit)
			},
			AfterFirstStop: func(t *testing.T, storage it.RestartStorage) {
				// Given application "test1/app1" declares partition IDs 0 through 2 with no stored partition checkpoints
				removeInitialRecoveryMarkers(t, storage)
				// And initial recovery fails for <failed_partitions> with "PLog read failed"
				// failed_partitions = partition 1
				control.planInitialRecoveryFailure(failed)
				logCap.Reset()
			},
			SecondRun: func(t *testing.T, vit *it.VIT) {
				// Then partition checkpoints are <checkpoint_state>
				// checkpoint_state = present for partitions 0 and 2 only
				requireInitialRecoveryCheckpointPresence(t, vit, failed)
				// And initial-recovery error "PLog read failed" is logged for <failed_partitions>
				// failed_partitions = partition 1
				requireInitialRecoveryFailureLogs(t, logCap, failed)
				// And deployed partitions are <deployed_partitions>
				// deployed_partitions = 0 and 2
				require.ElementsMatch(t, []istructs.PartitionID{0, 2}, deployedInitialRecoveryPartitions(t, vit))
				// And deployment is skipped for <failed_partitions>
				// failed_partitions = partition 1
				requireInitialRecoveryPartitionsExcluded(t, vit, failed)
				// And no asynchronous projector or scheduled job runs for <failed_partitions>
				// failed_partitions = partition 1
				requireNoInitialRecoveryWorkers(t, vit, failed)
				// And the server becomes ready
				require.NotNil(t, vit.VoedgerVM)

				// When VAOperator submits a command to partition <target_partition>
				// target_partition = 1
				response := vit.PostWS(target, "c.sys.CUD", cudBody1Doc, httpu.WithNoRetryPolicy(), httpu.Expect500())
				// Then response status is "500 Internal Server Error"
				require.Equal(t, http.StatusInternalServerError, response.HTTPResp.StatusCode)
				// And response error is "partition not found"
				requireInitialRecoveryPartitionNotFound(t, response.SysError)

				// When the PLog read failure is resolved without restarting the VVM
				control.resolve()
				// And VAOperator submits another command to partition <target_partition>
				// target_partition = 1
				response = vit.PostWS(target, "c.sys.CUD", cudBody1Doc, httpu.WithNoRetryPolicy(), httpu.Expect500())
				// Then response status is "500 Internal Server Error"
				require.Equal(t, http.StatusInternalServerError, response.HTTPResp.StatusCode)
				// And response error is "partition not found"
				requireInitialRecoveryPartitionNotFound(t, response.SysError)
				// And no partition or workspace recovery has started for partition <target_partition>
				// target_partition = 1
				requireNoInitialRecoveryRuntimeStarts(t, logCap, 1)
				// And no command event has been appended for partition <target_partition>
				// target_partition = 1
				require.Equal(t, lastPLogOffset, getLastPLogOffset(t, vit, appQName, 1))
				// And deployment is still skipped for <failed_partitions>
				// failed_partitions = partition 1
				requireInitialRecoveryPartitionsExcluded(t, vit, failed)
				// And no asynchronous projector or scheduled job runs for <failed_partitions>
				// failed_partitions = partition 1
				requireNoInitialRecoveryWorkers(t, vit, failed)
			},
		})
	})

	t.Run("recovery: scn: Initial recovery failures do not prevent server readiness: partitions 0 through 2: target 0", func(t *testing.T) {
		// | failed_partitions      | checkpoint_state                    | deployed_partitions | target_partition |
		// | partitions 0 through 2 | absent for every partition          | none                | 0                |
		control := newInitialRecoveryReadFailure()
		cfg := initialRecoveryFailureVITConfig(control, true)
		appQName := istructs.AppQName_test1_app1
		failed := []istructs.PartitionID{0, 1, 2}
		logCap := logger.StartCapture(t, logger.LogLevelVerbose)
		var target *it.AppWorkspace
		var lastPLogOffset istructs.Offset

		// When VAOperator starts the VVM
		it.TestRestartPreservingStorageWithHooks(t, &cfg, it.RestartPreservingStorageHooks{
			FirstRun: func(t *testing.T, vit *it.VIT) {
				target, lastPLogOffset = prepareInitialRecoveryFailureHistory(t, vit, 0)
				// And asynchronous projectors and scheduled jobs are configured for every partition
				requireConfiguredInitialRecoveryWorkers(t, vit)
			},
			AfterFirstStop: func(t *testing.T, storage it.RestartStorage) {
				// Given application "test1/app1" declares partition IDs 0 through 2 with no stored partition checkpoints
				removeInitialRecoveryMarkers(t, storage)
				// And initial recovery fails for <failed_partitions> with "PLog read failed"
				// failed_partitions = partitions 0 through 2
				control.planInitialRecoveryFailure(failed)
				logCap.Reset()
			},
			SecondRun: func(t *testing.T, vit *it.VIT) {
				// Then partition checkpoints are <checkpoint_state>
				// checkpoint_state = absent for every partition
				requireInitialRecoveryCheckpointPresence(t, vit, failed)
				// And initial-recovery error "PLog read failed" is logged for <failed_partitions>
				// failed_partitions = partitions 0 through 2
				requireInitialRecoveryFailureLogs(t, logCap, failed)
				// And deployed partitions are <deployed_partitions>
				// deployed_partitions = none
				require.ElementsMatch(t, []istructs.PartitionID{}, deployedInitialRecoveryPartitions(t, vit))
				// And deployment is skipped for <failed_partitions>
				// failed_partitions = partitions 0 through 2
				requireInitialRecoveryPartitionsExcluded(t, vit, failed)
				// And no asynchronous projector or scheduled job runs for <failed_partitions>
				// failed_partitions = partitions 0 through 2
				requireNoInitialRecoveryWorkers(t, vit, failed)
				// And the server becomes ready
				require.NotNil(t, vit.VoedgerVM)

				// When VAOperator submits a command to partition <target_partition>
				// target_partition = 0
				response := vit.PostWS(target, "c.sys.CUD", cudBody1Doc, httpu.WithNoRetryPolicy(), httpu.Expect500())
				// Then response status is "500 Internal Server Error"
				require.Equal(t, http.StatusInternalServerError, response.HTTPResp.StatusCode)
				// And response error is "partition not found"
				requireInitialRecoveryPartitionNotFound(t, response.SysError)

				// When the PLog read failure is resolved without restarting the VVM
				control.resolve()
				// And VAOperator submits another command to partition <target_partition>
				// target_partition = 0
				response = vit.PostWS(target, "c.sys.CUD", cudBody1Doc, httpu.WithNoRetryPolicy(), httpu.Expect500())
				// Then response status is "500 Internal Server Error"
				require.Equal(t, http.StatusInternalServerError, response.HTTPResp.StatusCode)
				// And response error is "partition not found"
				requireInitialRecoveryPartitionNotFound(t, response.SysError)
				// And no partition or workspace recovery has started for partition <target_partition>
				// target_partition = 0
				requireNoInitialRecoveryRuntimeStarts(t, logCap, 0)
				// And no command event has been appended for partition <target_partition>
				// target_partition = 0
				require.Equal(t, lastPLogOffset, getLastPLogOffset(t, vit, appQName, 0))
				// And deployment is still skipped for <failed_partitions>
				// failed_partitions = partitions 0 through 2
				requireInitialRecoveryPartitionsExcluded(t, vit, failed)
				// And no asynchronous projector or scheduled job runs for <failed_partitions>
				// failed_partitions = partitions 0 through 2
				requireNoInitialRecoveryWorkers(t, vit, failed)
			},
		})
	})

	t.Run("recovery: scn: Initial recovery failures do not prevent server readiness: partitions 0 through 2: target 1", func(t *testing.T) {
		// | failed_partitions      | checkpoint_state                    | deployed_partitions | target_partition |
		// | partitions 0 through 2 | absent for every partition          | none                | 1                |
		control := newInitialRecoveryReadFailure()
		cfg := initialRecoveryFailureVITConfig(control, true)
		appQName := istructs.AppQName_test1_app1
		failed := []istructs.PartitionID{0, 1, 2}
		logCap := logger.StartCapture(t, logger.LogLevelVerbose)
		var target *it.AppWorkspace
		var lastPLogOffset istructs.Offset

		// When VAOperator starts the VVM
		it.TestRestartPreservingStorageWithHooks(t, &cfg, it.RestartPreservingStorageHooks{
			FirstRun: func(t *testing.T, vit *it.VIT) {
				target, lastPLogOffset = prepareInitialRecoveryFailureHistory(t, vit, 1)
				// And asynchronous projectors and scheduled jobs are configured for every partition
				requireConfiguredInitialRecoveryWorkers(t, vit)
			},
			AfterFirstStop: func(t *testing.T, storage it.RestartStorage) {
				// Given application "test1/app1" declares partition IDs 0 through 2 with no stored partition checkpoints
				removeInitialRecoveryMarkers(t, storage)
				// And initial recovery fails for <failed_partitions> with "PLog read failed"
				// failed_partitions = partitions 0 through 2
				control.planInitialRecoveryFailure(failed)
				logCap.Reset()
			},
			SecondRun: func(t *testing.T, vit *it.VIT) {
				// Then partition checkpoints are <checkpoint_state>
				// checkpoint_state = absent for every partition
				requireInitialRecoveryCheckpointPresence(t, vit, failed)
				// And initial-recovery error "PLog read failed" is logged for <failed_partitions>
				// failed_partitions = partitions 0 through 2
				requireInitialRecoveryFailureLogs(t, logCap, failed)
				// And deployed partitions are <deployed_partitions>
				// deployed_partitions = none
				require.ElementsMatch(t, []istructs.PartitionID{}, deployedInitialRecoveryPartitions(t, vit))
				// And deployment is skipped for <failed_partitions>
				// failed_partitions = partitions 0 through 2
				requireInitialRecoveryPartitionsExcluded(t, vit, failed)
				// And no asynchronous projector or scheduled job runs for <failed_partitions>
				// failed_partitions = partitions 0 through 2
				requireNoInitialRecoveryWorkers(t, vit, failed)
				// And the server becomes ready
				require.NotNil(t, vit.VoedgerVM)

				// When VAOperator submits a command to partition <target_partition>
				// target_partition = 1
				response := vit.PostWS(target, "c.sys.CUD", cudBody1Doc, httpu.WithNoRetryPolicy(), httpu.Expect500())
				// Then response status is "500 Internal Server Error"
				require.Equal(t, http.StatusInternalServerError, response.HTTPResp.StatusCode)
				// And response error is "partition not found"
				requireInitialRecoveryPartitionNotFound(t, response.SysError)

				// When the PLog read failure is resolved without restarting the VVM
				control.resolve()
				// And VAOperator submits another command to partition <target_partition>
				// target_partition = 1
				response = vit.PostWS(target, "c.sys.CUD", cudBody1Doc, httpu.WithNoRetryPolicy(), httpu.Expect500())
				// Then response status is "500 Internal Server Error"
				require.Equal(t, http.StatusInternalServerError, response.HTTPResp.StatusCode)
				// And response error is "partition not found"
				requireInitialRecoveryPartitionNotFound(t, response.SysError)
				// And no partition or workspace recovery has started for partition <target_partition>
				// target_partition = 1
				requireNoInitialRecoveryRuntimeStarts(t, logCap, 1)
				// And no command event has been appended for partition <target_partition>
				// target_partition = 1
				require.Equal(t, lastPLogOffset, getLastPLogOffset(t, vit, appQName, 1))
				// And deployment is still skipped for <failed_partitions>
				// failed_partitions = partitions 0 through 2
				requireInitialRecoveryPartitionsExcluded(t, vit, failed)
				// And no asynchronous projector or scheduled job runs for <failed_partitions>
				// failed_partitions = partitions 0 through 2
				requireNoInitialRecoveryWorkers(t, vit, failed)
			},
		})
	})

	t.Run("recovery: scn: Initial recovery failures do not prevent server readiness: partitions 0 through 2: target 2", func(t *testing.T) {
		// | failed_partitions      | checkpoint_state                    | deployed_partitions | target_partition |
		// | partitions 0 through 2 | absent for every partition          | none                | 2                |
		control := newInitialRecoveryReadFailure()
		cfg := initialRecoveryFailureVITConfig(control, true)
		appQName := istructs.AppQName_test1_app1
		failed := []istructs.PartitionID{0, 1, 2}
		logCap := logger.StartCapture(t, logger.LogLevelVerbose)
		var target *it.AppWorkspace
		var lastPLogOffset istructs.Offset

		// When VAOperator starts the VVM
		it.TestRestartPreservingStorageWithHooks(t, &cfg, it.RestartPreservingStorageHooks{
			FirstRun: func(t *testing.T, vit *it.VIT) {
				target, lastPLogOffset = prepareInitialRecoveryFailureHistory(t, vit, 2)
				// And asynchronous projectors and scheduled jobs are configured for every partition
				requireConfiguredInitialRecoveryWorkers(t, vit)
			},
			AfterFirstStop: func(t *testing.T, storage it.RestartStorage) {
				// Given application "test1/app1" declares partition IDs 0 through 2 with no stored partition checkpoints
				removeInitialRecoveryMarkers(t, storage)
				// And initial recovery fails for <failed_partitions> with "PLog read failed"
				// failed_partitions = partitions 0 through 2
				control.planInitialRecoveryFailure(failed)
				logCap.Reset()
			},
			SecondRun: func(t *testing.T, vit *it.VIT) {
				// Then partition checkpoints are <checkpoint_state>
				// checkpoint_state = absent for every partition
				requireInitialRecoveryCheckpointPresence(t, vit, failed)
				// And initial-recovery error "PLog read failed" is logged for <failed_partitions>
				// failed_partitions = partitions 0 through 2
				requireInitialRecoveryFailureLogs(t, logCap, failed)
				// And deployed partitions are <deployed_partitions>
				// deployed_partitions = none
				require.ElementsMatch(t, []istructs.PartitionID{}, deployedInitialRecoveryPartitions(t, vit))
				// And deployment is skipped for <failed_partitions>
				// failed_partitions = partitions 0 through 2
				requireInitialRecoveryPartitionsExcluded(t, vit, failed)
				// And no asynchronous projector or scheduled job runs for <failed_partitions>
				// failed_partitions = partitions 0 through 2
				requireNoInitialRecoveryWorkers(t, vit, failed)
				// And the server becomes ready
				require.NotNil(t, vit.VoedgerVM)

				// When VAOperator submits a command to partition <target_partition>
				// target_partition = 2
				response := vit.PostWS(target, "c.sys.CUD", cudBody1Doc, httpu.WithNoRetryPolicy(), httpu.Expect500())
				// Then response status is "500 Internal Server Error"
				require.Equal(t, http.StatusInternalServerError, response.HTTPResp.StatusCode)
				// And response error is "partition not found"
				requireInitialRecoveryPartitionNotFound(t, response.SysError)

				// When the PLog read failure is resolved without restarting the VVM
				control.resolve()
				// And VAOperator submits another command to partition <target_partition>
				// target_partition = 2
				response = vit.PostWS(target, "c.sys.CUD", cudBody1Doc, httpu.WithNoRetryPolicy(), httpu.Expect500())
				// Then response status is "500 Internal Server Error"
				require.Equal(t, http.StatusInternalServerError, response.HTTPResp.StatusCode)
				// And response error is "partition not found"
				requireInitialRecoveryPartitionNotFound(t, response.SysError)
				// And no partition or workspace recovery has started for partition <target_partition>
				// target_partition = 2
				requireNoInitialRecoveryRuntimeStarts(t, logCap, 2)
				// And no command event has been appended for partition <target_partition>
				// target_partition = 2
				require.Equal(t, lastPLogOffset, getLastPLogOffset(t, vit, appQName, 2))
				// And deployment is still skipped for <failed_partitions>
				// failed_partitions = partitions 0 through 2
				requireInitialRecoveryPartitionsExcluded(t, vit, failed)
				// And no asynchronous projector or scheduled job runs for <failed_partitions>
				// failed_partitions = partitions 0 through 2
				requireNoInitialRecoveryWorkers(t, vit, failed)
			},
		})
	})

	t.Run("recovery: scn: Healthy partitions continue while a failed partition remains undeployed", func(t *testing.T) {
		// Given application "test1/app1" declares partition IDs 0 through 2
		control := newInitialRecoveryReadFailure()
		cfg := initialRecoveryFailureVITConfig(control, true)
		logCap := logger.StartCapture(t, logger.LogLevelVerbose)
		workspaces := map[istructs.PartitionID]*it.AppWorkspace{}
		it.TestRestartPreservingStorageWithHooks(t, &cfg, it.RestartPreservingStorageHooks{
			FirstRun: func(t *testing.T, vit *it.VIT) {
				owner := vit.GetPrincipal(istructs.AppQName_test1_app1, recoveryTestLoginName)
				for id := istructs.PartitionID(0); id < 3; id++ {
					workspaces[id] = createWorkspacesInPartition(t, vit, owner, id, 1)[0]
				}
				// And asynchronous projectors and scheduled jobs are configured for every partition
				requireConfiguredInitialRecoveryWorkers(t, vit)
			},
			AfterFirstStop: func(t *testing.T, storage it.RestartStorage) {
				removeInitialRecoveryMarkers(t, storage)
				control.planInitialRecoveryFailure([]istructs.PartitionID{1})
				logCap.Reset()
			},
			SecondRun: func(t *testing.T, vit *it.VIT) {
				// And the VVM has started with initial recovery failed for partition 1 with "PLog read failed"
				logCap.HasLine("stage=bootstrap.initial_recovery.error", "app=test1/app1", "partid=1", "PLog read failed")
				// And partitions 0 and 2 are deployed and the server is ready
				require.ElementsMatch(t, []istructs.PartitionID{0, 2}, deployedInitialRecoveryPartitions(t, vit))
				require.NotNil(t, vit.VoedgerVM)
				// When VAOperator submits commands to partitions 0 and 2 until runtime recovery completes
				var statuses []int
				for _, id := range []istructs.PartitionID{0, 2} {
					response := vit.PostWS(workspaces[id], "c.sys.CUD", cudBody1Doc)
					statuses = append(statuses, response.HTTPResp.StatusCode)
				}
				// Then the commands succeed on partitions 0 and 2
				require.Equal(t, []int{http.StatusOK, http.StatusOK}, statuses)
				// And partition 1 remains undeployed
				requireInitialRecoveryPartitionsExcluded(t, vit, []istructs.PartitionID{1})
				// And no asynchronous projector or scheduled job runs for partition 1
				requireNoInitialRecoveryWorkers(t, vit, []istructs.PartitionID{1})
				// When VAOperator submits a command to partition 1
				response := vit.PostWS(workspaces[1], "c.sys.CUD", cudBody1Doc, httpu.WithNoRetryPolicy(), httpu.Expect500())
				// Then response status is "500 Internal Server Error"
				require.Equal(t, http.StatusInternalServerError, response.HTTPResp.StatusCode)
				// And response error is "partition not found"
				requireInitialRecoveryPartitionNotFound(t, response.SysError)
			},
		})
	})

	t.Run("recovery: scn: A later bootstrap deploys a previously failed partition after successful initial recovery", func(t *testing.T) {
		// Given application "test1/app1" declares partition IDs 0 through 2
		control := newInitialRecoveryReadFailure()
		control.planInitialRecoveryFailure([]istructs.PartitionID{1})
		// No login provisioning is requested: the first run deliberately excludes partition 1.
		writes := &initialRecoveryCheckpointWrites{}
		cfg := initialRecoveryFailureVITConfig(control, false, writes)
		appQName := istructs.AppQName_test1_app1
		appID := istructs.ClusterApps[appQName]
		logCap := logger.StartCapture(t, logger.LogLevelVerbose)
		preserved := map[istructs.PartitionID]checkpoints.PartitionCheckpoint{}
		var appWSID istructs.WSID
		it.TestRestartPreservingStorageWithHooks(t, &cfg, it.RestartPreservingStorageHooks{
			FirstRun: func(t *testing.T, vit *it.VIT) {
				// And the VVM has started with initial recovery failed for partition 1 with "PLog read failed"
				logCap.HasLine("stage=bootstrap.initial_recovery.error", "app=test1/app1", "partid=1", "PLog read failed")
				store, err := vit.IAppStorageProvider.AppStorage(istructs.AppQName_sys_vvm)
				require.NoError(t, err)
				checkpointStorage := vvmstorage.NewRecoveryCheckpointStorage(store)
				// And partition 1 is undeployed and has no partition checkpoint
				requireInitialRecoveryPartitionsExcluded(t, vit, []istructs.PartitionID{1})
				_, present, err := checkpointStorage.GetPartitionCheckpoint(appID, 1)
				require.NoError(t, err)
				require.False(t, present)
				// And partitions 0 and 2 have stored partition checkpoints
				for _, id := range []istructs.PartitionID{0, 2} {
					checkpoint, present, err := checkpointStorage.GetPartitionCheckpoint(appID, id)
					require.NoError(t, err)
					require.True(t, present)
					preserved[id] = checkpoint
				}
				for index := istructs.WSID(0); index < 3; index++ {
					wsid := istructs.NewWSID(istructs.CurrentClusterID(), istructs.FirstBaseAppWSID+index)
					if coreutils.AppPartitionID(wsid, 3) == 1 {
						appWSID = wsid
					}
				}
				require.NotZero(t, appWSID)
			},
			AfterFirstStop: func(t *testing.T, _ it.RestartStorage) {
				// When the PLog read failure is resolved and VAOperator restarts the VVM
				control.resolve()
				writes.reset()
				logCap.Reset()
			},
			SecondRun: func(t *testing.T, vit *it.VIT) {
				store, err := vit.IAppStorageProvider.AppStorage(istructs.AppQName_sys_vvm)
				require.NoError(t, err)
				checkpointStorage := vvmstorage.NewRecoveryCheckpointStorage(store)
				// Then partition 1 receives a partition checkpoint after its workspace checkpoints are persisted
				_, present, err := checkpointStorage.GetPartitionCheckpoint(appID, 1)
				require.NoError(t, err)
				require.True(t, present)
				_, present, err = checkpointStorage.GetWorkspaceCheckpoint(appID, appWSID)
				require.NoError(t, err)
				require.True(t, present)
				workspaceKey, _ := vvmstorage.WorkspaceCheckpointKey(appID, appWSID)
				keyCapture := &bootstrapCheckpointKeyCapture{}
				_, _, err = vvmstorage.NewRecoveryCheckpointStorage(keyCapture).GetPartitionCheckpoint(appID, 1)
				require.NoError(t, err)
				workspaceOrder := writes.index(workspaceKey)
				partitionOrder := writes.index(keyCapture.key)
				require.GreaterOrEqual(t, workspaceOrder, 0)
				require.Greater(t, partitionOrder, workspaceOrder)
				// And the stored partition checkpoints for partitions 0 and 2 are unchanged
				for id, checkpoint := range preserved {
					actual, present, err := checkpointStorage.GetPartitionCheckpoint(appID, id)
					require.NoError(t, err)
					require.True(t, present)
					require.Equal(t, checkpoint, actual)
				}
				// And partitions 0 through 2 are deployed
				require.ElementsMatch(t, []istructs.PartitionID{0, 1, 2}, deployedInitialRecoveryPartitions(t, vit))
				// And the server becomes ready
				require.NotNil(t, vit.VoedgerVM)
				// When VAOperator submits a command to partition 1 until runtime recovery completes
				response := vit.PostApp(appQName, appWSID, "c.sys.CUD", `{"cuds":[]}`,
					httpu.WithAuthorizeBy(vit.GetSystemPrincipal(appQName).Token))
				// Then the command succeeds
				require.Equal(t, http.StatusOK, response.HTTPResp.StatusCode)
			},
		})
	})

	t.Run("recovery: scn: Partition recovery starts from a checkpoint: absent", func(t *testing.T) {
		// | checkpoint          |
		// | absent              |
		// Given the PLog contains events through offset 50
		// And the stored partition checkpoint is <checkpoint>
		// checkpoint = absent
		// When VAOperator restarts the VVM and submits a command
		// Then response status is "503 Service Unavailable" while partition recovery runs
		// When VAOperator retries the command after partition recovery completes
		// Then response status is "503 Service Unavailable" while workspace recovery runs
		// When VAOperator retries the command after workspace recovery completes
		// Then the command succeeds with a PLog event after offset 50
		// And no invalid partition-checkpoint error is logged
		runRecoveryCheckpointScenario(t, checkpointScopePartition, checkpointAbsent)
	})

	t.Run("recovery: scn: Partition recovery starts from a checkpoint: lastPLogOffset 0", func(t *testing.T) {
		// | checkpoint          |
		// | lastPLogOffset 0    |
		// Given the PLog contains events through offset 50
		// And the stored partition checkpoint is <checkpoint>
		// checkpoint = lastPLogOffset 0
		// When VAOperator restarts the VVM and submits a command
		// Then response status is "503 Service Unavailable" while partition recovery runs
		// When VAOperator retries the command after partition recovery completes
		// Then response status is "503 Service Unavailable" while workspace recovery runs
		// When VAOperator retries the command after workspace recovery completes
		// Then the command succeeds with a PLog event after offset 50
		// And no invalid partition-checkpoint error is logged
		runRecoveryCheckpointScenario(t, checkpointScopePartition, checkpointZero)
	})

	t.Run("recovery: scn: Partition recovery starts from a checkpoint: lastPLogOffset 10", func(t *testing.T) {
		// | checkpoint          |
		// | lastPLogOffset 10   |
		// Given the PLog contains events through offset 50
		// And the stored partition checkpoint is <checkpoint>
		// checkpoint = lastPLogOffset 10
		//                                                                         PLog end
		//                                                                          |
		//                                                                          v
		// PLog: -- 1 -- 2 -- ... -- 8 -- 9 -- 10 -- 11 -- 12 -- ... -- 48 -- 49 -- 50
		//                                     \___________ recovery scan __________/
		//                                     ^                                    ^
		//                                     |                                    |
		//              checkpoint: stored lastPLogOffset                  actual log end offset
		//
		// When VAOperator restarts the VVM and submits a command
		// Then response status is "503 Service Unavailable" while partition recovery runs
		// When VAOperator retries the command after partition recovery completes
		// Then response status is "503 Service Unavailable" while workspace recovery runs
		// When VAOperator retries the command after workspace recovery completes
		// Then the command succeeds with a PLog event after offset 50
		// And no invalid partition-checkpoint error is logged
		runRecoveryCheckpointScenario(t, checkpointScopePartition, checkpointValidButOutdated)
	})

	t.Run("recovery: scn: Partition recovery starts from a checkpoint: lastPLogOffset 50", func(t *testing.T) {
		// | checkpoint          |
		// | lastPLogOffset 50   |
		// Given the PLog contains events through offset 50
		// And the stored partition checkpoint is <checkpoint>
		// checkpoint = lastPLogOffset 50
		//                                 valid offset / PLog end
		//                                        |
		//                                        v
		// PLog: -- 1 -- 2 --  ... -- 48 -- 49 -- 50
		//                                        \_ recovery scan 50-50 inc.
		//                                        ^
		//                                        |
		//                   stored lastPLogOffset / actual log end offset
		//
		// When VAOperator restarts the VVM and submits a command
		// Then response status is "503 Service Unavailable" while partition recovery runs
		// When VAOperator retries the command after partition recovery completes
		// Then response status is "503 Service Unavailable" while workspace recovery runs
		// When VAOperator retries the command after workspace recovery completes
		// Then the command succeeds with a PLog event after offset 50
		// And no invalid partition-checkpoint error is logged
		runRecoveryCheckpointScenario(t, checkpointScopePartition, checkpointValid)
	})

	t.Run("recovery: scn: Invalid partition checkpoint falls back to the PLog beginning: lastPLogOffset 1000000 beyond the PLog end", func(t *testing.T) {
		// | checkpoint                                         | checkpoint_offset |
		// | lastPLogOffset 1000000 beyond the PLog end         | 1000000           |
		// Given the PLog contains events through offset 50
		// And the stored partition checkpoint is <checkpoint>
		// checkpoint = lastPLogOffset 1000000 beyond the PLog end
		//                                    PLog end                          invalid offset
		//                                       |                                   |
		//                                       v                                   v
		// PLog: -- 1 -- 2 -- ... -- 48 -- 49 -- 50       ... no events ...       1000000
		//          \__ fallback recovery scan __/                                   |
		//          ^                                                         stored lastPLogOffset
		//          |                                                        scan gaves no callbacks -> fallback
		//      FirstOffset
		//
		// When VAOperator restarts the VVM and submits a command
		// Then response status is "503 Service Unavailable" while partition recovery runs
		// When VAOperator retries the command after partition recovery completes
		// Then response status is "503 Service Unavailable" while workspace recovery runs
		// When VAOperator retries the command after workspace recovery completes
		// Then the command succeeds with a PLog event after offset 50
		// And error stage "cp.partition_recovery.checkpoint.invalid" is logged
		// And error message is "stored PLog offset <checkpoint_offset> does not identify an existing event; retrying from FirstOffset"
		// checkpoint_offset = 1000000
		runRecoveryCheckpointScenario(t, checkpointScopePartition, checkpointBeyondTail)
	})

	t.Run("recovery: scn: Invalid partition checkpoint falls back to the PLog beginning: lastPLogOffset 10 whose first returned event is 11", func(t *testing.T) {
		// | checkpoint                                         | checkpoint_offset |
		// | lastPLogOffset 10 whose first returned event is 11 | 10                |
		// Given the PLog contains events through offset 50
		// And the stored partition checkpoint is <checkpoint>
		// checkpoint = lastPLogOffset 10 whose first returned event is 11
		//                                  stored lastPLogOffset                      PLog end
		//                                           |                                   |
		//                                           v                                   v
		// PLog: -- 1 -- 2 -- ... -- 8 -- 9 -- [10 missing] -- 11 -- 12 -- ... -- 49 -- 50
		//          \________________ fallback recovery scan ____________________________/
		//          ^                                 \__________ initial scan __________/
		//          |                                              ^
		//      FirstOffset                                        |
		//                                              first callback is 11, not 10 -> fallback
		//
		// When VAOperator restarts the VVM and submits a command
		// Then response status is "503 Service Unavailable" while partition recovery runs
		// When VAOperator retries the command after partition recovery completes
		// Then response status is "503 Service Unavailable" while workspace recovery runs
		// When VAOperator retries the command after workspace recovery completes
		// Then the command succeeds with a PLog event after offset 50
		// And error stage "cp.partition_recovery.checkpoint.invalid" is logged
		// And error message is "stored PLog offset <checkpoint_offset> does not identify an existing event; retrying from FirstOffset"
		// checkpoint_offset = 10
		runRecoveryCheckpointScenario(t, checkpointScopePartition, checkpointMissingEvent)
	})

	t.Run("recovery: scn: Workspace recovery starts from a checkpoint: absent", func(t *testing.T) {
		// | checkpoint                |
		// | absent                    |
		// Given the WLog has an ID-bearing event at offset 6 with record ID 200001
		// And the WLog has an ID-bearing event at offset 10 with record IDs 200002 through 200005
		// And the WLog has a CUD-free event at offset 14
		// And the WLog ends at offset 14
		// And the stored workspace checkpoint is <checkpoint>
		// checkpoint = absent
		// And the VVM has restarted and completed partition recovery
		// When VAOperator submits an insert into the unrecovered workspace
		// Then response status is "503 Service Unavailable" while workspace recovery runs
		// When VAOperator retries the insert after workspace recovery completes
		// Then the insert succeeds with a WLog event at offset 15
		// And the inserted record has ID 200006
		// And no invalid workspace-checkpoint error is logged
		runRecoveryCheckpointScenario(t, checkpointScopeWorkspace, checkpointAbsent)
	})

	t.Run("recovery: scn: Workspace recovery starts from a checkpoint: wlogIDBearingOffset 0", func(t *testing.T) {
		// | checkpoint                |
		// | wlogIDBearingOffset 0     |
		// Given the WLog has an ID-bearing event at offset 6 with record ID 200001
		// And the WLog has an ID-bearing event at offset 10 with record IDs 200002 through 200005
		// And the WLog has a CUD-free event at offset 14
		// And the WLog ends at offset 14
		// And the stored workspace checkpoint is <checkpoint>
		// checkpoint = wlogIDBearingOffset 0
		// And the VVM has restarted and completed partition recovery
		// When VAOperator submits an insert into the unrecovered workspace
		// Then response status is "503 Service Unavailable" while workspace recovery runs
		// When VAOperator retries the insert after workspace recovery completes
		// Then the insert succeeds with a WLog event at offset 15
		// And the inserted record has ID 200006
		// And no invalid workspace-checkpoint error is logged
		runRecoveryCheckpointScenario(t, checkpointScopeWorkspace, checkpointZero)
	})

	t.Run("recovery: scn: Workspace recovery starts from a checkpoint: wlogIDBearingOffset 6", func(t *testing.T) {
		// | checkpoint                |
		// | wlogIDBearingOffset 6     |
		// Given the WLog has an ID-bearing event at offset 6 with record ID 200001
		// And the WLog has an ID-bearing event at offset 10 with record IDs 200002 through 200005
		// And the WLog has a CUD-free event at offset 14
		// And the WLog ends at offset 14
		// And the stored workspace checkpoint is <checkpoint>
		// checkpoint = wlogIDBearingOffset 6
		//                                                  latest ID-bearing event     WLog end
		//                                                             |                  |
		//                                                             v                  v
		// WLog: -- 1 -- 2 -- 3 -- 4 -- 5 -- 6 -- 7 -- 8 -- 9 -- 10 -- 11 -- 12 -- 13 -- 14
		//                                   \____________ recovery scan _________________/
		//                                   ^                         ^                  ^
		//                                   |                         |                  |
		//                      stored wlogIDBearingOffset       latest recovered IDs     actual log end
		//                           valid but outdated
		//
		// And the VVM has restarted and completed partition recovery
		// When VAOperator submits an insert into the unrecovered workspace
		// Then response status is "503 Service Unavailable" while workspace recovery runs
		// When VAOperator retries the insert after workspace recovery completes
		// Then the insert succeeds with a WLog event at offset 15
		// And the inserted record has ID 200006
		// And no invalid workspace-checkpoint error is logged
		runRecoveryCheckpointScenario(t, checkpointScopeWorkspace, checkpointValidButOutdated)
	})

	t.Run("recovery: scn: Workspace recovery starts from a checkpoint: wlogIDBearingOffset 10", func(t *testing.T) {
		// | checkpoint                |
		// | wlogIDBearingOffset 10    |
		// Given the WLog has an ID-bearing event at offset 6 with record ID 200001
		// And the WLog has an ID-bearing event at offset 10 with record IDs 200002 through 200005
		// And the WLog has a CUD-free event at offset 14
		// And the WLog ends at offset 14
		// And the stored workspace checkpoint is <checkpoint>
		// checkpoint = wlogIDBearingOffset 10
		//                                          the last ID-bearing offset        WLog end
		//                                                        |                       |
		//                                                        v                       v
		// WLog: -- 1 -- 2 -- 3 -- 4 -- 5 -- 6 -- 7 -- 8 -- 9 -- 10 -- 11 -- 12 -- 13 -- 14
		//                                                        \____ recovery scan ____/
		//                                                        ^                       ^
		//                                                        |                       |
		//                                           stored wlogIDBearingOffset      actual log end
		//
		// And the VVM has restarted and completed partition recovery
		// When VAOperator submits an insert into the unrecovered workspace
		// Then response status is "503 Service Unavailable" while workspace recovery runs
		// When VAOperator retries the insert after workspace recovery completes
		// Then the insert succeeds with a WLog event at offset 15
		// And the inserted record has ID 200006
		// And no invalid workspace-checkpoint error is logged
		runRecoveryCheckpointScenario(t, checkpointScopeWorkspace, checkpointValid)
	})

	t.Run("recovery: scn: Workspace recovery starts from an ID-bearing checkpoint at the WLog end", func(t *testing.T) {
		// Given the WLog has allocated record IDs 200001 through 200005
		// And its ID-bearing event at offset 10 is the WLog end
		// And the stored workspace checkpoint is wlogIDBearingOffset 10
		// And the VVM has restarted and completed partition recovery
		// When VAOperator submits an insert into the unrecovered workspace
		// Then response status is "503 Service Unavailable" while workspace recovery runs
		// When VAOperator retries the insert after workspace recovery completes
		// Then the insert succeeds with a WLog event at offset 11
		// And the inserted record has ID 200006
		// And no invalid workspace-checkpoint error is logged
		runRecoveryCheckpointScenario(t, checkpointScopeWorkspace, checkpointAtWLogTail)
	})

	t.Run("recovery: scn: Non-ID-bearing log tail events advance only the WLog offset: CUD-free events", func(t *testing.T) {
		// | tail_events                 |
		// | CUD-free events             |
		// Given the WLog has allocated record IDs 200001 through 200005 at offset 10
		// And the WLog has <tail_events> from offset 11 through offset 14
		// tail_events = CUD-free events
		// And the stored workspace checkpoint is wlogIDBearingOffset 10
		// And the VVM has restarted and completed partition recovery
		// When VAOperator submits an insert into the unrecovered workspace
		// Then response status is "503 Service Unavailable" while workspace recovery runs
		// When VAOperator retries the insert after workspace recovery completes
		// Then the insert succeeds with a WLog event at offset 15
		// And the inserted record has ID 200006
		// And no invalid workspace-checkpoint error is logged
		runRecoveryCheckpointScenario(t, checkpointScopeWorkspace, checkpointValid)
	})

	t.Run("recovery: scn: Non-ID-bearing log tail events advance only the WLog offset: singleton-only events", func(t *testing.T) {
		// | tail_events                 |
		// | singleton-only events       |
		// Given the WLog has allocated record IDs 200001 through 200005 at offset 10
		// And the WLog has <tail_events> from offset 11 through offset 14
		// tail_events = singleton-only events
		// And the stored workspace checkpoint is wlogIDBearingOffset 10
		// And the VVM has restarted and completed partition recovery
		// When VAOperator submits an insert into the unrecovered workspace
		// Then response status is "503 Service Unavailable" while workspace recovery runs
		// When VAOperator retries the insert after workspace recovery completes
		// Then the insert succeeds with a WLog event at offset 15
		// And the inserted record has ID 200006
		// And no invalid workspace-checkpoint error is logged
		runRecoveryCheckpointScenario(t, checkpointScopeWorkspace, checkpointSingletonSuffix)
	})

	t.Run("recovery: scn: Non-ID-bearing log tail events advance only the WLog offset: updates of existing records", func(t *testing.T) {
		// | tail_events                 |
		// | updates of existing records |
		// Given the WLog has allocated record IDs 200001 through 200005 at offset 10
		// And the WLog has <tail_events> from offset 11 through offset 14
		// tail_events = updates of existing records
		// And the stored workspace checkpoint is wlogIDBearingOffset 10
		// And the VVM has restarted and completed partition recovery
		// When VAOperator submits an insert into the unrecovered workspace
		// Then response status is "503 Service Unavailable" while workspace recovery runs
		// When VAOperator retries the insert after workspace recovery completes
		// Then the insert succeeds with a WLog event at offset 15
		// And the inserted record has ID 200006
		// And no invalid workspace-checkpoint error is logged
		runRecoveryCheckpointScenario(t, checkpointScopeWorkspace, checkpointExistingRecordSuffix)
	})

	t.Run("recovery: scn: Workspace without user record IDs starts allocation at FirstUserRecordID: absent", func(t *testing.T) {
		// | checkpoint            |
		// | absent                |
		// Given the WLog contains CUD-free and singleton-only events through offset 14
		// And the WLog contains no newly allocated non-singleton record ID
		// And the stored workspace checkpoint is <checkpoint>
		// checkpoint = absent
		// And the VVM has restarted and completed partition recovery
		// When VAOperator submits an insert into the unrecovered workspace
		// Then response status is "503 Service Unavailable" while workspace recovery runs
		// When VAOperator retries the insert after workspace recovery completes
		// Then the insert succeeds with a WLog event at offset 15
		// And the inserted record has ID 200001
		// And no invalid workspace-checkpoint error is logged
		runRecoveryCheckpointScenario(t, checkpointScopeWorkspace, checkpointNoUserIDsAbsent)
	})

	t.Run("recovery: scn: Workspace without user record IDs starts allocation at FirstUserRecordID: wlogIDBearingOffset 0", func(t *testing.T) {
		// | checkpoint            |
		// | wlogIDBearingOffset 0 |
		// Given the WLog contains CUD-free and singleton-only events through offset 14
		// And the WLog contains no newly allocated non-singleton record ID
		// And the stored workspace checkpoint is <checkpoint>
		// checkpoint = wlogIDBearingOffset 0
		// And the VVM has restarted and completed partition recovery
		// When VAOperator submits an insert into the unrecovered workspace
		// Then response status is "503 Service Unavailable" while workspace recovery runs
		// When VAOperator retries the insert after workspace recovery completes
		// Then the insert succeeds with a WLog event at offset 15
		// And the inserted record has ID 200001
		// And no invalid workspace-checkpoint error is logged
		runRecoveryCheckpointScenario(t, checkpointScopeWorkspace, checkpointNoUserIDsZero)
	})

	t.Run("recovery: scn: Invalid workspace checkpoint falls back to the WLog beginning: wlogIDBearingOffset 15 beyond the WLog end", func(t *testing.T) {
		// | checkpoint                                                   | checkpoint_offset |
		// | wlogIDBearingOffset 15 beyond the WLog end                   | 15                |
		// Given the WLog has an ID-bearing event at offset 6 with record ID 200001
		// And the WLog has a non-ID-bearing event at offset 9
		// And the WLog has an ID-bearing event at offset 10 with record IDs 200002 through 200005
		// And the WLog has a CUD-free event at offset 12
		// And the WLog has a singleton-only event at offset 13
		// And the WLog has an update of an existing record at offset 14
		// And the WLog ends at offset 14
		// And the stored workspace checkpoint is <checkpoint>
		// checkpoint = wlogIDBearingOffset 15 beyond the WLog end
		//                                                                            WLog end
		//                                                                               |
		//                                                                               v
		// WLog: -- 1 -- 2 -- 3 -- 4 -- 5 -- 6 -- 7 -- 8 -- 9 -- 10 -- 11 -- 12 -- 13 -- 14     no event at 15
		//          \____________________________ fallback recovery scan ________________/             ^
		//          ^                                                                                  |
		//          |                                                                        stored wlogIDBearingOffset
		//      FirstOffset                                                                    no callbacks on read
		//
		// And the VVM has restarted and completed partition recovery
		// When VAOperator submits an insert into the unrecovered workspace
		// Then response status is "503 Service Unavailable" while workspace recovery runs
		// When VAOperator retries the insert after workspace recovery completes
		// Then the insert succeeds with a WLog event at offset 15
		// And the inserted record has ID 200006
		// And error stage "cp.workspace_recovery.checkpoint.invalid" is logged
		// And error message is "stored WLog offset <checkpoint_offset> does not identify an ID-bearing event; retrying from FirstOffset"
		// checkpoint_offset = 15
		runRecoveryCheckpointScenario(t, checkpointScopeWorkspace, checkpointBeyondTail)
	})

	t.Run("recovery: scn: Invalid workspace checkpoint falls back to the WLog beginning: wlogIDBearingOffset 8 whose first returned event is 9", func(t *testing.T) {
		// | checkpoint                                                   | checkpoint_offset |
		// | wlogIDBearingOffset 8 whose first returned event is 9        | 8                 |
		// Given the WLog has an ID-bearing event at offset 6 with record ID 200001
		// And the WLog has a non-ID-bearing event at offset 9
		// And the WLog has an ID-bearing event at offset 10 with record IDs 200002 through 200005
		// And the WLog has a CUD-free event at offset 12
		// And the WLog has a singleton-only event at offset 13
		// And the WLog has an update of an existing record at offset 14
		// And the WLog ends at offset 14
		// And the stored workspace checkpoint is <checkpoint>
		// checkpoint = wlogIDBearingOffset 8 whose first returned event is 9
		//                                      stored wlogIDBearingOffset                       WLog end
		//                                                  |                                      |
		//                                                  v                                      v
		// WLog: -- 1 -- 2 -- 3 -- 4 -- 5 -- 6 -- 7 -- [8 missing] -- 9 -- 10 -- 11 -- 12 -- 13 -- 14
		//                                              \_initial scan, first event 9 -> invalid___/
		//          \________________________ fallback recovery scan ______________________________/
		//          ^
		//          |
		//      FirstOffset
		//
		// And the VVM has restarted and completed partition recovery
		// When VAOperator submits an insert into the unrecovered workspace
		// Then response status is "503 Service Unavailable" while workspace recovery runs
		// When VAOperator retries the insert after workspace recovery completes
		// Then the insert succeeds with a WLog event at offset 15
		// And the inserted record has ID 200006
		// And error stage "cp.workspace_recovery.checkpoint.invalid" is logged
		// And error message is "stored WLog offset <checkpoint_offset> does not identify an ID-bearing event; retrying from FirstOffset"
		// checkpoint_offset = 8
		runRecoveryCheckpointScenario(t, checkpointScopeWorkspace, checkpointMissingEvent)
	})

	t.Run("recovery: scn: Invalid workspace checkpoint falls back to the WLog beginning: wlogIDBearingOffset 12 pointing to a CUD-free event", func(t *testing.T) {
		// | checkpoint                                                   | checkpoint_offset |
		// | wlogIDBearingOffset 12 pointing to a CUD-free event          | 12                |
		// Given the WLog has an ID-bearing event at offset 6 with record ID 200001
		// And the WLog has a non-ID-bearing event at offset 9
		// And the WLog has an ID-bearing event at offset 10 with record IDs 200002 through 200005
		// And the WLog has a CUD-free event at offset 12
		// And the WLog has a singleton-only event at offset 13
		// And the WLog has an update of an existing record at offset 14
		// And the WLog ends at offset 14
		// And the stored workspace checkpoint is <checkpoint>
		// checkpoint = wlogIDBearingOffset 12 pointing to a CUD-free event
		//                                           CUD-free checkpoint     WLog end
		//                                                    |                  |
		//                                                    v                  v
		// WLog: -- 1 -- 2 -- 3 -- 4 -- ... -- 10 -- 11 -- 12 -- 13 -- 14
		//                                                    \_ initial scan ___/
		//          \____________ fallback recovery scan __________________/
		//          ^
		//          |
		//      FirstOffset
		//
		// And the VVM has restarted and completed partition recovery
		// When VAOperator submits an insert into the unrecovered workspace
		// Then response status is "503 Service Unavailable" while workspace recovery runs
		// When VAOperator retries the insert after workspace recovery completes
		// Then the insert succeeds with a WLog event at offset 15
		// And the inserted record has ID 200006
		// And error stage "cp.workspace_recovery.checkpoint.invalid" is logged
		// And error message is "stored WLog offset <checkpoint_offset> does not identify an ID-bearing event; retrying from FirstOffset"
		// checkpoint_offset = 12
		runRecoveryCheckpointScenario(t, checkpointScopeWorkspace, checkpointCUDFree)
	})

	t.Run("recovery: scn: Invalid workspace checkpoint falls back to the WLog beginning: wlogIDBearingOffset 13 pointing to a singleton-only event", func(t *testing.T) {
		// | checkpoint                                                   | checkpoint_offset |
		// | wlogIDBearingOffset 13 pointing to a singleton-only event    | 13                |
		// Given the WLog has an ID-bearing event at offset 6 with record ID 200001
		// And the WLog has a non-ID-bearing event at offset 9
		// And the WLog has an ID-bearing event at offset 10 with record IDs 200002 through 200005
		// And the WLog has a CUD-free event at offset 12
		// And the WLog has a singleton-only event at offset 13
		// And the WLog has an update of an existing record at offset 14
		// And the WLog ends at offset 14
		// And the stored workspace checkpoint is <checkpoint>
		// checkpoint = wlogIDBearingOffset 13 pointing to a singleton-only event
		// And the VVM has restarted and completed partition recovery
		// When VAOperator submits an insert into the unrecovered workspace
		// Then response status is "503 Service Unavailable" while workspace recovery runs
		// When VAOperator retries the insert after workspace recovery completes
		// Then the insert succeeds with a WLog event at offset 15
		// And the inserted record has ID 200006
		// And error stage "cp.workspace_recovery.checkpoint.invalid" is logged
		// And error message is "stored WLog offset <checkpoint_offset> does not identify an ID-bearing event; retrying from FirstOffset"
		// checkpoint_offset = 13
		runRecoveryCheckpointScenario(t, checkpointScopeWorkspace, checkpointSingleton)
	})

	t.Run("recovery: scn: Invalid workspace checkpoint falls back to the WLog beginning: wlogIDBearingOffset 14 pointing to an existing-record update", func(t *testing.T) {
		// | checkpoint                                                   | checkpoint_offset |
		// | wlogIDBearingOffset 14 pointing to an existing-record update | 14                |
		// Given the WLog has an ID-bearing event at offset 6 with record ID 200001
		// And the WLog has a non-ID-bearing event at offset 9
		// And the WLog has an ID-bearing event at offset 10 with record IDs 200002 through 200005
		// And the WLog has a CUD-free event at offset 12
		// And the WLog has a singleton-only event at offset 13
		// And the WLog has an update of an existing record at offset 14
		// And the WLog ends at offset 14
		// And the stored workspace checkpoint is <checkpoint>
		// checkpoint = wlogIDBearingOffset 14 pointing to an existing-record update
		// And the VVM has restarted and completed partition recovery
		// When VAOperator submits an insert into the unrecovered workspace
		// Then response status is "503 Service Unavailable" while workspace recovery runs
		// When VAOperator retries the insert after workspace recovery completes
		// Then the insert succeeds with a WLog event at offset 15
		// And the inserted record has ID 200006
		// And error stage "cp.workspace_recovery.checkpoint.invalid" is logged
		// And error message is "stored WLog offset <checkpoint_offset> does not identify an ID-bearing event; retrying from FirstOffset"
		// checkpoint_offset = 14
		runRecoveryCheckpointScenario(t, checkpointScopeWorkspace, checkpointExistingRecordUpdate)
	})
}

type checkpointScope int

const (
	checkpointScopePartition checkpointScope = iota
	checkpointScopeWorkspace
)

type checkpointKind int

const (
	checkpointAbsent checkpointKind = iota
	checkpointZero
	checkpointValidButOutdated
	checkpointValid
	checkpointAtWLogTail
	checkpointBeyondTail
	checkpointMissingEvent
	checkpointCUDFree
	checkpointSingleton
	checkpointExistingRecordUpdate
	checkpointSingletonSuffix
	checkpointExistingRecordSuffix
	checkpointNoUserIDsAbsent
	checkpointNoUserIDsZero
)

const (
	recoveryTestPLogTail             istructs.Offset = 50
	recoveryTestBeyondPLogTail       istructs.Offset = 1_000_000
	recoveryTestInitialWLogTail      istructs.Offset = 3
	recoveryTestFirstIDBearingOffset istructs.Offset = 6
	recoveryTestWLogIDBearingOffset  istructs.Offset = 10
	recoveryTestCUDFreeOffset        istructs.Offset = 12
	recoveryTestSingletonOffset      istructs.Offset = 13
	recoveryTestWLogTail             istructs.Offset = 14
	recoveryTestMissingWLogOffset    istructs.Offset = 8
)

func runRecoveryCheckpointScenario(t *testing.T, scope checkpointScope, kind checkpointKind) {
	t.Helper()
	appQName := istructs.AppQName_test1_app1
	appID := istructs.ClusterApps[appQName]
	cfg := it.NewOwnVITConfig(it.WithApp(appQName, provideRecoveryTestApp,
		it.WithUserLogin(recoveryTestLoginName, recoveryTestLoginPassword),
		it.WithChildWorkspace(it.QNameApp1_TestWSKind, recoveryTestWorkspaceName, "", "", recoveryTestLoginName, map[string]interface{}{"IntFld": 42})))
	logCap := logger.StartCapture(t, logger.LogLevelVerbose)

	var (
		partitionID istructs.PartitionID
		expected    expectedWorkspaceSequence
		currentWS   *it.AppWorkspace
		appStructs  istructs.IAppStructs
	)
	it.TestRestartPreservingStorageWithHooks(t, &cfg, it.RestartPreservingStorageHooks{
		FirstRun: func(t *testing.T, vit *it.VIT) {
			currentWS = vit.WS(appQName, recoveryTestWorkspaceName)
			var err error
			partitionID, err = vit.IAppPartitions.AppWorkspacePartitionID(appQName, currentWS.WSID)
			require.NoError(t, err)
			appStructs, err = vit.IAppStructsProvider.BuiltIn(appQName)
			require.NoError(t, err)
			plogOffsets := vit.SubscribeForN10nProjectionKey(in10n.ProjectionKey{
				App:        appQName,
				Projection: actualizers.PLogUpdatesQName,
				WS:         istructs.WSID(partitionID),
			})
			collectionOffsets := vit.SubscribeForN10n(currentWS, collection.QNameCollectionView)

			//                          ID-bearing                ID-bearing
			//                          one new ID              four new IDs             WLog end
			//                              |                         |                      |
			//                              v                         v                      v
			// WLog: -- 1 -- 2 -- 3 -- 4 -- 5 -- 6 -- 7 -- 8 -- 9 -- 10 -- 11 -- 12 -- 13 -- 14
			//       init prefix            ^                         ^                      ^
			//                              |                         |                      |
			//                     first ID-bearing          wlogIDBearingOffset    actual log end offset
			//
			expectedWLogTail := recoveryTestWLogTail
			nextRecordID := istructs.FirstUserRecordID
			collectionOffset := recoveryTestWLogIDBearingOffset
			expectedIDBearingOffset := recoveryTestWLogIDBearingOffset
			require.Equal(t, recoveryTestInitialWLogTail, getLastWLogOffset(t, vit, currentWS))
			if isNoUserIDsCheckpoint(kind) {
				appendSingletonOnlyEvents(t, vit, currentWS, appStructs, recoveryTestInitialWLogTail+1)
				fillWLogWithNonIDBearingEvents(t, vit, currentWS, recoveryTestWLogTail)
				collectionOffset = recoveryTestInitialWLogTail + 1
				expectedIDBearingOffset = istructs.NullOffset
			} else {
				fillWLogWithNonIDBearingEvents(t, vit, currentWS, recoveryTestFirstIDBearingOffset-1)
				firstIDBearing := vit.PostWS(currentWS, "c.sys.CUD", cudBody1Doc)
				require.Equal(t, recoveryTestFirstIDBearingOffset, firstIDBearing.CurrentWLogOffset)
				require.Len(t, firstIDBearing.NewIDs, 1)
				require.Equal(t, istructs.RecordID(200001), firstIDBearing.NewIDs["1"])

				fillWLogWithNonIDBearingEvents(t, vit, currentWS, recoveryTestWLogIDBearingOffset-1)
				lastIDBearing := vit.PostWS(currentWS, "c.sys.CUD", fourAppWorkspaceDocs)
				require.Equal(t, recoveryTestWLogIDBearingOffset, lastIDBearing.CurrentWLogOffset)
				require.Len(t, lastIDBearing.NewIDs, 4)
				for rawID := 1; rawID <= 4; rawID++ {
					require.Equal(t, istructs.RecordID(200001+rawID), lastIDBearing.NewIDs[strconv.Itoa(rawID)])
				}
				nextRecordID = lastIDBearing.NewIDs["4"] + 1

				if _, invalidCheckpoint := invalidWorkspaceCheckpointOffset(kind); invalidCheckpoint {
					fillWLogWithNonIDBearingEvents(t, vit, currentWS, recoveryTestCUDFreeOffset)
					singleton := vit.PostWS(currentWS, "c.sys.CUD", singletonCUD)
					require.Equal(t, recoveryTestSingletonOffset, singleton.CurrentWLogOffset)
					require.Empty(t, singleton.NewIDs)
					updated := vit.PostWS(currentWS, "c.sys.CUD", fmt.Sprintf(
						`{"cuds":[{"sys.ID":%d,"fields":{}}]}`, lastIDBearing.NewIDs["1"]))
					require.Equal(t, recoveryTestWLogTail, updated.CurrentWLogOffset)
					require.Empty(t, updated.NewIDs)
				} else {
					switch kind {
					case checkpointAtWLogTail:
						expectedWLogTail = recoveryTestWLogIDBearingOffset
					case checkpointSingletonSuffix:
						appendSingletonOnlyEvents(t, vit, currentWS, appStructs, recoveryTestWLogTail)
					case checkpointExistingRecordSuffix:
						appendExistingRecordUpdates(t, vit, currentWS, lastIDBearing.NewIDs["1"], recoveryTestWLogTail)
					default:
						fillWLogWithNonIDBearingEvents(t, vit, currentWS, recoveryTestWLogTail)
					}
				}
			}
			require.Equal(t, expectedWLogTail, getLastWLogOffset(t, vit, currentWS))
			fillPLogWithNonIDBearingEvents(t, vit, appQName, partitionID, currentWS.Owner, recoveryTestPLogTail)
			require.Equal(t, recoveryTestPLogTail, getLastPLogOffset(t, vit, appQName, partitionID))
			vit.WaitForOffset(plogOffsets, recoveryTestPLogTail)
			vit.WaitForOffset(collectionOffsets, collectionOffset)

			expected = expectedWorkspaceSequence{
				wsid:                currentWS.WSID,
				wlogIDBearingOffset: expectedIDBearingOffset,
				nextWLogOffset:      expectedWLogTail + 1,
				nextRecordID:        nextRecordID,
			}
		},
		AfterFirstStop: func(t *testing.T, storage it.RestartStorage) {
			// need to avoid recoveryCheckpointProjector modify the checkpoint
			// since we're going modify the checkpoint according to the test
			moveActualizerToPLogEnd(t, appStructs, partitionID)
			checkpointAppStorage := storage.AppStorage(t, istructs.AppQName_sys_vvm)
			checkpointStorage := vvmstorage.NewRecoveryCheckpointStorage(checkpointAppStorage)
			configureRecoveryCheckpoint(t, checkpointStorage, checkpointAppStorage,
				storage.AppStorage(t, appQName), appID, partitionID, expected.wsid, scope, kind)
			logCap.Reset()
		},
		SecondRun: func(t *testing.T, vit *it.VIT) {
			currentWS = vit.WS(appQName, recoveryTestWorkspaceName)
			require.Equal(t, expected.wsid, currentWS.WSID)

			// first request after restart -> 503, the partition is recovering
			resp := vit.PostWS(currentWS, "c.sys.CUD", cudBody1Doc, httpu.WithNoRetryPolicy(), httpu.Expect503())
			require.Equal(t, http.StatusServiceUnavailable, resp.HTTPResp.StatusCode)
			logCap.EventuallyHasLine("stage=cp.partition_recovery.complete", fmt.Sprintf("partid=%d", partitionID))

			resp = vit.PostWS(currentWS, "c.sys.CUD", cudBody1Doc, httpu.WithNoRetryPolicy(), httpu.Expect503())
			require.Equal(t, http.StatusServiceUnavailable, resp.HTTPResp.StatusCode)
			logCap.EventuallyHasLine("stage=cp.workspace_recovery.complete", fmt.Sprintf("wsid=%d", currentWS.WSID))

			currentWLogOffset := checkNextWorkspaceSequence(vit, currentWS, expected)
			checkLastPLogEvent(t, vit, appQName, partitionID, currentWS.WSID, recoveryTestPLogTail+1, currentWLogOffset)
			checkRecoveryCheckpointLogs(t, logCap, scope, kind)
		},
	})
}

func moveActualizerToPLogEnd(t *testing.T, appStructs istructs.IAppStructs, partitionID istructs.PartitionID) {
	t.Helper()
	view := appdefsys.ProjectionOffsetsView
	key := appStructs.ViewRecords().KeyBuilder(view.Name)
	key.PutInt32(view.Fields.Partition, int32(partitionID))
	key.PutQName(view.Fields.Projector, checkpoints.QNameProjectorRecoveryCheckpoint)
	value := appStructs.ViewRecords().NewValueBuilder(view.Name)
	value.PutInt64(view.Fields.Offset, int64(recoveryTestPLogTail))
	require.NoError(t, appStructs.ViewRecords().Put(istructs.NullWSID, key, value))
}

func fillWLogWithNonIDBearingEvents(t *testing.T, vit *it.VIT, workspace *it.AppWorkspace, tail istructs.Offset) {
	t.Helper()
	current := getLastWLogOffset(t, vit, workspace)
	require.LessOrEqual(t, current, tail)
	for current < tail {
		response := vit.PostWS(workspace, nonIDBearingCommand, "{}")
		current++
		require.Equal(t, current, response.CurrentWLogOffset)
		require.Empty(t, response.NewIDs)
	}
}

func appendSingletonOnlyEvents(t *testing.T, vit *it.VIT, workspace *it.AppWorkspace,
	appStructs istructs.IAppStructs, tail istructs.Offset) {
	t.Helper()
	current := getLastWLogOffset(t, vit, workspace)
	require.Less(t, current, tail)
	response := vit.PostWS(workspace, "c.sys.CUD", singletonCUD)
	current++
	require.Equal(t, current, response.CurrentWLogOffset)
	require.Empty(t, response.NewIDs)

	singletonID, err := appStructs.Records().GetSingletonID(appdef.NewQName("app1pkg", "Config"))
	require.NoError(t, err)
	for current < tail {
		response = vit.PostWS(workspace, "c.sys.CUD", fmt.Sprintf(
			`{"cuds":[{"sys.ID":%d,"fields":{}}]}`, singletonID))
		current++
		require.Equal(t, current, response.CurrentWLogOffset)
		require.Empty(t, response.NewIDs)
	}
}

func appendExistingRecordUpdates(t *testing.T, vit *it.VIT, workspace *it.AppWorkspace,
	recordID istructs.RecordID, tail istructs.Offset) {
	t.Helper()
	current := getLastWLogOffset(t, vit, workspace)
	require.LessOrEqual(t, current, tail)
	for current < tail {
		response := vit.PostWS(workspace, "c.sys.CUD", fmt.Sprintf(
			`{"cuds":[{"sys.ID":%d,"fields":{}}]}`, recordID))
		current++
		require.Equal(t, current, response.CurrentWLogOffset)
		require.Empty(t, response.NewIDs)
	}
}

func fillPLogWithNonIDBearingEvents(t *testing.T, vit *it.VIT, appQName appdef.AppQName,
	partitionID istructs.PartitionID, principal *it.Principal, tail istructs.Offset) {
	t.Helper()
	current := getLastPLogOffset(t, vit, appQName, partitionID)
	require.LessOrEqual(t, current, tail)
	for current < tail {
		response := vit.PostProfile(principal, nonIDBearingCommand, "{}")
		require.Empty(t, response.NewIDs)
		current++
	}
}

func initialRecoveryVITConfig() it.VITConfig {
	appQName := istructs.AppQName_test1_app1
	return it.NewOwnVITConfig(it.WithApp(appQName, provideRecoveryTestAppWithPartitions(3),
		it.WithUserLogin(recoveryTestLoginName, recoveryTestLoginPassword),
	))
}

func initialRecoveryFailureVITConfig(control *initialRecoveryReadFailure, withUser bool, observers ...*initialRecoveryCheckpointWrites) it.VITConfig {
	appQName := istructs.AppQName_test1_app1
	var writes *initialRecoveryCheckpointWrites
	if len(observers) > 0 {
		writes = observers[0]
	}
	var options []it.AppOptFunc
	if withUser {
		options = append(options, it.WithUserLogin(recoveryTestLoginName, recoveryTestLoginPassword))
	}
	return it.NewOwnVITConfig(
		it.WithApp(appQName, provideInitialRecoveryFailureTestApp, options...),
		it.WithVVMConfig(func(cfg *vvm.VVMConfig) {
			underlyingFactory := cfg.StorageFactory
			cfg.StorageFactory = func(currentTime timeu.ITime) (istorage.IAppStorageFactory, error) {
				underlying, err := underlyingFactory(currentTime)
				if err != nil {
					return nil, err
				}
				return &initialRecoveryFailureFactory{
					IAppStorageFactory: underlying,
					control:            control,
					checkpointWrites:   writes,
				}, nil
			}
		}),
	)
}

func provideInitialRecoveryFailureTestApp(apis builtinapps.APIs, cfg *istructsmem.AppConfigType, ep extensionpoints.IExtensionPoint) builtinapps.Def {
	def := provideRecoveryTestAppWithPartitions(3)(apis, cfg, ep)
	def.NumAppWorkspaces = 3 // Give every partition an app workspace and scheduled job.
	schema, err := it.SchemaTestApp1FS.ReadFile("schemaTestApp1.vsql")
	if err != nil {
		panic(err)
	}
	for index := range def.Packages {
		if def.Packages[index].Path == it.App1PkgPath {
			def.Packages[index].FS = fstest.MapFS{
				"schemaTestApp1.vsql": &fstest.MapFile{Data: schema},
				"initial_recovery_job.vsql": &fstest.MapFile{Data: []byte(`ALTER WORKSPACE sys.AppWorkspaceWS (
					EXTENSION ENGINE BUILTIN (JOB InitialRecoveryJob '@every 1h');
				);`)},
			}
		}
	}
	cfg.AddJobs(istructsmem.BuiltinJob{Name: appdef.NewQName("app1pkg", "InitialRecoveryJob"),
		Func: func(istructs.IState, istructs.IIntents) error { return nil }})
	return def
}

func prepareInitialRecoveryFailureHistory(t *testing.T, vit *it.VIT, partitionID istructs.PartitionID) (*it.AppWorkspace, istructs.Offset) {
	t.Helper()
	owner := vit.GetPrincipal(istructs.AppQName_test1_app1, recoveryTestLoginName)
	workspace := createWorkspacesInPartition(t, vit, owner, partitionID, 1)[0]
	vit.PostWS(workspace, nonIDBearingCommand, "{}")
	return workspace, getLastPLogOffset(t, vit, istructs.AppQName_test1_app1, partitionID)
}

func requireInitialRecoveryCheckpointPresence(t *testing.T, vit *it.VIT, failed []istructs.PartitionID) {
	t.Helper()
	store, err := vit.IAppStorageProvider.AppStorage(istructs.AppQName_sys_vvm)
	require.NoError(t, err)
	checkpointStorage := vvmstorage.NewRecoveryCheckpointStorage(store)
	for id := istructs.PartitionID(0); id < 3; id++ {
		_, present, err := checkpointStorage.GetPartitionCheckpoint(istructs.ClusterApps[istructs.AppQName_test1_app1], id)
		require.NoError(t, err)
		require.Equal(t, !containsPartition(failed, id), present)
	}
}

func requireInitialRecoveryFailureLogs(t *testing.T, logs logger.ILogCaptor, failed []istructs.PartitionID) {
	t.Helper()
	for _, id := range failed {
		logs.HasLine("stage=bootstrap.initial_recovery.error", "app=test1/app1",
			fmt.Sprintf("partid=%d", id), "read PLog", "PLog read failed")
	}
}

func removeInitialRecoveryMarkers(t *testing.T, storage it.RestartStorage) {
	t.Helper()
	store := storage.AppStorage(t, istructs.AppQName_sys_vvm)
	checkpointsStorage := vvmstorage.NewRecoveryCheckpointStorage(store)
	for id := istructs.PartitionID(0); id < 3; id++ {
		removePartitionCheckpoint(t, checkpointsStorage, store, istructs.ClusterApps[istructs.AppQName_test1_app1], id)
	}
}

func deployedInitialRecoveryPartitions(t *testing.T, vit *it.VIT) []istructs.PartitionID {
	t.Helper()
	var deployed []istructs.PartitionID
	for id := istructs.PartitionID(0); id < 3; id++ {
		partition, err := vit.IAppPartitions.Borrow(istructs.AppQName_test1_app1, id, appparts.ProcessorKind_Command)
		if errors.Is(err, appparts.ErrNotFound) {
			continue
		}
		require.NoError(t, err)
		partition.Release()
		deployed = append(deployed, id)
	}
	return deployed
}

func requireInitialRecoveryPartitionsExcluded(t *testing.T, vit *it.VIT, ids []istructs.PartitionID) {
	t.Helper()
	count, err := vit.IAppPartitions.AppPartsCount(istructs.AppQName_test1_app1)
	require.NoError(t, err)
	require.Equal(t, istructs.NumAppPartitions(3), count)
	for _, id := range ids {
		partition, err := vit.IAppPartitions.Borrow(istructs.AppQName_test1_app1, id, appparts.ProcessorKind_Command)
		require.Nil(t, partition)
		require.ErrorIs(t, err, appparts.ErrNotFound)
	}
}

func requireConfiguredInitialRecoveryWorkers(t *testing.T, vit *it.VIT) {
	t.Helper()
	actualizers := map[istructs.PartitionID][]appdef.QName{}
	for id, names := range vit.IAppPartitions.WorkedActualizers(istructs.AppQName_test1_app1) {
		actualizers[id] = names
	}
	jobs := map[istructs.PartitionID]bool{}
	for id, names := range vit.IAppPartitions.WorkedSchedulers(istructs.AppQName_test1_app1) {
		_, jobs[id] = names[appdef.NewQName("app1pkg", "InitialRecoveryJob")]
	}
	for id := istructs.PartitionID(0); id < 3; id++ {
		require.Contains(t, actualizers[id], checkpoints.QNameProjectorRecoveryCheckpoint)
		require.True(t, jobs[id], "partition %d must have a configured scheduler", id)
	}
}

func requireNoInitialRecoveryWorkers(t *testing.T, vit *it.VIT, ids []istructs.PartitionID) {
	t.Helper()
	for id, names := range vit.IAppPartitions.WorkedActualizers(istructs.AppQName_test1_app1) {
		if containsPartition(ids, id) {
			require.Empty(t, names, "excluded partition %d has actualizers", id)
		}
	}
	for id, jobs := range vit.IAppPartitions.WorkedSchedulers(istructs.AppQName_test1_app1) {
		if containsPartition(ids, id) {
			require.Empty(t, jobs, "excluded partition %d has schedulers", id)
		}
	}
}

func requireInitialRecoveryPartitionNotFound(t *testing.T, err error) {
	t.Helper()
	var responseError coreutils.SysError
	require.ErrorAs(t, err, &responseError)
	require.Equal(t, http.StatusInternalServerError, responseError.HTTPStatus)
	require.Equal(t, "partition not found", responseError.Message)
}

func requireNoInitialRecoveryRuntimeStarts(t *testing.T, logs logger.ILogCaptor, id istructs.PartitionID) {
	t.Helper()
	for _, stage := range []string{"cp.partition_recovery.start", "cp.workspace_recovery.start"} {
		require.Zero(t, countBootstrapLogLines(logs.String(), "stage="+stage, fmt.Sprintf("partid=%d", id)))
	}
}

func containsPartition(partitions []istructs.PartitionID, target istructs.PartitionID) bool {
	for _, partitionID := range partitions {
		if partitionID == target {
			return true
		}
	}
	return false
}

type initialRecoveryFailureFactory struct {
	istorage.IAppStorageFactory
	control          *initialRecoveryReadFailure
	checkpointWrites *initialRecoveryCheckpointWrites
}

func (f *initialRecoveryFailureFactory) AppStorage(appName istorage.SafeAppName) (istorage.IAppStorage, error) {
	storage, err := f.IAppStorageFactory.AppStorage(appName)
	if err == nil && f.checkpointWrites != nil && strings.HasPrefix(appName.String(), "sysvvm") {
		return &initialRecoveryCheckpointWriteStorage{IAppStorage: storage, writes: f.checkpointWrites}, nil
	}
	if err != nil || !strings.HasPrefix(appName.String(), "test1app1") {
		return storage, err
	}
	return &initialRecoveryFailureStorage{IAppStorage: storage, control: f.control}, nil
}

type initialRecoveryFailureStorage struct {
	istorage.IAppStorage
	control *initialRecoveryReadFailure
}

func (s *initialRecoveryFailureStorage) Read(ctx context.Context, pKey, startCCols, finishCCols []byte,
	callback istorage.ReadCallback) error {
	if err := s.control.readError(pKey); err != nil {
		return err
	}
	return s.IAppStorage.Read(ctx, pKey, startCCols, finishCCols, callback)
}

// initialRecoveryReadFailure controls only the storage dependency:
//
//	planInitialRecoveryFailure(ids) -> selected PLog keys return an error
//	                                -> unrelated reads use real storage
//	resolve()                       -> all reads use real storage
//
// Resolving storage does not deploy excluded partitions; only a later bootstrap can.
type initialRecoveryReadFailure struct {
	mu         sync.Mutex
	partitions map[string]bool
}

func newInitialRecoveryReadFailure() *initialRecoveryReadFailure {
	return &initialRecoveryReadFailure{partitions: map[string]bool{}}
}

func (c *initialRecoveryReadFailure) planInitialRecoveryFailure(ids []istructs.PartitionID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range ids {
		key, _ := istructsmem.PLogKey(id, istructs.FirstOffset)
		c.partitions[string(key)] = true
	}
}

func (c *initialRecoveryReadFailure) readError(key []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.partitions[string(key)] {
		return errors.New("PLog read failed")
	}
	return nil
}

func (c *initialRecoveryReadFailure) resolve() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.partitions)
}

// Observe checkpoint persistence at the storage boundary without changing initializer behavior.
type initialRecoveryCheckpointWriteStorage struct {
	istorage.IAppStorage
	writes *initialRecoveryCheckpointWrites
}

func (s *initialRecoveryCheckpointWriteStorage) Put(key, columns, value []byte) error {
	if err := s.IAppStorage.Put(key, columns, value); err != nil {
		return err
	}
	s.writes.mu.Lock()
	defer s.writes.mu.Unlock()
	s.writes.keys = append(s.writes.keys, bytes.Clone(key))
	return nil
}

type initialRecoveryCheckpointWrites struct {
	mu   sync.Mutex
	keys [][]byte
}

func (c *initialRecoveryCheckpointWrites) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys = nil
}

func (c *initialRecoveryCheckpointWrites) index(key []byte) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	for index, written := range c.keys {
		if bytes.Equal(written, key) {
			return index
		}
	}
	return -1
}

func createWorkspacesInPartition(t *testing.T, vit *it.VIT, owner *it.Principal,
	partitionID istructs.PartitionID, count int) []*it.AppWorkspace {
	t.Helper()
	result := make([]*it.AppWorkspace, 0, count)
	for candidate := 0; len(result) < count; candidate++ {
		workspace := vit.CreateWorkspace(it.SimpleWSParams(fmt.Sprintf("initial-recovery-%d-%d", partitionID, candidate)), owner)
		actualPartitionID, err := vit.IAppPartitions.AppWorkspacePartitionID(workspace.AppQName(), workspace.WSID)
		require.NoError(t, err)
		if actualPartitionID == partitionID {
			result = append(result, workspace)
		}
	}
	return result
}

func removePartitionCheckpoint(t *testing.T, checkpointStorage checkpoints.IRecoveryCheckpointStorage,
	storage istorage.IAppStorage, appID istructs.ClusterAppID, partitionID istructs.PartitionID) {
	t.Helper()
	_, ok, err := checkpointStorage.GetPartitionCheckpoint(appID, partitionID)
	require.NoError(t, err)
	if !ok {
		return
	}
	workspacePKey, cCols := vvmstorage.WorkspaceCheckpointKey(appID, 0)
	prefix := binary.BigEndian.Uint32(workspacePKey[:4]) - 1
	pKey := make([]byte, 0, 10)
	pKey = binary.BigEndian.AppendUint32(pKey, prefix)
	pKey = binary.BigEndian.AppendUint32(pKey, uint32(appID))
	pKey = binary.BigEndian.AppendUint16(pKey, uint16(partitionID))
	deleteFromStorage(t, storage, pKey, cCols)
}

func deleteWorkspaceCheckpoint(t *testing.T, checkpointStorage checkpoints.IRecoveryCheckpointStorage,
	storage istorage.IAppStorage, appID istructs.ClusterAppID, wsid istructs.WSID) {
	t.Helper()
	_, ok, err := checkpointStorage.GetWorkspaceCheckpoint(appID, wsid)
	require.NoError(t, err)
	if !ok {
		return
	}
	pKey, cCols := vvmstorage.WorkspaceCheckpointKey(appID, wsid)
	deleteFromStorage(t, storage, pKey, cCols)
}

func getLastPLogOffset(t *testing.T, vit *it.VIT, appQName appdef.AppQName,
	partitionID istructs.PartitionID) istructs.Offset {
	t.Helper()
	appStructs, err := vit.IAppStructsProvider.BuiltIn(appQName)
	require.NoError(t, err)
	tail := istructs.NullOffset
	err = appStructs.Events().ReadPLog(t.Context(), partitionID, istructs.FirstOffset, istructs.ReadToTheEnd,
		func(offset istructs.Offset, event istructs.IPLogEvent) error {
			defer event.Release()
			tail = offset
			return nil
		})
	require.NoError(t, err)
	return tail
}

func getLastWLogOffset(t *testing.T, vit *it.VIT, workspace *it.AppWorkspace) istructs.Offset {
	t.Helper()
	appStructs, err := vit.IAppStructsProvider.BuiltIn(workspace.AppQName())
	require.NoError(t, err)
	tail := istructs.NullOffset
	err = appStructs.Events().ReadWLog(t.Context(), workspace.WSID, istructs.FirstOffset, istructs.ReadToTheEnd,
		func(offset istructs.Offset, event istructs.IWLogEvent) error {
			defer event.Release()
			tail = offset
			return nil
		})
	require.NoError(t, err)
	return tail
}

func configureRecoveryCheckpoint(t *testing.T, checkpointStorage checkpoints.IRecoveryCheckpointStorage,
	checkpointAppStorage, appStorage istorage.IAppStorage, appID istructs.ClusterAppID,
	partitionID istructs.PartitionID, wsid istructs.WSID, scope checkpointScope, kind checkpointKind) {
	t.Helper()

	if scope == checkpointScopePartition {
		configurePartitionCheckpoint(t, checkpointStorage, checkpointAppStorage, appStorage, appID, partitionID, kind)
		return
	}

	require.NoError(t, checkpointStorage.PutPartitionCheckpoint(appID, partitionID, checkpoints.PartitionCheckpoint{LastPLogOffset: recoveryTestPLogTail}))
	configureWorkspaceCheckpoint(t, checkpointStorage, checkpointAppStorage, appStorage, appID, wsid, kind)
}

func configurePartitionCheckpoint(t *testing.T, checkpointStorage checkpoints.IRecoveryCheckpointStorage,
	checkpointAppStorage, appStorage istorage.IAppStorage, appID istructs.ClusterAppID,
	partitionID istructs.PartitionID, kind checkpointKind) {
	t.Helper()
	var offset istructs.Offset
	switch kind {
	case checkpointAbsent:
		removePartitionCheckpoint(t, checkpointStorage, checkpointAppStorage, appID, partitionID)
		_, ok, err := checkpointStorage.GetPartitionCheckpoint(appID, partitionID)
		require.NoError(t, err)
		require.False(t, ok)
		return
	case checkpointZero:
		offset = istructs.NullOffset
	case checkpointValidButOutdated:
		offset = 10
	case checkpointValid:
		offset = recoveryTestPLogTail
	case checkpointBeyondTail:
		offset = recoveryTestBeyondPLogTail
	case checkpointMissingEvent:
		offset = 10
		pKey, cCols := istructsmem.PLogKey(partitionID, offset)
		deleteFromStorage(t, appStorage, pKey, cCols)
	default:
		require.FailNow(t, "unsupported partition checkpoint kind", "%d", kind)
	}
	require.NoError(t, checkpointStorage.PutPartitionCheckpoint(appID, partitionID,
		checkpoints.PartitionCheckpoint{LastPLogOffset: offset}))
}

func configureWorkspaceCheckpoint(t *testing.T, checkpointStorage checkpoints.IRecoveryCheckpointStorage,
	checkpointAppStorage, appStorage istorage.IAppStorage, appID istructs.ClusterAppID,
	wsid istructs.WSID, kind checkpointKind) {
	t.Helper()
	var offset istructs.Offset
	switch kind {
	case checkpointAbsent, checkpointNoUserIDsAbsent:
		pKey, cCols := vvmstorage.WorkspaceCheckpointKey(appID, wsid)
		_, exists, err := checkpointStorage.GetWorkspaceCheckpoint(appID, wsid)
		require.NoError(t, err)
		if exists {
			deleteFromStorage(t, checkpointAppStorage, pKey, cCols)
		}
		_, ok, err := checkpointStorage.GetWorkspaceCheckpoint(appID, wsid)
		require.NoError(t, err)
		require.False(t, ok)
		return
	case checkpointZero, checkpointNoUserIDsZero:
		offset = istructs.NullOffset
	case checkpointValidButOutdated:
		offset = recoveryTestFirstIDBearingOffset
	case checkpointValid, checkpointSingletonSuffix, checkpointExistingRecordSuffix:
		offset = recoveryTestWLogIDBearingOffset
	case checkpointAtWLogTail:
		offset = recoveryTestWLogIDBearingOffset
	default:
		var invalid bool
		offset, invalid = invalidWorkspaceCheckpointOffset(kind)
		if !invalid {
			require.FailNow(t, "unsupported workspace checkpoint kind", "%d", kind)
		}
		if kind == checkpointMissingEvent {
			pKey, cCols := istructsmem.WLogKey(wsid, offset)
			deleteFromStorage(t, appStorage, pKey, cCols)
		}
	}
	require.NoError(t, checkpointStorage.PutWorkspaceCheckpoint(appID, wsid,
		checkpoints.WorkspaceCheckpoint{WLogIDBearingOffset: offset}))
}

func checkRecoveryCheckpointLogs(t *testing.T, logCap logger.ILogCaptor, scope checkpointScope,
	kind checkpointKind) {
	t.Helper()
	partitionCheckpointInvalid := kind == checkpointBeyondTail || kind == checkpointMissingEvent
	if scope == checkpointScopePartition && partitionCheckpointInvalid {
		offset := recoveryTestBeyondPLogTail
		if kind == checkpointMissingEvent {
			offset = 10
		}
		message := fmt.Sprintf(
			"stored PLog offset %d does not identify an existing event; retrying from FirstOffset", offset)
		logCap.HasLine("level=ERROR", "stage=cp.partition_recovery.checkpoint.invalid",
			fmt.Sprintf("msg=%q", message))
	} else {
		logCap.NotContains("cp.partition_recovery.checkpoint.invalid")
	}

	offset, workspaceCheckpointInvalid := invalidWorkspaceCheckpointOffset(kind)
	if scope == checkpointScopeWorkspace && workspaceCheckpointInvalid {
		message := fmt.Sprintf(
			"stored WLog offset %d does not identify an ID-bearing event; retrying from FirstOffset", offset)
		logCap.HasLine("level=ERROR", "stage=cp.workspace_recovery.checkpoint.invalid",
			fmt.Sprintf("msg=%q", message))
	} else {
		logCap.NotContains("cp.workspace_recovery.checkpoint.invalid")
	}
}

func isNoUserIDsCheckpoint(kind checkpointKind) bool {
	return kind == checkpointNoUserIDsAbsent || kind == checkpointNoUserIDsZero
}

func invalidWorkspaceCheckpointOffset(kind checkpointKind) (istructs.Offset, bool) {
	switch kind {
	case checkpointBeyondTail:
		return recoveryTestWLogTail + 1, true
	case checkpointMissingEvent:
		return recoveryTestMissingWLogOffset, true
	case checkpointCUDFree:
		return recoveryTestCUDFreeOffset, true
	case checkpointSingleton:
		return recoveryTestSingletonOffset, true
	case checkpointExistingRecordUpdate:
		return recoveryTestWLogTail, true
	default:
		return istructs.NullOffset, false
	}
}

func deleteFromStorage(t *testing.T, storage istorage.IAppStorage, pKey, cCols []byte) {
	t.Helper()
	var value []byte
	ok, err := storage.Get(pKey, cCols, &value)
	require.NoError(t, err)
	require.True(t, ok, "storage cell to delete does not exist")
	ok, err = storage.CompareAndDelete(pKey, cCols, value)
	require.NoError(t, err)
	require.True(t, ok, "storage cell changed before deletion")
}

func provideRecoveryTestApp(apis builtinapps.APIs, cfg *istructsmem.AppConfigType, ep extensionpoints.IExtensionPoint) builtinapps.Def {
	return provideRecoveryTestAppWithPartitions(1)(apis, cfg, ep)
}

func provideRecoveryTestAppWithPartitions(numPartitions istructs.NumAppPartitions) builtinapps.Builder {
	return func(apis builtinapps.APIs, cfg *istructsmem.AppConfigType, ep extensionpoints.IExtensionPoint) builtinapps.Def {
		def := it.ProvideApp1(apis, cfg, ep)
		def.NumParts = numPartitions
		def.NumAppWorkspaces = 1
		return def
	}
}

func checkNextWorkspaceSequence(vit *it.VIT, workspace *it.AppWorkspace,
	expected expectedWorkspaceSequence) istructs.Offset {
	require := require.New(vit.T)

	// insert a test doc and check its counters
	resp := vit.PostWS(workspace, "c.sys.CUD", singleDoc)
	require.Len(resp.NewIDs, 1)
	require.Equal(expected.nextRecordID, resp.NewIDs["1"], "unexpected NewID in response at WLog offset %d", resp.CurrentWLogOffset)
	require.Equal(expected.nextWLogOffset, resp.CurrentWLogOffset, "unexpected WLog offset for NewID %d", resp.NewIDs["1"])

	// check the CurrentWLogOffset is the actual WLog end
	appStructs, err := vit.IAppStructsProvider.BuiltIn(workspace.AppQName())
	require.NoError(err)
	eventCount := 0
	err = appStructs.Events().ReadWLog(vit.T.Context(), workspace.WSID, resp.CurrentWLogOffset, istructs.ReadToTheEnd,
		func(wlogOffset istructs.Offset, event istructs.IWLogEvent) error {
			defer event.Release()
			eventCount++
			require.Equal(resp.CurrentWLogOffset, wlogOffset)
			cudCount := 0
			for cud := range event.CUDs {
				cudCount++
				require.Equal(resp.NewIDs["1"], cud.ID())
			}
			require.Equal(1, cudCount)
			return nil
		})
	require.NoError(err)
	require.Equal(1, eventCount, "unexpected events after WLog offset %d", resp.CurrentWLogOffset)
	return resp.CurrentWLogOffset
}

func checkLastPLogEvent(t *testing.T, vit *it.VIT, appQName appdef.AppQName,
	partitionID istructs.PartitionID, wsid istructs.WSID, expectedLastPLogOffset, expectedWLogOffset istructs.Offset) {
	t.Helper()
	appStructs, err := vit.IAppStructsProvider.BuiltIn(appQName)
	require.NoError(t, err)
	eventCount := 0
	err = appStructs.Events().ReadPLog(t.Context(), partitionID, expectedLastPLogOffset, istructs.ReadToTheEnd,
		func(offset istructs.Offset, event istructs.IPLogEvent) error {
			defer event.Release()
			eventCount++
			require.Equal(t, expectedLastPLogOffset, offset)
			require.Equal(t, wsid, event.Workspace())
			require.Equal(t, expectedWLogOffset, event.WLogOffset())
			return nil
		})
	require.NoError(t, err)
	require.Equal(t, 1, eventCount, "unexpected events after PLog offset %d", expectedLastPLogOffset)
}

func requireStoredWorkspaceCheckpoint(t *testing.T, checkpointStorage checkpoints.IRecoveryCheckpointStorage,
	appID istructs.ClusterAppID, expected expectedWorkspaceSequence) {
	t.Helper()
	checkpoint, ok, err := checkpointStorage.GetWorkspaceCheckpoint(appID, expected.wsid)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, expected.wlogIDBearingOffset, checkpoint.WLogIDBearingOffset)
}
