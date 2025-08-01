/*
 * Copyright (c) 2024-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package federation

import (
	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/coreutils"
	"github.com/voedger/voedger/pkg/iblobstorage"
	"github.com/voedger/voedger/pkg/in10n"
	"github.com/voedger/voedger/pkg/istructs"
)

type iFederationBase interface {
	Func(relativeURL string, body string, optFuncs ...coreutils.ReqOptFunc) (*coreutils.FuncResponse, error)
	Query(relativeURL string, optFuncs ...coreutils.ReqOptFunc) (*coreutils.FuncResponse, error)
	UploadBLOB(appQName appdef.AppQName, wsid istructs.WSID, blobReader iblobstorage.BLOBReader, optFuncs ...coreutils.ReqOptFunc) (blobID istructs.RecordID, err error)
	UploadTempBLOB(appQName appdef.AppQName, wsid istructs.WSID, blobReader iblobstorage.BLOBReader, duration iblobstorage.DurationType,
		optFuncs ...coreutils.ReqOptFunc) (blobSUUID iblobstorage.SUUID, err error)
	ReadBLOB(appQName appdef.AppQName, wsid istructs.WSID, ownerRecord appdef.QName, ownerRecordField appdef.FieldName, ownerID istructs.RecordID,
		optFuncs ...coreutils.ReqOptFunc) (iblobstorage.BLOBReader, error)
	ReadTempBLOB(appQName appdef.AppQName, wsid istructs.WSID, blobSUUID iblobstorage.SUUID, optFuncs ...coreutils.ReqOptFunc) (iblobstorage.BLOBReader, error)
	URLStr() string
	Port() int
	N10NUpdate(key in10n.ProjectionKey, val int64, optFuncs ...coreutils.ReqOptFunc) error
	N10NSubscribe(projectionKey in10n.ProjectionKey) (offsetsChan OffsetsChan, unsubscribe func(), err error)
	AdminFunc(relativeURL string, body string, optFuncs ...coreutils.ReqOptFunc) (*coreutils.FuncResponse, error)
}

type IFederation interface {
	iFederationBase
	WithRetry() IFederationWithRetry
}

// IFederationForQP is a specialized interface for query processing (QP) scenarios.
// Unlike IFederation, it provides a QueryNoRetry method that does not retry on HTTP 503 errors.
// This behavior is designed to prevent the depletion of query processing resources.
type IFederationForQP interface {
	// unlike IFederation.Query does not retry on 503 to avoid QPs depleetion
	QueryNoRetry(relativeURL string, optFuncs ...coreutils.ReqOptFunc) (*coreutils.FuncResponse, error)
}

// need for Workspace init workflow
// has WithRetryOn503 default option
type IFederationWithRetry interface {
	iFederationBase
	dummy()
}
