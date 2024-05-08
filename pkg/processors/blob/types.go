/*
 * Copyright (c) 2024-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package blob

import (
	"net/http"

	"github.com/voedger/voedger/pkg/iprocbus"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/pipeline"
)

type BLOBWriteDetailsSingle struct {
	Name     string
	MimeType string
}

type BLOBWriteDetailsMultipart struct {
	Boundary string
}

type BLOBReadDetails struct {
	BLOBID istructs.RecordID
}

type BLOBBaseMessage struct {
	Req      *http.Request
	Resp     http.ResponseWriter
	DoneChan chan struct{}
	WSID     istructs.WSID
	AppQName istructs.AppQName
	Header   map[string][]string
}

type BLOBMessage struct {
	BLOBBaseMessage
	BLOBDetails interface{}
}

func (bm *BLOBBaseMessage) Release() {
	bm.Req.Body.Close()
}

type BLOBMaxSizeType int64

type BLOBProcBus iprocbus.IProcBus

type ServiceFactory func(sc BLOBProcBus) pipeline.IService
