/*
 * Copyright (c) 2022-present unTill Pro, Ltd.
 */

package stateprovide

import (
	"context"

	"github.com/voedger/voedger/pkg/isecrets"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/state"
	"github.com/voedger/voedger/pkg/sys"
	"github.com/voedger/voedger/pkg/sys/storages"
)

type syncActualizerState struct {
	*hostState
	eventFunc state.PLogEventFunc
}

func (s *syncActualizerState) PLogEvent() istructs.IPLogEvent {
	return s.eventFunc()
}

func implProvideSyncActualizerState(ctx context.Context, params state.ISyncActualizerStateParams, n10nFunc state.N10nFunc,
	secretReader isecrets.ISecretReader, intentsLimit int, stateOpts state.StateOpts) state.IHostState {

	hs := &syncActualizerState{
		hostState: newHostState(ctx, "SyncActualizer", intentsLimit, params.AppStructs),
		eventFunc: params.PLogEvent,
	}
	ieventsFunc := func() istructs.IEvents { return params.AppStructs().Events() }
	hs.addStorage(sys.Storage_View, storages.NewViewRecordsStorage(ctx, params.AppStructs, params.WSID, n10nFunc), S_GET|S_GET_BATCH|S_INSERT|S_UPDATE)
	hs.addStorage(sys.Storage_Record, storages.NewRecordsStorage(params.AppStructs, params.WSID, nil), S_GET|S_GET_BATCH)
	hs.addStorage(sys.Storage_WLog, storages.NewWLogStorage(ctx, ieventsFunc, params.WSID), S_GET)
	hs.addStorage(sys.Storage_AppSecret, storages.NewAppSecretsStorage(secretReader), S_GET)
	hs.addStorage(sys.Storage_Uniq, storages.NewUniquesStorage(params.AppStructs, params.WSID, stateOpts.UniquesHandler), S_GET)
	hs.addStorage(sys.Storage_Logger, storages.NewLoggerStorage(), S_INSERT)
	return hs
}
