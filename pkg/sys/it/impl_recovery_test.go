/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package sys_it

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/coreutils"
	"github.com/voedger/voedger/pkg/goutils/timeu"
	"github.com/voedger/voedger/pkg/istorage"
	"github.com/voedger/voedger/pkg/istorage/provider"
	"github.com/voedger/voedger/pkg/istructs"
	commandprocessor "github.com/voedger/voedger/pkg/processors/command"
	it "github.com/voedger/voedger/pkg/vit"
	sys_test_template "github.com/voedger/voedger/pkg/vit/testdata"
	"github.com/voedger/voedger/pkg/vvm"
	vvmstorage "github.com/voedger/voedger/pkg/vvm/storage"
)

func TestCorrectIDsIssueAfterRecovery(t *testing.T) {
	require := require.New(t)
	keyspaceSuffix := provider.NewTestKeyspaceIsolationSuffix()
	var sharedStorageFactory istorage.IAppStorageFactory
	counter := 1
	cfg := it.NewOwnVITConfig(
		it.WithApp(istructs.AppQName_test1_app1, it.ProvideApp1,
			it.WithWorkspaceTemplate(it.QNameApp1_TestWSKind, "test_template", sys_test_template.TestTemplateFS),
			it.WithUserLogin("login", "pwd"),
			it.WithChildWorkspace(it.QNameApp1_TestWSKind, "test_ws", "test_template", "", "login", map[string]interface{}{"IntFld": 42}),
		),
		it.WithVVMConfig(func(cfg *vvm.VVMConfig) {
			switch counter {
			case 1:
				// 1st VVM launch
				var err error
				sharedStorageFactory, err = cfg.StorageFactory(cfg.Time)
				require.NoError(err)
				cfg.KeyspaceIsolationSuffix = keyspaceSuffix
				cfg.StorageFactory = func(timeu.ITime) (provider istorage.IAppStorageFactory, err error) {
					return sharedStorageFactory, nil
				}
			case 2:
				// 2nd VVM launch - the IDGenerator must be updated on recovery
				cfg.StorageFactory = func(timeu.ITime) (provider istorage.IAppStorageFactory, err error) {
					return sharedStorageFactory, nil
				}
				cfg.KeyspaceIsolationSuffix = keyspaceSuffix
			}
		}),
	)
	vit := it.NewVIT(t, &cfg)
	ws := vit.WS(istructs.AppQName_test1_app1, "test_ws")

	body := `{"args":{"sys.ID": 1,"orecord1":[{"sys.ID":2,"sys.ParentID":1,"orecord2":[{"sys.ID":3,"sys.ParentID":2}]}]},"unloggedArgs":{"sys.ID":4}}`
	resp := vit.PostWS(ws, "c.app1pkg.CmdODocOne", body)
	require.NotEmpty(resp.NewIDs)

	body = `{"cuds": [
		{"fields":{"sys.ID": 1,"sys.QName": "app1pkg.Root", "FldRoot": 2}},
		{"fields":{"sys.ID": 2,"sys.QName": "app1pkg.Nested", "sys.ParentID":1,"sys.Container": "Nested","FldNested":3}},
		{"fields":{"sys.ID": 3,"sys.QName": "app1pkg.Third", "Fld1": 42,"sys.ParentID":2,"sys.Container": "Third"}}
	]}`
	resp = vit.PostWS(ws, "c.sys.CUD", body)
	require.Len(resp.NewIDs, 3)
	firstVVMWLogOffset := resp.CurrentWLogOffset
	firstVVMMaxRecordID := resp.NewIDs["3"]

	vit.TearDown()

	// The first VVM bootstrapped both checkpoint levels and flushed them on
	// shutdown. Access the same sys-vvm storage outside either VVM so a stale
	// overlapping writer can be simulated deterministically.
	checkpointProvider := provider.Provide(sharedStorageFactory, keyspaceSuffix)
	sysVVMStorage, err := checkpointProvider.AppStorage(istructs.AppQName_sys_vvm)
	require.NoError(err)
	checkpoints := vvmstorage.NewRecoveryCheckpointStorage(sysVVMStorage)
	appID := istructs.ClusterApps[istructs.AppQName_test1_app1]
	partitionID := coreutils.AppPartitionID(ws.WSID, istructs.NumAppPartitions(vit.NumCommandProcessors))
	partitionBefore, ok, err := checkpoints.GetPartitionCheckpoint(appID, partitionID)
	require.NoError(err)
	require.True(ok)
	workspaceBefore, ok, err := checkpoints.GetWorkspaceCheckpoint(appID, ws.WSID)
	require.NoError(err)
	require.True(ok)
	require.Greater(workspaceBefore.NextWLogOffset, firstVVMWLogOffset)
	require.Greater(workspaceBefore.NextRecordID, firstVVMMaxRecordID)

	// A delayed writer from the first VVM must not be able to regress values
	// that are already visible in shared storage.
	require.NoError(checkpoints.PutPartitionCheckpoint(appID, partitionID,
		commandprocessor.PartitionCheckpoint{NextPLogOffset: istructs.FirstOffset}))
	require.NoError(checkpoints.PutWorkspaceCheckpoint(appID, ws.WSID,
		commandprocessor.WorkspaceCheckpoint{NextWLogOffset: istructs.FirstOffset, NextRecordID: istructs.FirstUserRecordID}))
	partitionAfterStaleWrite, ok, err := checkpoints.GetPartitionCheckpoint(appID, partitionID)
	require.NoError(err)
	require.True(ok)
	require.Equal(partitionBefore, partitionAfterStaleWrite)
	workspaceAfterStaleWrite, ok, err := checkpoints.GetWorkspaceCheckpoint(appID, ws.WSID)
	require.NoError(err)
	require.True(ok)
	require.Equal(workspaceBefore, workspaceAfterStaleWrite)

	// The second VVM recovers from the first VVM's checkpoints and continues
	// both workspace offset and record ID sequences.
	counter++
	vit = it.NewVIT(t, &cfg)
	ws = vit.WS(istructs.AppQName_test1_app1, "test_ws")
	body = `{"cuds": [
		{"fields":{"sys.ID": 1,"sys.QName": "app1pkg.Root", "FldRoot": 2}},
		{"fields":{"sys.ID": 2,"sys.QName": "app1pkg.Nested", "sys.ParentID":1,"sys.Container": "Nested","FldNested":3}},
		{"fields":{"sys.ID": 3,"sys.QName": "app1pkg.Third", "Fld1": 42,"sys.ParentID":2,"sys.Container": "Third"}}
	]}`
	resp = vit.PostWS(ws, "c.sys.CUD", body)
	require.Equal(firstVVMWLogOffset+1, resp.CurrentWLogOffset)
	require.Equal(firstVVMMaxRecordID+1, resp.NewIDs["1"])
	require.Equal(firstVVMMaxRecordID+3, resp.NewIDs["3"])
	vit.TearDown()

	partitionAfterHandoff, ok, err := checkpoints.GetPartitionCheckpoint(appID, partitionID)
	require.NoError(err)
	require.True(ok)
	require.Greater(partitionAfterHandoff.NextPLogOffset, partitionBefore.NextPLogOffset)
	workspaceAfterHandoff, ok, err := checkpoints.GetWorkspaceCheckpoint(appID, ws.WSID)
	require.NoError(err)
	require.True(ok)
	require.Equal(resp.CurrentWLogOffset+1, workspaceAfterHandoff.NextWLogOffset)
	require.Equal(resp.NewIDs["3"]+1, workspaceAfterHandoff.NextRecordID)

	// The stale first-VVM snapshots remain harmless after handoff as well.
	require.NoError(checkpoints.PutPartitionCheckpoint(appID, partitionID, partitionBefore))
	require.NoError(checkpoints.PutWorkspaceCheckpoint(appID, ws.WSID, workspaceBefore))
	actualPartition, ok, err := checkpoints.GetPartitionCheckpoint(appID, partitionID)
	require.NoError(err)
	require.True(ok)
	require.Equal(partitionAfterHandoff, actualPartition)
	actualWorkspace, ok, err := checkpoints.GetWorkspaceCheckpoint(appID, ws.WSID)
	require.NoError(err)
	require.True(ok)
	require.Equal(workspaceAfterHandoff, actualWorkspace)
}
