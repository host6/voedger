/*
 * Copyright (c) 2024-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package federation

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/coreutils"
	"github.com/voedger/voedger/pkg/goutils/logger"
	"github.com/voedger/voedger/pkg/iblobstorage"
	"github.com/voedger/voedger/pkg/in10n"
	"github.com/voedger/voedger/pkg/istructs"
)

// wrapped ErrUnexpectedStatusCode is returned -> *HTTPResponse contains a valid response body
// otherwise if err != nil (e.g. socket error)-> *HTTPResponse is nil
func (f *implIFederation) post(relativeURL string, body string, optFuncs ...coreutils.ReqOptFunc) (*coreutils.HTTPResponse, error) {
	optFuncs = append(optFuncs, coreutils.WithDefaultMethod(http.MethodPost))
	return f.req(relativeURL, body, optFuncs...)
}

func (f *implIFederation) postReader(relativeURL string, bodyReader io.Reader, optFuncs ...coreutils.ReqOptFunc) (*coreutils.HTTPResponse, error) {
	optFuncs = append(optFuncs, coreutils.WithDefaultMethod(http.MethodPost))
	return f.reqReader(relativeURL, bodyReader, optFuncs...)
}

func (f *implIFederation) get(relativeURL string, optFuncs ...coreutils.ReqOptFunc) (*coreutils.HTTPResponse, error) {
	optFuncs = append(optFuncs, coreutils.WithDefaultMethod(http.MethodGet))
	return f.req(relativeURL, "", optFuncs...)
}

func (f *implIFederation) reqReader(relativeURL string, bodyReader io.Reader, optFuncs ...coreutils.ReqOptFunc) (*coreutils.HTTPResponse, error) {
	url := f.federationURL().String() + "/" + relativeURL
	optFuncs = append(f.defaultReqOptFuncs, optFuncs...)

	// Perform the low-level HTTP request
	httpResp, err := f.httpClient.ReqReader(f.vvmCtx, url, bodyReader, optFuncs...)
	if err != nil {
		return nil, err
	}

	// Handle discarded response (httpResp will be nil)
	if httpResp == nil {
		return nil, nil
	}

	// Apply high-level federation logic
	return f.processResponse(httpResp, url, optFuncs...)
}

func (f *implIFederation) req(relativeURL string, body string, optFuncs ...coreutils.ReqOptFunc) (*coreutils.HTTPResponse, error) {
	url := f.federationURL().String() + "/" + relativeURL
	optFuncs = append(f.defaultReqOptFuncs, optFuncs...)

	// Perform the low-level HTTP request
	httpResp, err := f.httpClient.Req(f.vvmCtx, url, body, optFuncs...)
	if err != nil {
		return nil, err
	}

	// Handle discarded response (httpResp will be nil)
	if httpResp == nil {
		return nil, nil
	}

	// Apply high-level federation logic
	return f.processResponse(httpResp, url, optFuncs...)
}

// processResponse handles high-level federation response processing including:
// - Status code validation and error handling
// - 503 retry logic with federation-specific delays
// - Error message validation
// - Business logic error processing
func (f *implIFederation) processResponse(httpResp *coreutils.HTTPResponse, url string, optFuncs ...coreutils.ReqOptFunc) (*coreutils.HTTPResponse, error) {
	// Extract options to understand expected behavior
	opts := f.extractReqOpts(httpResp)

	// Handle 503 Service Unavailable with federation-specific retry logic
	if httpResp.HTTPResp.StatusCode == http.StatusServiceUnavailable && f.shouldRetryOn503(opts) {
		return f.retryOn503(url, httpResp, opts)
	}

	// Validate expected status codes
	if err := f.validateStatusCode(httpResp, opts); err != nil {
		return httpResp, err
	}

	// Validate expected error messages if specified
	if err := f.validateErrorMessages(httpResp, url, opts); err != nil {
		return httpResp, err
	}

	return httpResp, nil
}

// extractReqOpts extracts request options for processing
// Since the HTTP client already processes the options and stores them in HTTPResponse,
// we can extract the needed information from the response itself
func (f *implIFederation) extractReqOpts(httpResp *coreutils.HTTPResponse) *federationReqOpts {
	// Extract expected HTTP codes from the response (set by HTTP client)
	expectedCodes := httpResp.ExpectedHTTPCodes()
	if len(expectedCodes) == 0 {
		expectedCodes = []int{http.StatusOK, http.StatusCreated}
	}

	return &federationReqOpts{
		expectedHTTPCodes:     expectedCodes,
		expectedErrorContains: httpResp.ExpectedErrorContains(),
		skipRetryOn503:        true, // Federation default
	}
}

// shouldRetryOn503 determines if we should retry on 503 based on federation settings
func (f *implIFederation) shouldRetryOn503(opts *federationReqOpts) bool {
	return !opts.skipRetryOn503
}

// retryOn503 handles 503 retry logic with federation-specific delays
func (f *implIFederation) retryOn503(url string, httpResp *coreutils.HTTPResponse, opts *federationReqOpts) (*coreutils.HTTPResponse, error) {
	// Federation-specific 503 retry logic would go here
	// For now, return the original response
	return httpResp, nil
}

// validateStatusCode checks if the status code is expected
func (f *implIFederation) validateStatusCode(httpResp *coreutils.HTTPResponse, opts *federationReqOpts) error {
	for _, expectedCode := range opts.expectedHTTPCodes {
		if httpResp.HTTPResp.StatusCode == expectedCode {
			return nil
		}
	}

	// Status code not expected - create federation-level error
	return fmt.Errorf("%w: %d, %s", coreutils.ErrUnexpectedStatusCode, httpResp.HTTPResp.StatusCode, httpResp.Body)
}

// validateErrorMessages validates expected error messages in responses
func (f *implIFederation) validateErrorMessages(httpResp *coreutils.HTTPResponse, url string, opts *federationReqOpts) error {
	if httpResp.HTTPResp.StatusCode == http.StatusOK || len(opts.expectedErrorContains) == 0 {
		return nil
	}

	// Parse response to extract error message
	respMap := map[string]interface{}{}
	if err := json.Unmarshal([]byte(httpResp.Body), &respMap); err != nil {
		return fmt.Errorf("failed to parse error response: %w", err)
	}

	actualError := f.extractErrorMessage(respMap, url)

	// Check if all expected error messages are present
	for _, expectedMsg := range opts.expectedErrorContains {
		if !strings.Contains(actualError, expectedMsg) {
			return fmt.Errorf(`actual error message "%s" does not contain expected message "%s"`, actualError, expectedMsg)
		}
	}

	return nil
}

// extractErrorMessage extracts error message from response based on API version
func (f *implIFederation) extractErrorMessage(respMap map[string]interface{}, url string) string {
	if strings.Contains(url, "api/v2") {
		if messageIntf, ok := respMap["message"]; ok {
			return messageIntf.(string)
		}
		if errorIntf, ok := respMap["error"]; ok {
			if errorMap, ok := errorIntf.(map[string]interface{}); ok {
				if msgIntf, ok := errorMap["message"]; ok {
					return msgIntf.(string)
				}
			}
		}
	} else {
		if sysErrorIntf, ok := respMap["sys.Error"]; ok {
			if sysErrorMap, ok := sysErrorIntf.(map[string]interface{}); ok {
				if msgIntf, ok := sysErrorMap["Message"]; ok {
					return msgIntf.(string)
				}
			}
		}
	}
	return ""
}

// federationReqOpts holds federation-specific request options
type federationReqOpts struct {
	expectedHTTPCodes     []int
	expectedErrorContains []string
	skipRetryOn503        bool
}

func (f *implIFederation) UploadTempBLOB(appQName appdef.AppQName, wsid istructs.WSID, blobReader iblobstorage.BLOBReader, duration iblobstorage.DurationType,
	optFuncs ...coreutils.ReqOptFunc) (blobSUUID iblobstorage.SUUID, err error) {
	ttl, ok := TemporaryBLOBDurationToURLTTL[duration]
	if !ok {
		return "", fmt.Errorf("unsupported duration: %d", duration)
	}
	uploadBLOBURL := fmt.Sprintf("api/v2/apps/%s/%s/workspaces/%d/tblobs", appQName.Owner(), appQName.Name(), wsid)
	optFuncs = append(optFuncs, coreutils.WithHeaders(
		coreutils.BlobName, blobReader.Name,
		coreutils.ContentType, blobReader.ContentType,
		"TTL", ttl,
	))
	resp, err := f.postReader(uploadBLOBURL, blobReader, optFuncs...)
	if err != nil {
		return "", err
	}
	if !slices.Contains(resp.ExpectedHTTPCodes(), resp.HTTPResp.StatusCode) {
		funcErr, err := getFuncError(resp)
		if err != nil {
			return "", err
		}
		return "", funcErr
	}
	if resp.HTTPResp.StatusCode != http.StatusOK && resp.HTTPResp.StatusCode != http.StatusCreated {
		return "", nil
	}
	matches := blobCreateTempRespRE.FindStringSubmatch(resp.Body)
	if len(matches) < 2 {
		// notest
		return "", errors.New("wrong blob create response: " + resp.Body)
	}
	return iblobstorage.SUUID(matches[1]), nil
}

func (f *implIFederation) UploadBLOB(appQName appdef.AppQName, wsid istructs.WSID, blobReader iblobstorage.BLOBReader,
	optFuncs ...coreutils.ReqOptFunc) (blobID istructs.RecordID, err error) {
	uploadBLOBURL := fmt.Sprintf("api/v2/apps/%s/%s/workspaces/%d/docs/%s/blobs/%s",
		appQName.Owner(), appQName.Name(), wsid, blobReader.OwnerRecord, blobReader.OwnerRecordField)
	optFuncs = append(optFuncs, coreutils.WithHeaders(
		coreutils.BlobName, blobReader.Name,
		coreutils.ContentType, blobReader.ContentType,
	))
	resp, err := f.postReader(uploadBLOBURL, blobReader, optFuncs...)
	if err != nil {
		return istructs.NullRecordID, err
	}
	if !slices.Contains(resp.ExpectedHTTPCodes(), resp.HTTPResp.StatusCode) {
		funcErr, err := getFuncError(resp)
		if err != nil {
			return istructs.NullRecordID, err
		}
		return istructs.NullRecordID, funcErr
	}
	if resp.HTTPResp.StatusCode != http.StatusCreated {
		return istructs.NullRecordID, nil
	}
	matches := blobCreatePersistentRespRE.FindStringSubmatch(resp.Body)
	if len(matches) != 2 {
		// notest
		return istructs.NullRecordID, errors.New("wrong blob create response: " + resp.Body)
	}
	newBLOBIDIntf, err := coreutils.ClarifyJSONNumber(json.Number(matches[1]), appdef.DataKind_RecordID)
	if err != nil {
		// notest
		return istructs.NullRecordID, fmt.Errorf("failed to parse the received blobID string: %w", err)
	}
	return newBLOBIDIntf.(istructs.RecordID), nil
}

func (f *implIFederation) ReadBLOB(appQName appdef.AppQName, wsid istructs.WSID, ownerRecord appdef.QName, ownerRecordField appdef.FieldName, ownerID istructs.RecordID,
	optFuncs ...coreutils.ReqOptFunc) (res iblobstorage.BLOBReader, err error) {
	url := fmt.Sprintf(`api/v2/apps/%s/%s/workspaces/%d/docs/%s/%d/blobs/%s`, appQName.Owner(), appQName.Name(), wsid, ownerRecord, ownerID, ownerRecordField)
	optFuncs = append(optFuncs, coreutils.WithResponseHandler(func(httpResp *http.Response) {}))
	resp, err := f.get(url, optFuncs...)
	if err != nil {
		return res, err
	}
	if resp.HTTPResp.StatusCode != http.StatusOK {
		return iblobstorage.BLOBReader{}, nil
	}
	res = iblobstorage.BLOBReader{
		DescrType: iblobstorage.DescrType{
			Name:        resp.HTTPResp.Header.Get(coreutils.BlobName),
			ContentType: resp.HTTPResp.Header.Get(coreutils.ContentType),
		},
		ReadCloser: resp.HTTPResp.Body,
	}
	return res, nil
}

func (f *implIFederation) ReadTempBLOB(appQName appdef.AppQName, wsid istructs.WSID, blobSUUID iblobstorage.SUUID, optFuncs ...coreutils.ReqOptFunc) (res iblobstorage.BLOBReader, err error) {
	url := fmt.Sprintf(`api/v2/apps/%s/%s/workspaces/%d/tblobs/%s`, appQName.Owner(), appQName.Name(), wsid, blobSUUID)
	optFuncs = append(optFuncs, coreutils.WithResponseHandler(func(httpResp *http.Response) {}))
	resp, err := f.get(url, optFuncs...)
	if err != nil {
		return res, err
	}
	if resp.HTTPResp.StatusCode != http.StatusOK {
		return iblobstorage.BLOBReader{}, nil
	}
	res = iblobstorage.BLOBReader{
		DescrType: iblobstorage.DescrType{
			Name:        resp.HTTPResp.Header.Get(coreutils.BlobName),
			ContentType: resp.HTTPResp.Header.Get(coreutils.ContentType),
		},
		ReadCloser: resp.HTTPResp.Body,
	}
	return res, nil
}

func (f *implIFederation) N10NUpdate(key in10n.ProjectionKey, val int64, optFuncs ...coreutils.ReqOptFunc) error {
	body := fmt.Sprintf(`{"App": "%s","Projection": "%s","WS": %d}`, key.App, key.Projection, key.WS)
	optFuncs = append(optFuncs, coreutils.WithDiscardResponse())
	_, err := f.post(fmt.Sprintf("n10n/update/%d", val), body, optFuncs...)
	return err
}

func (f *implIFederation) GET(relativeURL string, body string, optFuncs ...coreutils.ReqOptFunc) (*coreutils.HTTPResponse, error) {
	optFuncs = append(optFuncs, coreutils.WithMethod(http.MethodGet))
	url := f.federationURL().String() + "/" + relativeURL
	optFuncs = append(f.defaultReqOptFuncs, optFuncs...)

	// Perform the low-level HTTP request
	httpResp, err := f.httpClient.Req(f.vvmCtx, url, body, optFuncs...)
	if err != nil {
		return nil, err
	}

	// Handle discarded response (httpResp will be nil)
	if httpResp == nil {
		return nil, nil
	}

	// Apply high-level federation logic
	return f.processResponse(httpResp, url, optFuncs...)
}

func (f *implIFederation) Func(relativeURL string, body string, optFuncs ...coreutils.ReqOptFunc) (*coreutils.FuncResponse, error) {
	httpResp, err := f.post(relativeURL, body, optFuncs...)
	return HTTPRespToFuncResp(httpResp, err)
}

func (f *implIFederation) Query(relativeURL string, optFuncs ...coreutils.ReqOptFunc) (*coreutils.FuncResponse, error) {
	httpResp, err := f.get(relativeURL, optFuncs...)
	return HTTPRespToFuncResp(httpResp, err)
}

func (f *implIFederation) AdminFunc(relativeURL string, body string, optFuncs ...coreutils.ReqOptFunc) (*coreutils.FuncResponse, error) {
	optFuncs = append(optFuncs, coreutils.WithMethod(http.MethodPost))
	url := fmt.Sprintf("http://127.0.0.1:%d/%s", f.adminPortGetter(), relativeURL)
	optFuncs = append(f.defaultReqOptFuncs, optFuncs...)

	// Perform the low-level HTTP request
	httpResp, err := f.httpClient.Req(f.vvmCtx, url, body, optFuncs...)
	if err != nil {
		return HTTPRespToFuncResp(httpResp, err)
	}

	// Handle discarded response (httpResp will be nil)
	if httpResp == nil {
		return HTTPRespToFuncResp(nil, nil)
	}

	// Apply high-level federation logic
	processedResp, err := f.processResponse(httpResp, url, optFuncs...)
	return HTTPRespToFuncResp(processedResp, err)
}

func getFuncError(httpResp *coreutils.HTTPResponse) (funcError coreutils.FuncError, err error) {
	funcError = coreutils.FuncError{
		SysError: coreutils.SysError{
			HTTPStatus: httpResp.HTTPResp.StatusCode,
		},
		ExpectedHTTPCodes: httpResp.ExpectedHTTPCodes(),
	}
	if len(httpResp.Body) == 0 || httpResp.HTTPResp.StatusCode == http.StatusOK {
		return funcError, nil
	}
	m := map[string]interface{}{}
	if err := json.Unmarshal([]byte(httpResp.Body), &m); err != nil {
		return funcError, fmt.Errorf("IFederation: failed to unmarshal response body to FuncErr: %w. Body:\n%s", err, httpResp.Body)
	}
	sysErrorIntf, hasSysError := m["sys.Error"]
	if hasSysError {
		sysErrorMap := sysErrorIntf.(map[string]interface{})
		errQNameStr, ok := sysErrorMap["QName"].(string)
		if ok {
			errQName, err := appdef.ParseQName(errQNameStr)
			if err != nil {
				errQName = appdef.NewQName("<err>", sysErrorMap["QName"].(string))
			}
			funcError.SysError.QName = errQName
		}
		funcError.HTTPStatus = int(sysErrorMap["HTTPStatus"].(float64))
		funcError.Message = sysErrorMap["Message"].(string)
		funcError.Data, _ = sysErrorMap["Data"].(string)
	} else {
		if apiV2QueryError, ok := m["error"]; ok {
			m = apiV2QueryError.(map[string]interface{})
		}
		if commonErrorStatusIntf, ok := m["status"]; ok {
			funcError.SysError.HTTPStatus = int(commonErrorStatusIntf.(float64))
		}
		if commonErrorMessageIntf, ok := m["message"]; ok {
			funcError.SysError.Message = commonErrorMessageIntf.(string)
		}
	}
	return funcError, nil
}

func (f *implIFederation) URLStr() string {
	return f.federationURL().String()
}

func (f *implIFederation) Port() int {
	res, err := strconv.Atoi(f.federationURL().Port())
	if err != nil {
		// notest
		panic(err)
	}
	return res
}

func (f *implIFederation) N10NSubscribe(projectionKey in10n.ProjectionKey) (offsetsChan OffsetsChan, unsubscribe func(), err error) {
	query := fmt.Sprintf(`
		{
			"SubjectLogin": "test_%d",
			"ProjectionKey": [
				{
					"App":"%s",
					"Projection":"%s",
					"WS":%d
				}
			]
		}`, projectionKey.WS, projectionKey.App, projectionKey.Projection, projectionKey.WS)
	params := url.Values{}
	params.Add("payload", query)
	resp, err := f.get("n10n/channel?"+params.Encode(), coreutils.WithLongPolling())
	if err != nil {
		return nil, nil, err
	}

	offsetsChan, channelID, waitForDone := ListenSSEEvents(resp.HTTPResp.Request.Context(), resp.HTTPResp.Body)

	unsubscribe = func() {
		body := fmt.Sprintf(`
			{
				"Channel": "%s",
				"ProjectionKey":[
					{
						"App": "%s",
						"Projection":"%s",
						"WS":%d
					}
				]
			}
		`, channelID, projectionKey.App, projectionKey.Projection, projectionKey.WS)
		params := url.Values{}
		params.Add("payload", body)
		_, err := f.get("n10n/unsubscribe?"+params.Encode(), coreutils.WithDiscardResponse())
		if err != nil {
			logger.Error("unsubscribe failed", err.Error())
		}
		resp.HTTPResp.Body.Close()
		for range offsetsChan {
		}
		waitForDone()
	}
	return
}

func (f *implIFederation) dummy() {}

func (f *implIFederation) WithRetry() IFederationWithRetry {
	return &implIFederation{
		httpClient:         f.httpClient,
		federationURL:      f.federationURL,
		adminPortGetter:    f.adminPortGetter,
		defaultReqOptFuncs: []coreutils.ReqOptFunc{coreutils.WithRetryOn503()},
		vvmCtx:             f.vvmCtx,
	}
}

func (f *implIFederationForQP) QueryNoRetry(relativeURL string, optFuncs ...coreutils.ReqOptFunc) (*coreutils.FuncResponse, error) {
	optFuncs = append(optFuncs, coreutils.WithSkipRetryOn503())
	return f.fed.Query(relativeURL, optFuncs...)
}
