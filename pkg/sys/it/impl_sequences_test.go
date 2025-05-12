/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package sys_it

import (
	"log"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/coreutils"
	"github.com/voedger/voedger/pkg/isequencer"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/istructsmem"
	it "github.com/voedger/voedger/pkg/vit"
	"github.com/voedger/voedger/pkg/vvm"
)

// [~server.design.sequences/it.SequencesTrustLevel0~impl]
func TestSequencesTrustLevel_0(t *testing.T) {
	require := require.New(t)
	cfg := it.NewOwnVITConfig(
		it.WithApp(istructs.AppQName_test1_app1, it.ProvideApp1,
			it.WithUserLogin("login", "pwd"),
			it.WithChildWorkspace(it.QNameApp1_TestWSKind, "test_ws1", "", "", "login", map[string]interface{}{"IntFld": 42}),
			it.WithChildWorkspace(it.QNameApp1_TestWSKind, "test_ws2", "", "", "login", map[string]interface{}{"IntFld": 42}),
			it.WithChildWorkspace(it.QNameApp1_TestWSKind, "test_ws3", "", "", "login", map[string]interface{}{"IntFld": 42}),
		), it.WithVVMConfig(func(cfg *vvm.VVMConfig) {
			// is default in tests, but for sure
			cfg.SequencesTrustLevel = isequencer.SequencesTrustLevel_0
		}))
	vit := it.NewVIT(t, &cfg)
	defer vit.TearDown()

	as, err := vit.IAppStorageProvider.AppStorage(istructs.AppQName_test1_app1)
	require.NoError(err)

	body := `{"cuds":[{"fields":{"sys.ID":1,"sys.QName":"app1pkg.category","name":"Awesome food"}}]}`

	t.Run("record - protected against overwrite", func(t *testing.T) {
		ws := vit.WS(istructs.AppQName_test1_app1, "test_ws1")
		resp := vit.PostWS(ws, "c.sys.CUD", body)
		newID := resp.NewID()

		// Corrupt the storage: Insert a conflicting key that will be used on creating the next record
		pkey, ccols := istructsmem.RecordKey(ws.WSID, newID+1)
		require.NoError(as.Put(pkey, ccols, []byte{1}))

		// try to insert one more record
		vit.PostWS(ws, "c.sys.CUD", body, coreutils.Expect500("ApplyRecords: sequences violation"))
	})

	t.Run("wlog event - protected against overwrite", func(t *testing.T) {
		ws := vit.WS(istructs.AppQName_test1_app1, "test_ws2")
		resp := vit.PostWS(ws, "c.sys.CUD", body)
		wlogOffset := resp.CurrentWLogOffset

		// Corrupt the storage: Insert a conflicting key that will be used on creating the next wlog event
		pkey, cols := istructsmem.WLogKey(ws.WSID, wlogOffset+1)
		require.NoError(as.Put(pkey, cols, []byte{1}))

		// try to insert one more event
		vit.PostWS(ws, "c.sys.CUD", body, coreutils.Expect500("PutWLog: sequences violation"))
	})

	t.Run("plog event - protected against overwrite", func(t *testing.T) {
		ws := vit.WS(istructs.AppQName_test1_app1, "test_ws3")
		resp := vit.PostWS(ws, "c.sys.CUD", body)

		plogOffset := findPLogOffsetByWLogOffset(t, vit, ws, resp.CurrentWLogOffset)

		// Corrupt the storage: Insert a conflicting key that will be used on creating the next plog event
		partitionID, err := vit.IAppPartitions.AppWorkspacePartitionID(istructs.AppQName_test1_app1, ws.WSID)
		require.NoError(err)
		pkey, cols := istructsmem.PLogKey(partitionID, plogOffset+1)
		require.NoError(as.Put(pkey, cols, []byte{1}))

		// try to insert one more event
		vit.PostWS(ws, "c.sys.CUD", body, coreutils.Expect500("PutPLog: sequences violation"))
	})
}

// [~server.design.sequences/it.SequencesTrustLevel1~impl]
func TestSequencesTrustLevel_1(t *testing.T) {
	require := require.New(t)
	cfg := it.NewOwnVITConfig(
		it.WithApp(istructs.AppQName_test1_app1, it.ProvideApp1,
			it.WithUserLogin("login", "pwd"),
			it.WithChildWorkspace(it.QNameApp1_TestWSKind, "test_ws1", "", "", "login", map[string]interface{}{"IntFld": 42}),
			it.WithChildWorkspace(it.QNameApp1_TestWSKind, "test_ws2", "", "", "login", map[string]interface{}{"IntFld": 42}),
			it.WithChildWorkspace(it.QNameApp1_TestWSKind, "test_ws3", "", "", "login", map[string]interface{}{"IntFld": 42}),
		), it.WithVVMConfig(func(cfg *vvm.VVMConfig) {
			cfg.SequencesTrustLevel = isequencer.SequencesTrustLevel_1
		}))
	vit := it.NewVIT(t, &cfg)
	defer vit.TearDown()

	body := `{"cuds":[{"fields":{"sys.ID":1,"sys.QName":"app1pkg.category","name":"Awesome food"}}]}`

	t.Run("record - no protection against overwrite", func(t *testing.T) {
		ws := vit.WS(istructs.AppQName_test1_app1, "test_ws1")
		resp := vit.PostWS(ws, "c.sys.CUD", body)
		newID := resp.NewID()

		// Corrupt the storage: Insert a conflicting key that will be used on creating the next record
		appStorage, err := vit.IAppStorageProvider.AppStorage(istructs.AppQName_test1_app1)
		require.NoError(err)
		pkey, ccols := istructsmem.RecordKey(ws.WSID, newID+1)
		require.NoError(appStorage.Put(pkey, ccols, []byte{1}))

		// DISASTER: RECORD OVERWRITE HAPPENS HERE
		vit.PostWS(ws, "c.sys.CUD", body).Println()

		// check the key is actually overwritten: it should contain data (was empty)
		appStorage, err = vit.IAppStorageProvider.AppStorage(istructs.AppQName_test1_app1)
		require.NoError(err)
		data := []byte{}
		ok, err := appStorage.Get(pkey, ccols, &data)
		require.NoError(err)
		require.True(ok)
		require.NotEmpty(data)
		log.Println(data)
	})

	t.Run("wlog event - protected against overwrite", func(t *testing.T) {
		ws := vit.WS(istructs.AppQName_test1_app1, "test_ws2")
		resp := vit.PostWS(ws, "c.sys.CUD", body)
		wlogOffset := resp.CurrentWLogOffset

		appStorage, err := vit.IAppStorageProvider.AppStorage(istructs.AppQName_test1_app1)
		require.NoError(err)

		// Corrupt the storage: Insert a conflicting key that will be used on creating the next wlog event
		pkey, cols := istructsmem.WLogKey(ws.WSID, wlogOffset+1)
		require.NoError(appStorage.Put(pkey, cols, []byte{1}))

		// try to insert one more event
		vit.PostWS(ws, "c.sys.CUD", body, coreutils.Expect500("PutWLog: sequences violation"))
	})

	t.Run("plog event - protected against overwrite", func(t *testing.T) {
		ws := vit.WS(istructs.AppQName_test1_app1, "test_ws3")
		resp := vit.PostWS(ws, "c.sys.CUD", body)

		appStorage, err := vit.IAppStorageProvider.AppStorage(istructs.AppQName_test1_app1)
		require.NoError(err)

		plogOffset := findPLogOffsetByWLogOffset(t, vit, ws, resp.CurrentWLogOffset)

		// Corrupt the storage: Insert a conflicting key that will be used on creating the next plog event
		partitionID, err := vit.IAppPartitions.AppWorkspacePartitionID(istructs.AppQName_test1_app1, ws.WSID)
		require.NoError(err)
		pkey, cols := istructsmem.PLogKey(partitionID, plogOffset+1)
		require.NoError(appStorage.Put(pkey, cols, []byte{1}))

		// try to insert one more event
		vit.PostWS(ws, "c.sys.CUD", body, coreutils.Expect500("PutPLog: sequences violation"))
	})
}

// [~server.design.sequences/it.SequencesTrustLevel2~impl]
func TestSequencesTrustLevel_2(t *testing.T) {
	require := require.New(t)
	cfg := it.NewOwnVITConfig(
		it.WithApp(istructs.AppQName_test1_app1, it.ProvideApp1,
			it.WithUserLogin("login", "pwd"),
			it.WithChildWorkspace(it.QNameApp1_TestWSKind, "test_ws1", "", "", "login", map[string]interface{}{"IntFld": 42}),
			it.WithChildWorkspace(it.QNameApp1_TestWSKind, "test_ws2", "", "", "login", map[string]interface{}{"IntFld": 42}),
			it.WithChildWorkspace(it.QNameApp1_TestWSKind, "test_ws3", "", "", "login", map[string]interface{}{"IntFld": 42}),
		), it.WithVVMConfig(func(cfg *vvm.VVMConfig) {
			cfg.SequencesTrustLevel = isequencer.SequencesTrustLevel_2
		}))
	vit := it.NewVIT(t, &cfg)
	defer vit.TearDown()

	body := `{"cuds":[{"fields":{"sys.ID":1,"sys.QName":"app1pkg.category","name":"Awesome food"}}]}`

	t.Run("record - no protection against overwrite", func(t *testing.T) {
		ws := vit.WS(istructs.AppQName_test1_app1, "test_ws1")
		resp := vit.PostWS(ws, "c.sys.CUD", body)
		newID := resp.NewID()

		// Corrupt the storage: Insert a conflicting key that will be used on creating the next record
		appStorage, err := vit.IAppStorageProvider.AppStorage(istructs.AppQName_test1_app1)
		require.NoError(err)
		pkey, ccols := istructsmem.RecordKey(ws.WSID, newID+1)
		require.NoError(appStorage.Put(pkey, ccols, []byte{1}))

		// DISASTER: RECORD IS OVERWRITTEN HERE
		vit.PostWS(ws, "c.sys.CUD", body)

		// check the key is actually overwritten: it should contain data (was empty)
		appStorage, err = vit.IAppStorageProvider.AppStorage(istructs.AppQName_test1_app1)
		require.NoError(err)
		data := []byte{}
		ok, err := appStorage.Get(pkey, ccols, &data)
		require.NoError(err)
		require.True(ok)
		require.NotEmpty(data)
		log.Println(data)
	})

	t.Run("wlog event - no protection against overwrite", func(t *testing.T) {
		ws := vit.WS(istructs.AppQName_test1_app1, "test_ws2")
		resp := vit.PostWS(ws, "c.sys.CUD", body)
		wlogOffset := resp.CurrentWLogOffset

		appStorage, err := vit.IAppStorageProvider.AppStorage(istructs.AppQName_test1_app1)
		require.NoError(err)

		// Corrupt the storage: Insert a conflicting key that will be used on creating the next wlog event
		pkey, ccols := istructsmem.WLogKey(ws.WSID, wlogOffset+1)
		require.NoError(appStorage.Put(pkey, ccols, []byte{1}))

		// DISASTER: WLOG EVENT IS OVERWRITTEN HERE
		vit.PostWS(ws, "c.sys.CUD", body)

		// check the wlog event is actually overwritten
		appStorage, err = vit.IAppStorageProvider.AppStorage(istructs.AppQName_test1_app1)
		require.NoError(err)
		data := []byte{}
		ok, err := appStorage.Get(pkey, ccols, &data)
		require.NoError(err)
		require.True(ok)
		require.NotEmpty(data)
		log.Println(data)
	})

	t.Run("plog event - no protection against overwrite", func(t *testing.T) {
		ws := vit.WS(istructs.AppQName_test1_app1, "test_ws3")
		resp := vit.PostWS(ws, "c.sys.CUD", body)

		appStorage, err := vit.IAppStorageProvider.AppStorage(istructs.AppQName_test1_app1)
		require.NoError(err)

		plogOffset := findPLogOffsetByWLogOffset(t, vit, ws, resp.CurrentWLogOffset)

		// Corrupt the storage: Insert a conflicting key that will be used on creating the next plog event
		partitionID, err := vit.IAppPartitions.AppWorkspacePartitionID(istructs.AppQName_test1_app1, ws.WSID)
		require.NoError(err)
		pkey, ccols := istructsmem.PLogKey(partitionID, plogOffset+1)
		require.NoError(appStorage.Put(pkey, ccols, []byte{1}))

		// DISASTER: PLOG EVENT IS OVERWRITTEN HERE
		vit.PostWS(ws, "c.sys.CUD", body)

		// check the plog event is actually overwritten
		appStorage, err = vit.IAppStorageProvider.AppStorage(istructs.AppQName_test1_app1)
		require.NoError(err)
		data := []byte{}
		ok, err := appStorage.Get(pkey, ccols, &data)
		require.NoError(err)
		require.True(ok)
		require.NotEmpty(data)
		log.Println(data)
	})
}
