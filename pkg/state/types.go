/*
 * Copyright (c) 2022-present unTill Pro, Ltd.
 */

package state

import (
	"context"

	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/goutils/httpu"
	"github.com/voedger/voedger/pkg/iauthnz"
	"github.com/voedger/voedger/pkg/isecrets"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/itokens"
	"github.com/wneessen/go-mail"

	"github.com/voedger/voedger/pkg/coreutils/federation"
)

type PartitionIDFunc func() istructs.PartitionID
type WSIDFunc func() istructs.WSID
type N10nFunc func(view appdef.QName, wsid istructs.WSID, offset istructs.Offset)
type AppStructsFunc func() istructs.IAppStructs
type CUDFunc func() istructs.ICUD
type ObjectBuilderFunc func() istructs.IObjectBuilder
type PrincipalsFunc func() []iauthnz.Principal
type TokenFunc func() string
type PLogEventFunc func() istructs.IPLogEvent
type CommandPrepareArgsFunc func() istructs.CommandPrepareArgs
type ArgFunc func() istructs.IObject
type UnloggedArgFunc func() istructs.IObject
type WLogOffsetFunc func() istructs.Offset
type OriginFunc func() string
type FederationFunc func() federation.IFederation
type QNameFunc func() appdef.QName
type TokensFunc func() itokens.ITokens
type PrepareArgsFunc func() istructs.PrepareArgs
type ExecQueryCallbackFunc func() istructs.ExecQueryCallback
type UnixTimeFunc func() int64

type ICommandProcessorStateParams interface {
	AppStructs() istructs.IAppStructs
	WSID() istructs.WSID
	CUD() istructs.ICUD
	Principals() []iauthnz.Principal
	Token() string
	CmdResultBuilder() istructs.IObjectBuilder
	CommandPrepareArgs() istructs.CommandPrepareArgs
	Arg() istructs.IObject
	UnloggedArg() istructs.IObject
	WLogOffset() istructs.Offset
	Origin() string
}

type IQueryProcessorStateParams interface {
	AppStructs() istructs.IAppStructs
	WSID() istructs.WSID
	Principals() []iauthnz.Principal
	Token() string
	PrepareArgs() istructs.PrepareArgs
	Arg() istructs.IObject
	ResultBuilder() istructs.IObjectBuilder
	QueryCallback() istructs.ExecQueryCallback
}

type ISyncActualizerStateParams interface {
	AppStructs() istructs.IAppStructs
	WSID() istructs.WSID
	PLogEvent() istructs.IPLogEvent
}

type MockedStateFactory func(ctx context.Context, intentsLimit int, appStructsFunc AppStructsFunc) IHostState
type CommandProcessorStateFactory func(ctx context.Context, params ICommandProcessorStateParams, secretReader isecrets.ISecretReader, intentsLimit int, stateOpts StateOpts) IHostState
type SyncActualizerStateFactory func(ctx context.Context, params ISyncActualizerStateParams, n10nFunc N10nFunc, secretReader isecrets.ISecretReader, intentsLimit int, stateOpts StateOpts) IHostState
type QueryProcessorStateFactory func(ctx context.Context, params IQueryProcessorStateParams, secretReader isecrets.ISecretReader, itokens itokens.ITokens, federation federation.IFederation, stateOpts StateOpts, httpClient httpu.IHTTPClient) IHostState
type AsyncActualizerStateFactory func(ctx context.Context, appStructsFunc AppStructsFunc, partitionIDFunc PartitionIDFunc, wsidFunc WSIDFunc, n10nFunc N10nFunc, secretReader isecrets.ISecretReader, eventFunc PLogEventFunc, tokensFunc itokens.ITokens, federationFunc federation.IFederation, intentsLimit, bundlesLimit int, stateOpts StateOpts, emailSender IEmailSender, httpClient httpu.IHTTPClient) IBundledHostState
type SchedulerStateFactory func(ctx context.Context, appStructsFunc AppStructsFunc, wsidFunc WSIDFunc, n10nFunc N10nFunc, secretReader isecrets.ISecretReader, tokensFunc itokens.ITokens, federationFunc federation.IFederation, unixTimeFunc UnixTimeFunc, intentsLimit int, optFuncs StateOpts, emailSender IEmailSender, httpClient httpu.IHTTPClient) IHostState

type FederationCommandHandler = func(owner, appname string, wsid istructs.WSID, command appdef.QName, body string) (statusCode int, newIDs map[string]istructs.RecordID, result string, err error)
type FederationBlobHandler = func(owner, appname string, wsid istructs.WSID, ownerRecord appdef.QName, ownerRecordField appdef.FieldName, ownerID istructs.RecordID) (result []byte, err error)
type UniquesHandler = func(entity appdef.QName, wsid istructs.WSID, data map[string]interface{}) (istructs.RecordID, error)

type EventsFunc func() istructs.IEvents
type RecordsFunc func() istructs.IRecords

type IEmailSender interface {
	Send(host string, msg EmailMessage, opts ...mail.Option) error
}

type EmailMessage struct {
	Subject string
	From    string
	To      []string
	CC      []string
	BCC     []string
	Body    string
}

type StateOpts struct {
	FederationCommandHandler FederationCommandHandler
	FederationBlobHandler    FederationBlobHandler
	UniquesHandler           UniquesHandler
}

type ApplyBatchItem struct {
	Key   istructs.IStateKeyBuilder
	Value istructs.IStateValueBuilder
	IsNew bool
}

type GetBatchItem struct {
	Key   istructs.IStateKeyBuilder
	Value istructs.IStateValue
}
