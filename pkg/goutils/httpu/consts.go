/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package httpu

import (
	"errors"
	"net"
	"net/http"
	"syscall"
	"time"
)

const (
	Authorization                                = "Authorization"
	ContentType                                  = "Content-Type"
	ContentDisposition                           = "Content-Disposition"
	Accept                                       = "Accept"
	Origin                                       = "Origin"
	RetryAfter                                   = "Retry-After"
	ContentType_ApplicationJSON                  = "application/json"
	ContentType_ApplicationXBinary               = "application/x-binary"
	ContentType_TextPlain                        = "text/plain"
	ContentType_TextHTML                         = "text/html"
	ContentType_MultipartFormData                = "multipart/form-data"
	BearerPrefix                                 = "Bearer "
	WSAECONNRESET                  syscall.Errno = 10054
	WSAECONNREFUSED                syscall.Errno = 10061
	maxHTTPRequestTimeout                        = time.Hour
	httpBaseRetryDelay                           = 20 * time.Millisecond
	httpMaxRetryDelay                            = 1 * time.Second
	localhostDynamic                             = "127.0.0.1:0"
)

var (
	constDefaultOpts = []ReqOptFunc{
		WithRetryErrorMatcher(func(err error) bool {
			// https://github.com/voedger/voedger/issues/1694
			return IsWSAEError(err, WSAECONNREFUSED)
		}),
		WithRetryPolicyOnStatus(http.StatusBadGateway, 30*time.Second, nil),
		WithRetryPolicyOnStatus(http.StatusServiceUnavailable, 30*time.Second, nil),
		WithRetryPolicyOnStatus(http.StatusGatewayTimeout, 30*time.Second, nil),
		WithRetryPolicyOnStatus(http.StatusTooManyRequests, 30*time.Second, func(resp *http.Response, opts IReqOpts) bool {
			retryAfterStr := resp.Header.Get(RetryAfter)
			if len(retryAfterStr) == 0 {
				return true
			}
			тут спать и еще за контекстом следить
			if resp.Header.Get(RetryAfter) == "0" {
				return false
			}
			return true
		}),

		WithRetryErrorMatcher(func(err error) bool {
			return errors.Is(err, errRetry)
		}),
	}
	LocalhostIP = net.IPv4(127, 0, 0, 1)
)
