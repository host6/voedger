/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package sys_it

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/coreutils"
	"github.com/voedger/voedger/pkg/istructs"
	syscheckpoints "github.com/voedger/voedger/pkg/sys/checkpoints"
	it "github.com/voedger/voedger/pkg/vit"
	sys_test_template "github.com/voedger/voedger/pkg/vit/testdata"
	vvmstorage "github.com/voedger/voedger/pkg/vvm/storage"
)

func TestCorrectIDsIssueAfterRecovery(t *testing.T) {
	cfg := it.NewOwnVITConfig(
		it.WithApp(istructs.AppQName_test1_app1, it.ProvideApp1,
			it.WithWorkspaceTemplate(it.QNameApp1_TestWSKind, "test_template", sys_test_template.TestTemplateFS),
			it.WithUserLogin("login", "pwd"),
			it.WithChildWorkspace(it.QNameApp1_TestWSKind, "test_ws", "test_template", "", "login", map[string]interface{}{"IntFld": 42}),
		),
	)
	const cudBody = `{"cuds": [
		{"fields":{"sys.ID": 1,"sys.QName": "app1pkg.Root", "FldRoot": 2}},
		{"fields":{"sys.ID": 2,"sys.QName": "app1pkg.Nested", "sys.ParentID":1,"sys.Container": "Nested","FldNested":3}},
		{"fields":{"sys.ID": 3,"sys.QName": "app1pkg.Third", "Fld1": 42,"sys.ParentID":2,"sys.Container": "Third"}}
	]}`

	var (
		wsid                istructs.WSID
		partitionID         istructs.PartitionID
		firstVVMWLogOffset  istructs.Offset
		firstVVMMaxRecordID istructs.RecordID
		secondVVMWLogOffset istructs.Offset
		partitionBefore     syscheckpoints.PartitionCheckpoint
		workspaceBefore     syscheckpoints.WorkspaceCheckpoint
	)
	appID := istructs.ClusterApps[istructs.AppQName_test1_app1]

	it.TestRestartPreservingStorageWithHooks(t, &cfg, it.RestartPreservingStorageHooks{
		FirstRun: func(t *testing.T, vit *it.VIT) {
			require := require.New(t)
			ws := vit.WS(istructs.AppQName_test1_app1, "test_ws")
			wsid = ws.WSID
			partitionID = coreutils.AppPartitionID(wsid, istructs.NumAppPartitions(vit.NumCommandProcessors))

			resp := vit.PostWS(ws, "c.sys.CUD", cudBody)
			require.Len(resp.NewIDs, 3)

			body := `{"args":{"sys.ID": 1,"orecord1":[{"sys.ID":2,"sys.ParentID":1,"orecord2":[{"sys.ID":3,"sys.ParentID":2}]}]},"unloggedArgs":{"sys.ID":4}}`
			resp = vit.PostWS(ws, "c.app1pkg.CmdODocOne", body)
			require.NotEmpty(resp.NewIDs)
			firstVVMWLogOffset = resp.CurrentWLogOffset
			for _, id := range resp.NewIDs {
				firstVVMMaxRecordID = max(firstVVMMaxRecordID, id)
			}
		},
		AfterFirstStop: func(t *testing.T, storage it.RestartStorage) {
			require := require.New(t)
			checkpointStorage := vvmstorage.NewRecoveryCheckpointStorage(
				storage.AppStorage(t, istructs.AppQName_sys_vvm))

			var ok bool
			var err error
			partitionBefore, ok, err = checkpointStorage.GetPartitionCheckpoint(appID, partitionID)
			require.NoError(err)
			require.True(ok)
			workspaceBefore, ok, err = checkpointStorage.GetWorkspaceCheckpoint(appID, wsid)
			require.NoError(err)
			require.True(ok)
			require.Equal(firstVVMWLogOffset, workspaceBefore.LastHandledWLogOffset)
		},
		SecondRun: func(t *testing.T, vit *it.VIT) {
			require := require.New(t)
			ws := vit.WS(istructs.AppQName_test1_app1, "test_ws")
			resp := vit.PostWS(ws, "c.sys.CUD", cudBody)
			require.Greater(resp.CurrentWLogOffset, firstVVMWLogOffset)
			require.Greater(resp.NewIDs["1"], firstVVMMaxRecordID)
			require.Equal(resp.NewIDs["1"]+1, resp.NewIDs["2"])
			require.Equal(resp.NewIDs["1"]+2, resp.NewIDs["3"])
			secondVVMWLogOffset = resp.CurrentWLogOffset
		},
		AfterSecondStop: func(t *testing.T, storage it.RestartStorage) {
			require := require.New(t)
			checkpointStorage := vvmstorage.NewRecoveryCheckpointStorage(
				storage.AppStorage(t, istructs.AppQName_sys_vvm))
			partitionAfterHandoff, ok, err := checkpointStorage.GetPartitionCheckpoint(appID, partitionID)
			require.NoError(err)
			require.True(ok)
			require.Greater(partitionAfterHandoff.LastHandledPLogOffset, partitionBefore.LastHandledPLogOffset)
			workspaceAfterHandoff, ok, err := checkpointStorage.GetWorkspaceCheckpoint(appID, wsid)
			require.NoError(err)
			require.True(ok)
			require.Equal(secondVVMWLogOffset, workspaceAfterHandoff.LastHandledWLogOffset)
		},
	})
}
