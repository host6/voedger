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
		WithRetryOnError(func(err error) bool {
			// https://github.com/voedger/voedger/issues/1694
			return IsWSAEError(err, WSAECONNREFUSED)
		}),
		WithRetryOnError(func(err error) bool {
			return errors.Is(err, errRetry)
		}),
		WithRetryOnStatus(http.StatusTooManyRequests, 30*time.Second, WithRespectRetryAfter()),
		WithRetryOnStatus(http.StatusBadGateway, 30*time.Second, nil),
		WithRetryOnStatus(http.StatusServiceUnavailable, 30*time.Second, nil),
		WithRetryOnStatus(http.StatusGatewayTimeout, 30*time.Second, nil),
	}
	LocalhostIP = net.IPv4(127, 0, 0, 1)
)
