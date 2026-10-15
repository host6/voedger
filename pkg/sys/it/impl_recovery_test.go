/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package sys_it

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/appdef"
	appdefsys "github.com/voedger/voedger/pkg/appdef/sys"
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

	t.Run("recovery: scn: Missing checkpoints are computed for every declared partition", func(t *testing.T) {
		// Given an application declares partition IDs 0 through 2
		cfg := initialRecoveryVITConfig()
		appQName := istructs.AppQName_test1_app1
		appID := istructs.ClusterApps[appQName]
		var workspaceWithIDs, workspaceWithoutIDs *it.AppWorkspace
		var partitionTwoTail istructs.Offset

		it.TestRestartPreservingStorageWithHooks(t, &cfg, it.RestartPreservingStorageHooks{
			FirstRun: func(t *testing.T, vit *it.VIT) {
				owner := vit.GetPrincipal(appQName, recoveryTestLoginName)
				workspaces := createWorkspacesInPartition(t, vit, owner, 1, 2)
				workspaceWithIDs, workspaceWithoutIDs = workspaces[0], workspaces[1]

				// And partition 1 PLog ends at offset 50 and contains workspace histories:
				//   | WSID | latest ID-bearing WLog offset |
				//   | 10   | 6                             |
				//   | 11   | none                          |
				fillWLogWithNonIDBearingEvents(t, vit, workspaceWithIDs, 5)
				response := vit.PostWS(workspaceWithIDs, "c.sys.CUD", cudBody1Doc)
				require.Equal(t, istructs.Offset(6), response.CurrentWLogOffset)
				fillPartitionWithNonIDBearingEvents(t, vit, workspaceWithoutIDs, 50)
				require.Equal(t, istructs.Offset(50), getLastPLogOffset(t, vit, appQName, 1))

				// And partition 2 has no stored partition checkpoint
				// And partition 2 PLog is empty
				partitionTwoTail = getLastPLogOffset(t, vit, appQName, 2)
			},
			AfterFirstStop: func(t *testing.T, storage it.RestartStorage) {
				checkpointAppStorage := storage.AppStorage(t, istructs.AppQName_sys_vvm)
				checkpointStorage := vvmstorage.NewRecoveryCheckpointStorage(checkpointAppStorage)
				for partitionID := istructs.PartitionID(0); partitionID < 3; partitionID++ {
					removePartitionCheckpoint(t, checkpointStorage, checkpointAppStorage, appID, partitionID)
				}
				deleteWorkspaceCheckpoint(t, checkpointStorage, checkpointAppStorage, appID, workspaceWithIDs.WSID)
				deleteWorkspaceCheckpoint(t, checkpointStorage, checkpointAppStorage, appID, workspaceWithoutIDs.WSID)
				// And partition 0 has stored checkpoint lastPLogOffset 20
				require.NoError(t, checkpointStorage.PutPartitionCheckpoint(appID, 0,
					checkpoints.PartitionCheckpoint{LastPLogOffset: 20}))
				deletePLogEvents(t, storage.AppStorage(t, appQName), 2, partitionTwoTail)
				for _, partitionID := range []istructs.PartitionID{1, 2} {
					_, ok, err := checkpointStorage.GetPartitionCheckpoint(appID, partitionID)
					require.NoError(t, err)
					require.False(t, ok)
				}
			},
			// When VAOperator starts the VVM
			SecondRun: func(t *testing.T, vit *it.VIT) {
				checkpointAppStorage, err := vit.IAppStorageProvider.AppStorage(istructs.AppQName_sys_vvm)
				require.NoError(t, err)
				checkpointStorage := vvmstorage.NewRecoveryCheckpointStorage(checkpointAppStorage)

				// Then partition 0 retains checkpoint lastPLogOffset 20
				requirePartitionCheckpoint(t, checkpointStorage, appID, 0, 20)
				// And workspace checkpoints for partition 1 are persisted before its partition checkpoint:
				//   | WSID | wlogIDBearingOffset |
				//   | 10   | 6                   |
				//   | 11   | 0                   |
				requireWorkspaceCheckpointOffset(t, checkpointStorage, appID, workspaceWithIDs.WSID, 6)
				requireWorkspaceCheckpointOffset(t, checkpointStorage, appID, workspaceWithoutIDs.WSID, 0)
				// And partition checkpoints are persisted:
				//   | partition ID | lastPLogOffset |
				//   | 1            | 50             |
				//   | 2            | 0              |
				requirePartitionCheckpoint(t, checkpointStorage, appID, 1, 50)
				requirePartitionCheckpoint(t, checkpointStorage, appID, 2, 0)
				// And the server becomes ready
				require.NotNil(t, vit.VoedgerVM)
			},
		})
	})

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
		// | failed_partitions      | checkpoint_state                    | target_partition |
		// | partition 1            | present for partitions 0 and 2 only | 1                |
		// Given an application declares partition IDs 0 through 2 with no stored partition checkpoints
		// And initial recovery fails for <failed_partitions> with "PLog read failed"
		// failed_partitions = partition 1
		// When VAOperator starts the VVM
		// Then partition checkpoints are <checkpoint_state>
		// checkpoint_state = present for partitions 0 and 2 only
		// And initial-recovery error "PLog read failed" is logged for <failed_partitions>
		// failed_partitions = partition 1
		// And the server becomes ready
		// When VAOperator submits a command to partition <target_partition>
		// target_partition = 1
		// Then response status is "503 Service Unavailable" while partition recovery runs
		// When partition recovery fails with "PLog read failed"
		// And VAOperator submits another command to partition <target_partition>
		// target_partition = 1
		// Then response status is "500 Internal Server Error"
		// And response error is "partition <target_partition>: recovery failed: PLog read failed"
		// target_partition = 1
		// And exactly one retry of partition <target_partition> recovery starts
		// target_partition = 1
		// When VAOperator submits another command before that retry completes
		// Then response status is "503 Service Unavailable" while the retry runs
		runInitialRecoveryFailureScenario(t, []istructs.PartitionID{1}, 1)
	})

	t.Run("recovery: scn: Initial recovery failures do not prevent server readiness: partitions 0 through 2", func(t *testing.T) {
		// | failed_partitions      | checkpoint_state                    | target_partition |
		// | partitions 0 through 2 | absent for every partition          | 0                |
		// Given an application declares partition IDs 0 through 2 with no stored partition checkpoints
		// And initial recovery fails for <failed_partitions> with "PLog read failed"
		// failed_partitions = partitions 0 through 2
		// When VAOperator starts the VVM
		// Then partition checkpoints are <checkpoint_state>
		// checkpoint_state = absent for every partition
		// And initial-recovery error "PLog read failed" is logged for <failed_partitions>
		// failed_partitions = partitions 0 through 2
		// And the server becomes ready
		// When VAOperator submits a command to partition <target_partition>
		// target_partition = 0
		// Then response status is "503 Service Unavailable" while partition recovery runs
		// When partition recovery fails with "PLog read failed"
		// And VAOperator submits another command to partition <target_partition>
		// target_partition = 0
		// Then response status is "500 Internal Server Error"
		// And response error is "partition <target_partition>: recovery failed: PLog read failed"
		// target_partition = 0
		// And exactly one retry of partition <target_partition> recovery starts
		// target_partition = 0
		// When VAOperator submits another command before that retry completes
		// Then response status is "503 Service Unavailable" while the retry runs
		runInitialRecoveryFailureScenario(t, []istructs.PartitionID{0, 1, 2}, 0)
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
		//              checkpoint: stored lastPLogOffset                  actual tail offset
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
		//                   stored lastPLogOffset / actual tail offset
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
		//                      stored wlogIDBearingOffset       latest recovered IDs     actual tail
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
		//                                           stored wlogIDBearingOffset      actual tail
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

	t.Run("recovery: scn: Workspace recovery starts from an ID-bearing checkpoint at the WLog tail", func(t *testing.T) {
		// Given the WLog has allocated record IDs 200001 through 200005
		// And its ID-bearing event at offset 10 is the WLog tail
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

	t.Run("recovery: scn: Non-ID-bearing suffix events advance only the WLog offset: CUD-free events", func(t *testing.T) {
		// | suffix_events               |
		// | CUD-free events             |
		// Given the WLog has allocated record IDs 200001 through 200005 at offset 10
		// And the WLog has <suffix_events> from offset 11 through offset 14
		// suffix_events = CUD-free events
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

	t.Run("recovery: scn: Non-ID-bearing suffix events advance only the WLog offset: singleton-only events", func(t *testing.T) {
		// | suffix_events               |
		// | singleton-only events       |
		// Given the WLog has allocated record IDs 200001 through 200005 at offset 10
		// And the WLog has <suffix_events> from offset 11 through offset 14
		// suffix_events = singleton-only events
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

	t.Run("recovery: scn: Non-ID-bearing suffix events advance only the WLog offset: updates of existing records", func(t *testing.T) {
		// | suffix_events               |
		// | updates of existing records |
		// Given the WLog has allocated record IDs 200001 through 200005 at offset 10
		// And the WLog has <suffix_events> from offset 11 through offset 14
		// suffix_events = updates of existing records
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
			//                     first ID-bearing          wlogIDBearingOffset    actual tail offset
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

func initialRecoveryFailureVITConfig(control *initialRecoveryReadFailure) it.VITConfig {
	appQName := istructs.AppQName_test1_app1
	return it.NewOwnVITConfig(
		it.WithApp(appQName, provideRecoveryTestAppWithPartitions(3),
			it.WithUserLogin(recoveryTestLoginName, recoveryTestLoginPassword),
		),
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
				}, nil
			}
		}),
	)
}

func runInitialRecoveryFailureScenario(t *testing.T, failedPartitions []istructs.PartitionID,
	targetPartition istructs.PartitionID) {
	t.Helper()
	appQName := istructs.AppQName_test1_app1
	appID := istructs.ClusterApps[appQName]
	control := newInitialRecoveryReadFailure()
	cfg := initialRecoveryFailureVITConfig(control)
	logCap := logger.StartCapture(t, logger.LogLevelVerbose)
	var targetWorkspace *it.AppWorkspace

	it.TestRestartPreservingStorageWithHooks(t, &cfg, it.RestartPreservingStorageHooks{
		FirstRun: func(t *testing.T, vit *it.VIT) {
			owner := vit.GetPrincipal(appQName, recoveryTestLoginName)
			targetWorkspace = createWorkspacesInPartition(t, vit, owner, targetPartition, 1)[0]
			vit.PostWS(targetWorkspace, nonIDBearingCommand, "{}")
		},
		AfterFirstStop: func(t *testing.T, storage it.RestartStorage) {
			checkpointAppStorage := storage.AppStorage(t, istructs.AppQName_sys_vvm)
			checkpointStorage := vvmstorage.NewRecoveryCheckpointStorage(checkpointAppStorage)
			for partitionID := istructs.PartitionID(0); partitionID < 3; partitionID++ {
				removePartitionCheckpoint(t, checkpointStorage, checkpointAppStorage, appID, partitionID)
				_, ok, err := checkpointStorage.GetPartitionCheckpoint(appID, partitionID)
				require.NoError(t, err)
				require.False(t, ok)
			}
			control.planInitialRecoveryFailure(failedPartitions)
			logCap.Reset()
		},
		SecondRun: func(t *testing.T, vit *it.VIT) {
			checkpointAppStorage, err := vit.IAppStorageProvider.AppStorage(istructs.AppQName_sys_vvm)
			require.NoError(t, err)
			checkpointStorage := vvmstorage.NewRecoveryCheckpointStorage(checkpointAppStorage)

			for partitionID := istructs.PartitionID(0); partitionID < 3; partitionID++ {
				_, ok, err := checkpointStorage.GetPartitionCheckpoint(appID, partitionID)
				require.NoError(t, err)
				require.Equal(t, !containsPartition(failedPartitions, partitionID), ok)
			}
			for _, partitionID := range failedPartitions {
				logCap.HasLine("stage=bootstrap.initial_recovery.error", "read PLog: PLog read failed",
					fmt.Sprintf("partid=%d", partitionID))
			}
			require.NotNil(t, vit.VoedgerVM)

			control.failRuntimeRecovery(targetPartition)
			response := vit.PostWS(targetWorkspace, "c.sys.CUD", cudBody1Doc,
				httpu.WithNoRetryPolicy(), httpu.Expect503())
			require.Equal(t, http.StatusServiceUnavailable, response.HTTPResp.StatusCode)

			logCap.EventuallyHasLine("stage=cp.partition_recovery.readplog.error", "PLog read failed")
			response = vit.PostWS(targetWorkspace, "c.sys.CUD", cudBody1Doc,
				httpu.WithNoRetryPolicy(), httpu.Expect500())
			require.Equal(t, http.StatusInternalServerError, response.HTTPResp.StatusCode)
			var sysError coreutils.SysError
			require.ErrorAs(t, response.SysError, &sysError)
			require.Equal(t, fmt.Sprintf("partition %d: recovery failed: PLog read failed", targetPartition),
				sysError.Message)
			control.requireRetryStarted(t)
			require.Equal(t, 2, control.runtimeReadCount())

			response = vit.PostWS(targetWorkspace, "c.sys.CUD", cudBody1Doc,
				httpu.WithNoRetryPolicy(), httpu.Expect503())
			require.Equal(t, http.StatusServiceUnavailable, response.HTTPResp.StatusCode)
			control.finishRetry()
		},
	})
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
	control *initialRecoveryReadFailure
}

func (f *initialRecoveryFailureFactory) AppStorage(appName istorage.SafeAppName) (istorage.IAppStorage, error) {
	storage, err := f.IAppStorageFactory.AppStorage(appName)
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

// initialRecoveryReadFailure injects PLog read failures across two recovery phases:
//
//	planInitialRecoveryFailure(partitions)
//	                 |
//	                 v
//	second VVM startup: InitialRecovery -> Read(PLog)
//	                                   | selected partition -> error
//	                                   | other partition    -> real storage
//	                                   v
//	                         VVM starts and becomes ready
//	                                   |
//	                 failRuntimeRecovery(target)
//	                                   |
//	                                   v
//	command processor recovery: target Read #1 -> error
//	                            target Read #2 -> signal retryStarted
//	                                                   |
//	                                                   v
//	                                             wait for retryRelease
//	                                                   ^
//	                                                   |
//	                          test assertions -> finishRetry
//
// Reads for unrelated PLog keys and callers continue to the underlying storage.
type initialRecoveryReadFailure struct {
	mu                     sync.Mutex
	initialRecoveryRunning bool
	initialPartitions      map[string]struct{}
	runtimePartition       string
	runtimeReads           int
	retryStarted           chan struct{}
	retryRelease           chan struct{}
	retryStartedOnce       sync.Once
	retryReleaseOnce       sync.Once
}

func newInitialRecoveryReadFailure() *initialRecoveryReadFailure {
	return &initialRecoveryReadFailure{
		initialPartitions: map[string]struct{}{},
		retryStarted:      make(chan struct{}),
		retryRelease:      make(chan struct{}),
	}
}

func (c *initialRecoveryReadFailure) planInitialRecoveryFailure(partitionIDs []istructs.PartitionID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.initialRecoveryRunning = true
	for _, partitionID := range partitionIDs {
		pKey, _ := istructsmem.PLogKey(partitionID, istructs.FirstOffset)
		c.initialPartitions[string(pKey)] = struct{}{}
	}
}

func (c *initialRecoveryReadFailure) failRuntimeRecovery(partitionID istructs.PartitionID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	pKey, _ := istructsmem.PLogKey(partitionID, istructs.FirstOffset)
	c.initialRecoveryRunning = false
	c.runtimePartition = string(pKey)
	c.runtimeReads = 0
}

func (c *initialRecoveryReadFailure) readError(pKey []byte) error {
	c.mu.Lock()
	if c.initialRecoveryRunning {
		_, fail := c.initialPartitions[string(pKey)]
		c.mu.Unlock()
		if fail {
			return errors.New("PLog read failed")
		}
		return nil
	}
	if string(pKey) != c.runtimePartition || !calledByCommandProcessor() {
		c.mu.Unlock()
		return nil
	}
	c.runtimeReads++
	readNumber := c.runtimeReads
	c.mu.Unlock()
	if readNumber == 2 {
		c.retryStartedOnce.Do(func() { close(c.retryStarted) })
		<-c.retryRelease
	}
	return errors.New("PLog read failed")
}

func calledByCommandProcessor() bool {
	pcs := make([]uintptr, 32)
	count := runtime.Callers(2, pcs)
	frames := runtime.CallersFrames(pcs[:count])
	for {
		frame, more := frames.Next()
		if strings.Contains(frame.Function, "/pkg/processors/command.") {
			return true
		}
		if !more {
			return false
		}
	}
}

func (c *initialRecoveryReadFailure) requireRetryStarted(t *testing.T) {
	t.Helper()
	select {
	case <-c.retryStarted:
	case <-time.After(time.Second):
		t.Fatal("partition recovery retry did not start")
	}
}

func (c *initialRecoveryReadFailure) runtimeReadCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.runtimeReads
}

func (c *initialRecoveryReadFailure) finishRetry() {
	c.retryReleaseOnce.Do(func() { close(c.retryRelease) })
}

func createWorkspacesInPartition(t *testing.T, vit *it.VIT, owner *it.Principal,
	partitionID istructs.PartitionID, count int) []*it.AppWorkspace {
	t.Helper()
	result := make([]*it.AppWorkspace, 0, count)
	for candidate := 0; len(result) < count; candidate++ {
		workspace := vit.CreateWorkspace(it.SimpleWSParams(fmt.Sprintf("initial-recovery-%d", candidate)), owner)
		actualPartitionID, err := vit.IAppPartitions.AppWorkspacePartitionID(workspace.AppQName(), workspace.WSID)
		require.NoError(t, err)
		if actualPartitionID == partitionID {
			result = append(result, workspace)
		}
	}
	return result
}

func fillPartitionWithNonIDBearingEvents(t *testing.T, vit *it.VIT, workspace *it.AppWorkspace,
	tail istructs.Offset) {
	t.Helper()
	partitionID, err := vit.IAppPartitions.AppWorkspacePartitionID(workspace.AppQName(), workspace.WSID)
	require.NoError(t, err)
	current := getLastPLogOffset(t, vit, workspace.AppQName(), partitionID)
	require.LessOrEqual(t, current, tail)
	for current < tail {
		response := vit.PostWS(workspace, nonIDBearingCommand, "{}")
		require.Empty(t, response.NewIDs)
		current++
	}
}

func deletePLogEvents(t *testing.T, storage istorage.IAppStorage, partitionID istructs.PartitionID,
	tail istructs.Offset) {
	t.Helper()
	for offset := istructs.FirstOffset; offset <= tail; offset++ {
		pKey, cCols := istructsmem.PLogKey(partitionID, offset)
		var value []byte
		ok, err := storage.Get(pKey, cCols, &value)
		require.NoError(t, err)
		if !ok {
			continue
		}
		ok, err = storage.CompareAndDelete(pKey, cCols, value)
		require.NoError(t, err)
		require.True(t, ok)
	}
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

func requirePartitionCheckpoint(t *testing.T, storage checkpoints.IRecoveryCheckpointStorage,
	appID istructs.ClusterAppID, partitionID istructs.PartitionID, expected istructs.Offset) {
	t.Helper()
	checkpoint, ok, err := storage.GetPartitionCheckpoint(appID, partitionID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, expected, checkpoint.LastPLogOffset)
}

func requireWorkspaceCheckpointOffset(t *testing.T, storage checkpoints.IRecoveryCheckpointStorage,
	appID istructs.ClusterAppID, wsid istructs.WSID, expected istructs.Offset) {
	t.Helper()
	checkpoint, ok, err := storage.GetWorkspaceCheckpoint(appID, wsid)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, expected, checkpoint.WLogIDBearingOffset)
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
