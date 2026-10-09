/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package sys_it

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/extensionpoints"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/istructsmem"
	it "github.com/voedger/voedger/pkg/vit"
	sys_test_template "github.com/voedger/voedger/pkg/vit/testdata"
	builtinapps "github.com/voedger/voedger/pkg/vvm/builtin"
	vvmstorage "github.com/voedger/voedger/pkg/vvm/storage"
)

const docWithNested = `{"cuds": [
	{"fields":{"sys.ID": 1,"sys.QName": "app1pkg.Root", "FldRoot": 2}},
	{"fields":{"sys.ID": 2,"sys.QName": "app1pkg.Nested", "sys.ParentID":1,"sys.Container": "Nested","FldNested":3}},
	{"fields":{"sys.ID": 3,"sys.QName": "app1pkg.Third", "Fld1": 42,"sys.ParentID":2,"sys.Container": "Third"}}
]}`

const singleDoc = `{"cuds":[
	{"fields":{"sys.ID":1,"sys.QName":"app1pkg.Root","FldRoot":3}}
]}`

const singletonCUD = `{"cuds":[
	{"fields":{"sys.ID":1,"sys.QName":"app1pkg.Config","Fld1":"singleton tail"}}
]}`

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
			checkNextWorkspaceSequence(t, vit, workspace1, ws1)
			checkNextWorkspaceSequence(t, vit, workspace2, ws2)
		},
	})
}

func provideRecoveryTestApp(apis builtinapps.APIs, cfg *istructsmem.AppConfigType,
	ep extensionpoints.IExtensionPoint) builtinapps.Def {
	def := it.ProvideApp1(apis, cfg, ep)
	def.NumParts = 1
	return def
}

func checkNextWorkspaceSequence(t *testing.T, vit *it.VIT, workspace *it.AppWorkspace,
	expected expectedWorkspaceSequence) {
	t.Helper()
	require := require.New(t)
	response := vit.PostWS(workspace, "c.sys.CUD", singleDoc)
	require.Len(response.NewIDs, 1)
	require.Equal(expected.nextWLogOffset, response.CurrentWLogOffset)
	require.Equal(expected.nextRecordID, response.NewIDs["1"])
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
