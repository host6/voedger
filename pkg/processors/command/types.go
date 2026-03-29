/*
 * Copyright (c) 2020-present unTill Pro, Ltd.
 */

package commandprocessor

import (
	"context"
	"time"

	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/appparts"
	"github.com/voedger/voedger/pkg/bus"
	"github.com/voedger/voedger/pkg/coreutils"
	"github.com/voedger/voedger/pkg/iauthnz"
	"github.com/voedger/voedger/pkg/iprocbus"
	"github.com/voedger/voedger/pkg/isecrets"
	"github.com/voedger/voedger/pkg/istructs"
	imetrics "github.com/voedger/voedger/pkg/metrics"
	"github.com/voedger/voedger/pkg/pipeline"
	"github.com/voedger/voedger/pkg/processors"
	"github.com/voedger/voedger/pkg/processors/actualizers"
	"github.com/voedger/voedger/pkg/state"
	"github.com/voedger/voedger/pkg/state/stateprovide"
)

type ServiceFactory func(commandsChannel CommandChannel) pipeline.IService
type CommandChannel iprocbus.ServiceChannel
type OperatorSyncActualizer pipeline.ISyncOperator
type SyncActualizerFactory func(vvmCtx context.Context, partitionID istructs.PartitionID) pipeline.ISyncOperator

type ValidateFunc func(ctx context.Context, appStructs istructs.IAppStructs, cudRow istructs.ICUDRow, wsid istructs.WSID) (err error)

type ICommandMessage interface {
	Body() []byte
	AppQName() appdef.AppQName
	WSID() istructs.WSID // url WSID
	Responder() bus.IResponder
	PartitionID() istructs.PartitionID
	RequestCtx() context.Context
	QName() appdef.QName // APIv1 -> cmd QName, APIv2 -> cmdQName or DocQName
	Token() string
	Host() string
	APIPath() processors.APIPath
	DocID() istructs.RecordID
	Method() string
	Origin() string
}

type xPath string

type commandProcessorMetrics struct {
	vvmName string
	app     appdef.AppQName
	metrics imetrics.IMetrics
}

func (m *commandProcessorMetrics) increase(metricName string, valueDelta float64) {
	m.metrics.IncreaseApp(metricName, m.vvmName, m.app, valueDelta)
}

type cmdWorkpiece struct {
	appParts                     appparts.IAppPartitions
	appPart                      appparts.IAppPartition
	appStructs                   istructs.IAppStructs
	requestData                  coreutils.MapObject
	cmdMes                       ICommandMessage
	argsObject                   istructs.IObject
	unloggedArgsObject           istructs.IObject
	reb                          istructs.IRawEventBuilder
	rawEvent                     istructs.IRawEvent
	pLogEvent                    istructs.IPLogEvent
	appPartition                 *appPartition
	workspace                    *workspace
	idGeneratorReporter          *implIDGeneratorReporter
	eca                          istructs.ExecCommandArgs
	metrics                      commandProcessorMetrics
	syncProjectorsStart          time.Time
	principals                   []iauthnz.Principal
	roles                        []appdef.QName
	parsedCUDs                   []parsedCUD
	wsDesc                       istructs.IRecord
	hostStateProvider            *hostStateProvider
	wsInitialized                bool
	cmdResultBuilder             istructs.IObjectBuilder
	cmdResult                    istructs.IObject
	iCommand                     appdef.ICommand
	iWorkspace                   appdef.IWorkspace
	appPartitionRestartScheduled bool
	cmdQName                     appdef.QName
	statusCodeOfSuccess          int
	reapplier                    istructs.IEventReapplier
	commandCtxStorage            istructs.IStateValue
	cmdResToLog                  string
	pLogOffset                   istructs.Offset // need for logging
	logCtxForSyncProjectors      context.Context // enriched log ctx from logEventAndCUDs (woffset, poffset, evqname), used by sync projectors
}

type implIDGeneratorReporter struct {
	istructs.IIDGenerator
	generatedIDs map[istructs.RecordID]istructs.RecordID
}

type parsedCUD struct {
	opKind         appdef.OperationKind // update can not be activate\deactivate because IsActive modified -> other fields update is not allowed, see
	existingRecord istructs.IRecord     // create -> nil
	id             int64
	qName          appdef.QName
	fields         coreutils.MapObject
	xPath          xPath
}

type implICommandMessage struct {
	body        []byte
	appQName    appdef.AppQName // need to determine where to send c.sys.Init request on create a new workspace
	wsid        istructs.WSID
	responder   bus.IResponder
	partitionID istructs.PartitionID
	requestCtx  context.Context
	qName       appdef.QName // APIv1 -> cmd QName, APIv2 -> cmdQName or DocQName
	token       string
	host        string
	apiPath     processors.APIPath
	docID       istructs.RecordID
	method      string
	origin      string
}

type wrongArgsCatcher struct {
	pipeline.NOOP
}

func (cmd *cmdWorkpiece) AppStructs() istructs.IAppStructs          { return cmd.appStructs }
func (cmd *cmdWorkpiece) WSID() istructs.WSID                       { return cmd.cmdMes.WSID() }
func (cmd *cmdWorkpiece) CUD() istructs.ICUD                        { return cmd.reb.CUDBuilder() }
func (cmd *cmdWorkpiece) Principals() []iauthnz.Principal           { return cmd.principals }
func (cmd *cmdWorkpiece) Token() string                             { return cmd.cmdMes.Token() }
func (cmd *cmdWorkpiece) CmdResultBuilder() istructs.IObjectBuilder { return cmd.cmdResultBuilder }
func (cmd *cmdWorkpiece) CommandPrepareArgs() istructs.CommandPrepareArgs {
	return cmd.eca.CommandPrepareArgs
}
func (cmd *cmdWorkpiece) Arg() istructs.IObject         { return cmd.argsObject }
func (cmd *cmdWorkpiece) UnloggedArg() istructs.IObject { return cmd.unloggedArgsObject }
func (cmd *cmdWorkpiece) WLogOffset() istructs.Offset   { return cmd.workspace.NextWLogOffset }
func (cmd *cmdWorkpiece) Origin() string                { return cmd.cmdMes.Origin() }

type hostStateProvider struct {
	state state.IHostState
	cmd   *cmdWorkpiece
}

func newHostStateProvider(ctx context.Context, secretReader isecrets.ISecretReader) *hostStateProvider {
	p := &hostStateProvider{}
	p.state = stateprovide.ProvideCommandProcessorStateFactory()(ctx, p, secretReader, actualizers.DefaultIntentsLimit, state.NullOpts)
	return p
}

func (p *hostStateProvider) AppStructs() istructs.IAppStructs { return p.cmd.AppStructs() }
func (p *hostStateProvider) WSID() istructs.WSID              { return p.cmd.WSID() }
func (p *hostStateProvider) CUD() istructs.ICUD               { return p.cmd.CUD() }
func (p *hostStateProvider) Principals() []iauthnz.Principal  { return p.cmd.Principals() }
func (p *hostStateProvider) Token() string                    { return p.cmd.Token() }
func (p *hostStateProvider) CmdResultBuilder() istructs.IObjectBuilder {
	return p.cmd.CmdResultBuilder()
}
func (p *hostStateProvider) CommandPrepareArgs() istructs.CommandPrepareArgs {
	return p.cmd.CommandPrepareArgs()
}
func (p *hostStateProvider) WLogOffset() istructs.Offset   { return p.cmd.WLogOffset() }
func (p *hostStateProvider) Origin() string                { return p.cmd.Origin() }
func (p *hostStateProvider) Arg() istructs.IObject         { return p.cmd.Arg() }
func (p *hostStateProvider) UnloggedArg() istructs.IObject { return p.cmd.UnloggedArg() }

func (p *hostStateProvider) bind(cmd *cmdWorkpiece) state.IHostState {
	p.cmd = cmd
	return p.state
}
