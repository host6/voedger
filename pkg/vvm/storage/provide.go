/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Alisher Nurmanov
 */
package storage

import (
	"github.com/voedger/voedger/pkg/ielections"
	"github.com/voedger/voedger/pkg/isequencer"
	"github.com/voedger/voedger/pkg/istorage"
	"github.com/voedger/voedger/pkg/istructs"
)

// [~server.design.orch/NewElectionsTTLStorage~impl]
func NewElectionsTTLStorage(sysVVMStorage ISysVvmStorage) ielections.ITTLStorage[TTLStorageImplKey, string] {
	return &implIElectionsTTLStorage{
		sysVVMStorage: sysVVMStorage,
	}
}

func NewVVMSeqStorageAdapter(sysVVMStorage ISysVvmStorage) isequencer.IVVMSeqStorageAdapter {
	return &implVVMSeqStorageAdapter{
		sysVVMStorage: sysVVMStorage,
	}
}

func NewAppTTLStorage(sysVVMStorage ISysVvmStorage, clusterAppID istructs.ClusterAppID) istructs.IAppTTLStorage {
	return &implAppTTLStorage{
		sysVVMStorage: sysVVMStorage,
		clusterAppID:  clusterAppID,
	}
}

// can not be wired directly by VVM because we have few storage providers: cached, uncached etc
func NewTestIVVMSeqStorageAdpater(asp istorage.IAppStorageProvider) isequencer.IVVMSeqStorageAdapter {
	sysVVMStorage, err := asp.AppStorage(istructs.AppQName_sys_vvm)
	if err != nil {
		panic(err)
	}
	return NewVVMSeqStorageAdapter(sysVVMStorage)
}
