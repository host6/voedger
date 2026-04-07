/*
 * Copyright (c) 2024-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package appparts

import (
	"context"

	"github.com/voedger/voedger/pkg/goutils/testingu"
	"github.com/voedger/voedger/pkg/iratesce"
	"github.com/voedger/voedger/pkg/istorage"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/vvm/storage"
)

func NewTestAppParts(asp istructs.IAppStructsProvider, appStorageProvider istorage.IAppStorageProvider) (IAppPartitions, func()) {
	vvmCtx, cancel := context.WithCancel(context.Background())
	appParts, cleanup := New2(
		vvmCtx,
		asp,
		NullSyncActualizerFactory,
		NullActualizerRunner,
		NullSchedulerRunner,
		NullExtensionEngineFactories,
		iratesce.TestBucketsFactory,
		testingu.MockTime,
		storage.NewTestIVVMSeqStorageAdpater(appStorageProvider),
	)
	combinedCleanup := func() {
		cancel()
		cleanup()
	}
	return appParts, combinedCleanup
}
