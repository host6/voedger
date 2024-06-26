/*
 * Copyright (c) 2021-present Sigma-Soft, Ltd.
 * @author: Nikolay Nikitin
 */

package appparts

import (
	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/iextengine"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/pipeline"
)

type SyncActualizerFactory = func(istructs.IAppStructs, istructs.PartitionID) pipeline.ISyncOperator

// New only for tests where sync actualizer is not used
func New(structs istructs.IAppStructsProvider) (ap IAppPartitions, cleanup func(), err error) {
	return New2(
		structs,
		func(istructs.IAppStructs, istructs.PartitionID) pipeline.ISyncOperator {
			return &pipeline.NOOP{}
		},
		&nullActualizers{},
		iextengine.ExtensionEngineFactories{
			appdef.ExtensionEngineKind_BuiltIn: iextengine.NullExtensionEngineFactory,
			appdef.ExtensionEngineKind_WASM:    iextengine.NullExtensionEngineFactory,
		},
	)
}

// New2 creates new app partitions.
//
// # Parameters:
//
//	structs - application structures provider
//	syncAct - sync actualizer factory, old actualizers style, should be used with builtin applications only
//	act - actualizers
//	eef - extension engine factories
func New2(
	structs istructs.IAppStructsProvider,
	syncAct SyncActualizerFactory,
	act IActualizers,
	eef iextengine.ExtensionEngineFactories,
) (ap IAppPartitions, cleanup func(), err error) {
	return newAppPartitions(structs, syncAct, act, eef)
}
