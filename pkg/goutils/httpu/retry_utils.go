/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package httpu

import (
	"net/http"
	"strconv"
	"time"
)

// parseRetryAfterHeader parses the Retry-After header value.
// It supports both seconds format (e.g., "120") and HTTP date format (e.g., "Wed, 21 Oct 2015 07:28:00 GMT").
// Returns the duration to wait, or 0 if the header is invalid or not present.
func parseRetryAfterHeader(resp *http.Response) time.Duration {
	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter == "" {
		return 0
	}

	// Try parsing as seconds first
	if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}

	// Try parsing as HTTP date
	if retryTime, err := http.ParseTime(retryAfter); err == nil {
		duration := time.Until(retryTime)
		if duration > 0 {
			return duration
		}
	}

	// Invalid or past date
	return 0
}

// getRetryConfigForStatusCode returns the retry configuration for a given status code.
// Returns nil if no configuration is found for the status code.
func (opts *reqOpts) getRetryConfigForStatusCode(statusCode int) *StatusCodeRetryConfig {
	for i := range opts.statusCodeRetryConfigs {
		if opts.statusCodeRetryConfigs[i].StatusCode == statusCode {
			return &opts.statusCodeRetryConfigs[i]
		}
	}
	return nil
}

// shouldRetryOnStatusCode checks if the given status code should be retried.
// It returns true if:
// 1. The status code is not in expectedHTTPCodes
// 2. There is a retry configuration for this status code
func (opts *reqOpts) shouldRetryOnStatusCode(statusCode int) bool {
	// Don't retry if the status code is expected
	for _, expectedCode := range opts.expectedHTTPCodes {
		if expectedCode == statusCode {
			return false
		}
	}

	// Check if we have a retry configuration for this status code
	return opts.getRetryConfigForStatusCode(statusCode) != nil
}
