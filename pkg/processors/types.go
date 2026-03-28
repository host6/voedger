/*
 * Copyright (c) 2024-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package processors

import (
	"context"

	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/appparts"
	"github.com/voedger/voedger/pkg/iauthnz"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/pipeline"
)

type VVMName string

type APIPath int

type ICmdProcWorkpiece interface {
	pipeline.IWorkpiece
	AppPartitions() appparts.IAppPartitions
	Context() context.Context
}

type ISyncProjectorWorkpiece interface {
	ICmdProcWorkpiece
	AppPartition() appparts.IAppPartition
	Event() istructs.IPLogEvent
	LogCtx() context.Context
	PLogOffset() istructs.Offset
}

type IQueryProcWorkpiece interface {
	pipeline.IWorkpiece
	ResetRateLimit(appdef.QName, appdef.OperationKind)
	GetPrincipals() []iauthnz.Principal
	AppPartition() appparts.IAppPartition
	AppPartitions() appparts.IAppPartitions
	Roles() []appdef.QName
}
