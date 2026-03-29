/*
 * Copyright (c) 2022-present unTill Pro, Ltd.
 */

package stateprovide

import (
	"context"

	"github.com/voedger/voedger/pkg/coreutils/federation"
	"github.com/voedger/voedger/pkg/goutils/httpu"
	"github.com/voedger/voedger/pkg/isecrets"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/itokens"
	"github.com/voedger/voedger/pkg/state"
	"github.com/voedger/voedger/pkg/sys"
	"github.com/voedger/voedger/pkg/sys/storages"
)

type queryProcessorState struct {
	*hostState
	queryArgs          state.PrepareArgsFunc
	queryCallback      state.ExecQueryCallbackFunc
	resultValueBuilder istructs.IStateValueBuilder
}

func (s queryProcessorState) QueryPrepareArgs() istructs.PrepareArgs {
	return s.queryArgs()
}

func (s queryProcessorState) QueryCallback() istructs.ExecQueryCallback {
	return s.queryCallback()
}

func (s *queryProcessorState) sendPrevQueryObject() error {
	if s.resultValueBuilder != nil {
		obj := s.resultValueBuilder.BuildValue().(*storages.ObjectStateValue).AsObject()
		s.resultValueBuilder = nil
		return s.queryCallback()(obj)
	}
	return nil
}

func (s *queryProcessorState) NewValue(key istructs.IStateKeyBuilder) (eb istructs.IStateValueBuilder, err error) {
	if key.Storage() == sys.Storage_Result {
		err = s.sendPrevQueryObject()
		if err != nil {
			return nil, err
		}
		eb, err = s.hostState.withInsert[sys.Storage_Result].ProvideValueBuilder(key, nil)
		if err != nil {
			return nil, err
		}
		s.resultValueBuilder = eb
		return eb, nil
	}
	return s.hostState.NewValue(key)
}

func (s *queryProcessorState) FindIntent(key istructs.IStateKeyBuilder) istructs.IStateValueBuilder {
	if key.Storage() == sys.Storage_Result {
		return s.resultValueBuilder
	}
	return s.hostState.FindIntent(key)
}

func (s *queryProcessorState) ApplyIntents() (err error) {
	err = s.sendPrevQueryObject()
	if err != nil {
		return err
	}
	return s.hostState.ApplyIntents()
}

func implProvideQueryProcessorState(
	ctx context.Context,
	params state.IQueryProcessorStateParams,
	secretReader isecrets.ISecretReader,
	itokens itokens.ITokens,
	federation federation.IFederation,
	stateOpts state.StateOpts,
	httpClient httpu.IHTTPClient) state.IHostState {

	s := &queryProcessorState{
		hostState:     newHostState(ctx, "QueryProcessor", queryProcessorStateMaxIntents, params.AppStructs),
		queryArgs:     params.PrepareArgs,
		queryCallback: params.QueryCallback,
	}

	ieventsFunc := func() istructs.IEvents { return params.AppStructs().Events() }

	s.addStorage(sys.Storage_View, storages.NewViewRecordsStorage(ctx, params.AppStructs, params.WSID, nil), S_GET|S_GET_BATCH|S_READ)
	s.addStorage(sys.Storage_Record, storages.NewRecordsStorage(params.AppStructs, params.WSID, nil), S_GET|S_GET_BATCH)
	s.addStorage(sys.Storage_WLog, storages.NewWLogStorage(ctx, ieventsFunc, params.WSID), S_GET|S_READ)
	s.addStorage(sys.Storage_HTTP, storages.NewHTTPStorage(httpClient), S_READ)
	s.addStorage(sys.Storage_FederationCommand, storages.NewFederationCommandStorage(params.AppStructs, params.WSID, federation, itokens, stateOpts.FederationCommandHandler), S_GET)
	s.addStorage(sys.Storage_FederationBlob, storages.NewFederationBlobStorage(params.AppStructs, params.WSID, federation, itokens, stateOpts.FederationBlobHandler), S_READ)
	s.addStorage(sys.Storage_AppSecret, storages.NewAppSecretsStorage(secretReader), S_GET)
	s.addStorage(sys.Storage_RequestSubject, storages.NewSubjectStorage(params.Principals, params.Token), S_GET)
	s.addStorage(sys.Storage_QueryContext, storages.NewQueryContextStorage(params.Arg, params.WSID), S_GET)
	s.addStorage(sys.Storage_Response, storages.NewResponseStorage(), S_INSERT)
	s.addStorage(sys.Storage_Result, storages.NewResultStorage(params.ResultBuilder), S_INSERT)
	s.addStorage(sys.Storage_Uniq, storages.NewUniquesStorage(params.AppStructs, params.WSID, stateOpts.UniquesHandler), S_GET)
	s.addStorage(sys.Storage_Logger, storages.NewLoggerStorage(), S_INSERT)

	return s
}
