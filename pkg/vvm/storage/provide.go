/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Alisher Nurmanov
 */
package storage

import (
	"github.com/voedger/voedger/pkg/ielections"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/sys/checkpoints"
)

// [~server.design.orch/NewElectionsTTLStorage~impl]
func NewElectionsTTLStorage(sysVVMStorage ISysVvmStorage) ielections.ITTLStorage[TTLStorageImplKey, string] {
	return &implIElectionsTTLStorage{
		sysVVMStorage: sysVVMStorage,
	}
}

func NewAppTTLStorage(sysVVMStorage ISysVvmStorage, clusterAppID istructs.ClusterAppID) istructs.IAppTTLStorage {
	return &implAppTTLStorage{
		sysVVMStorage: sysVVMStorage,
		clusterAppID:  clusterAppID,
	}
}

func NewRecoveryCheckpointStorage(sysVVMStorage ISysVvmStorage) checkpoints.IRecoveryCheckpointStorage {
	return &implRecoveryCheckpointStorage{
		sysVVMStorage: sysVVMStorage,
	}
}
