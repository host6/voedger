/*
 * Copyright (c) 2024-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package blob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/voedger/voedger/pkg/iblobstorage"
	"github.com/voedger/voedger/pkg/istructs"
	coreutils "github.com/voedger/voedger/pkg/utils"
	ibus "github.com/voedger/voedger/staging/src/github.com/untillpro/airs-ibus"
)

func blobReadMessageHandler(bbm BLOBBaseMessage, blobReadDetails BLOBReadDetails, blobStorage iblobstorage.IBLOBStorage, bus ibus.IBus, busTimeout time.Duration) {
	defer close(bbm.DoneChan)

	// request to VVM to check the principalToken
	req := ibus.Request{
		Method:   ibus.HTTPMethodPOST,
		WSID:     int64(bbm.WSID),
		AppQName: bbm.AppQName.String(),
		Resource: "c.sys.DownloadBLOBHelper",
		Header:   bbm.Header,
		Body:     []byte(`{}`),
		Host:     coreutils.Localhost,
	}
	blobHelperResp, _, _, err := bus.SendRequest2(bbm.Req.Context(), req, busTimeout)
	if err != nil {
		coreutils.WriteTextResponse(bbm.Resp, "failed to exec c.sys.DownloadBLOBHelper: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if blobHelperResp.StatusCode != http.StatusOK {
		coreutils.WriteTextResponse(bbm.Resp, "c.sys.DownloadBLOBHelper returned error: "+string(blobHelperResp.Data), blobHelperResp.StatusCode)
		return
	}

	// read the BLOB
	key := iblobstorage.KeyType{
		AppID: istructs.ClusterAppID_sys_blobber,
		WSID:  bbm.WSID,
		ID:    blobReadDetails.BLOBID,
	}
	stateWriterDiscard := func(state iblobstorage.BLOBState) error {
		if state.Status != iblobstorage.BLOBStatus_Completed {
			return errors.New("blob is not completed")
		}
		if len(state.Error) > 0 {
			return errors.New(state.Error)
		}
		bbm.Resp.Header().Set(coreutils.ContentType, state.Descr.MimeType)
		bbm.Resp.Header().Add("Content-Disposition", fmt.Sprintf(`attachment;filename="%s"`, state.Descr.Name))
		bbm.Resp.WriteHeader(http.StatusOK)
		return nil
	}
	if err := blobStorage.ReadBLOB(bbm.Req.Context(), key, stateWriterDiscard, bbm.Resp); err != nil {
		if errors.Is(err, iblobstorage.ErrBLOBNotFound) {
			coreutils.WriteTextResponse(bbm.Resp, err.Error(), http.StatusNotFound)
			return
		}
		coreutils.WriteTextResponse(bbm.Resp, err.Error(), http.StatusInternalServerError)
	}
}

func blobWriteMessageHandlerSingle(bbm BLOBBaseMessage, blobWriteDetails BLOBWriteDetailsSingle, blobStorage iblobstorage.IBLOBStorage, header map[string][]string,
	bus ibus.IBus, busTimeout time.Duration, blobMaxSize BLOBMaxSizeType) {
	defer close(bbm.DoneChan)

	blobID := writeBLOB(bbm.Req.Context(), int64(bbm.WSID), bbm.AppQName.String(), header, bbm.Resp, blobWriteDetails.Name,
		blobWriteDetails.MimeType, blobStorage, bbm.Req.Body, int64(blobMaxSize), bus, busTimeout)
	if blobID > 0 {
		coreutils.WriteTextResponse(bbm.Resp, strconv.FormatInt(blobID, coreutils.DecimalBase), http.StatusOK)
	}
}

func writeBLOB(ctx context.Context, wsid int64, appQName string, header map[string][]string, resp http.ResponseWriter,
	blobName, blobMimeType string, blobStorage iblobstorage.IBLOBStorage, body io.ReadCloser,
	blobMaxSize int64, bus ibus.IBus, busTimeout time.Duration) (blobID int64) {
	// request VVM for check the principalToken and get a blobID
	req := ibus.Request{
		Method:   ibus.HTTPMethodPOST,
		WSID:     wsid,
		AppQName: appQName,
		Resource: "c.sys.UploadBLOBHelper",
		Body:     []byte(`{}`),
		Header:   header,
		Host:     coreutils.Localhost,
	}
	blobHelperResp, _, _, err := bus.SendRequest2(ctx, req, busTimeout)
	if err != nil {
		coreutils.WriteTextResponse(resp, "failed to exec c.sys.UploadBLOBHelper: "+err.Error(), http.StatusInternalServerError)
		return 0
	}
	if blobHelperResp.StatusCode != http.StatusOK {
		coreutils.WriteTextResponse(resp, "c.sys.UploadBLOBHelper returned error: "+string(blobHelperResp.Data), blobHelperResp.StatusCode)
		return 0
	}
	cmdResp := map[string]interface{}{}
	if err := json.Unmarshal(blobHelperResp.Data, &cmdResp); err != nil {
		coreutils.WriteTextResponse(resp, "failed to json-unmarshal c.sys.UploadBLOBHelper result: "+err.Error(), http.StatusInternalServerError)
		return 0
	}
	newIDs := cmdResp["NewIDs"].(map[string]interface{})

	blobID = int64(newIDs["1"].(float64))
	// write the BLOB
	key := iblobstorage.KeyType{
		AppID: istructs.ClusterAppID_sys_blobber,
		WSID:  istructs.WSID(wsid),
		ID:    istructs.RecordID(blobID),
	}
	descr := iblobstorage.DescrType{
		Name:     blobName,
		MimeType: blobMimeType,
	}

	if err := blobStorage.WriteBLOB(ctx, key, descr, body, blobMaxSize); err != nil {
		if errors.Is(err, iblobstorage.ErrBLOBSizeQuotaExceeded) {
			coreutils.WriteTextResponse(resp, fmt.Sprintf("blob size quouta exceeded (max %d allowed)", blobMaxSize), http.StatusForbidden)
			return 0
		}
		coreutils.WriteTextResponse(resp, err.Error(), http.StatusInternalServerError)
		return 0
	}

	// set WDoc<sys.BLOB>.status = BLOBStatus_Completed
	req.Resource = "c.sys.CUD"
	req.Body = []byte(fmt.Sprintf(`{"cuds":[{"sys.ID": %d,"fields":{"status":%d}}]}`, blobID, iblobstorage.BLOBStatus_Completed))
	cudWDocBLOBUpdateResp, _, _, err := bus.SendRequest2(ctx, req, busTimeout)
	if err != nil {
		coreutils.WriteTextResponse(resp, "failed to exec c.sys.CUD: "+err.Error(), http.StatusInternalServerError)
		return 0
	}
	if cudWDocBLOBUpdateResp.StatusCode != http.StatusOK {
		coreutils.WriteTextResponse(resp, "c.sys.CUD returned error: "+string(cudWDocBLOBUpdateResp.Data), cudWDocBLOBUpdateResp.StatusCode)
		return 0
	}

	return blobID
}

func blobWriteMessageHandlerMultipart(bbm BLOBBaseMessage, blobStorage iblobstorage.IBLOBStorage, boundary string,
	bus ibus.IBus, busTimeout time.Duration, blobMaxSize BLOBMaxSizeType) {
	defer close(bbm.DoneChan)

	r := multipart.NewReader(bbm.Req.Body, boundary)
	var part *multipart.Part
	var err error
	blobIDs := []string{}
	partNum := 0
	for err == nil {
		part, err = r.NextPart()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				coreutils.WriteTextResponse(bbm.Resp, "failed to parse multipart: "+err.Error(), http.StatusBadRequest)
				return
			} else if partNum == 0 {
				coreutils.WriteTextResponse(bbm.Resp, "empty multipart request", http.StatusBadRequest)
				return
			}
			break
		}
		contentDisposition := part.Header.Get("Content-Disposition")
		mediaType, params, err := mime.ParseMediaType(contentDisposition)
		if err != nil {
			coreutils.WriteTextResponse(bbm.Resp, fmt.Sprintf("failed to parse Content-Disposition of part number %d: %s", partNum, contentDisposition), http.StatusBadRequest)
		}
		if mediaType != "form-data" {
			coreutils.WriteTextResponse(bbm.Resp, fmt.Sprintf("unsupported ContentDisposition mediaType of part number %d: %s", partNum, mediaType), http.StatusBadRequest)
		}
		contentType := part.Header.Get(coreutils.ContentType)
		if len(contentType) == 0 {
			contentType = "application/x-binary"
		}
		part.Header[coreutils.Authorization] = bbm.Header[coreutils.Authorization] // add auth header for c.sys.*BLOBHelper
		blobID := writeBLOB(bbm.Req.Context(), int64(bbm.WSID), bbm.AppQName.String(), part.Header, bbm.Resp,
			params["name"], contentType, blobStorage, part, int64(blobMaxSize), bus, busTimeout)
		if blobID == 0 {
			return // request handled
		}
		blobIDs = append(blobIDs, strconv.FormatInt(blobID, coreutils.DecimalBase))
		partNum++
	}
	coreutils.WriteTextResponse(bbm.Resp, strings.Join(blobIDs, ","), http.StatusOK)
}
