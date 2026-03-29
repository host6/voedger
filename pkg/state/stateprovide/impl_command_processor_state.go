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

type commandProcessorState struct {
	*hostState
	commandPrepareArgs state.CommandPrepareArgsFunc
}

func (s commandProcessorState) CommandPrepareArgs() istructs.CommandPrepareArgs {
	return s.commandPrepareArgs()
}

func implProvideCommandProcessorState(
	ctx context.Context,
	params state.ICommandProcessorStateParams,
	secretReader isecrets.ISecretReader,
	intentsLimit int,
	stateOpts state.StateOpts) state.IHostState {

	s := &commandProcessorState{
		hostState:          newHostState(ctx, "CommandProcessor", intentsLimit, params.AppStructs),
		commandPrepareArgs: params.CommandPrepareArgs,
	}

	ieventsFunc := func() istructs.IEvents { return params.AppStructs().Events() }

	s.addStorage(sys.Storage_View, storages.NewViewRecordsStorage(ctx, params.AppStructs, params.WSID, nil), S_GET|S_GET_BATCH)
	s.addStorage(sys.Storage_Record, storages.NewRecordsStorage(params.AppStructs, params.WSID, params.CUD), S_GET|S_GET_BATCH|S_INSERT|S_UPDATE)
	s.addStorage(sys.Storage_WLog, storages.NewWLogStorage(ctx, ieventsFunc, params.WSID), S_GET)
	s.addStorage(sys.Storage_AppSecret, storages.NewAppSecretsStorage(secretReader), S_GET)
	s.addStorage(sys.Storage_RequestSubject, storages.NewSubjectStorage(params.Principals, params.Token), S_GET)
	s.addStorage(sys.Storage_Result, storages.NewResultStorage(params.CmdResultBuilder), S_INSERT)
	s.addStorage(sys.Storage_Uniq, storages.NewUniquesStorage(params.AppStructs, params.WSID, stateOpts.UniquesHandler), S_GET)
	s.addStorage(sys.Storage_Response, storages.NewResponseStorage(), S_INSERT)
	s.addStorage(sys.Storage_CommandContext, storages.NewCommandContextStorage(params.Arg, params.UnloggedArg, params.WSID, params.WLogOffset, params.Origin), S_GET)
	s.addStorage(sys.Storage_Logger, storages.NewLoggerStorage(), S_INSERT)

	return s
}
