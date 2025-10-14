/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package httpu

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseRetryAfterHeader(t *testing.T) {
	t.Run("seconds format", func(t *testing.T) {
		resp := &http.Response{
			Header: http.Header{
				"Retry-After": []string{"120"},
			},
		}
		duration := parseRetryAfterHeader(resp)
		require.Equal(t, 120*time.Second, duration)
	})

	t.Run("HTTP date format", func(t *testing.T) {
		futureTime := time.Now().Add(5 * time.Minute)
		resp := &http.Response{
			Header: http.Header{
				"Retry-After": []string{futureTime.UTC().Format(http.TimeFormat)},
			},
		}
		duration := parseRetryAfterHeader(resp)
		require.Greater(t, duration, 4*time.Minute)
		require.Less(t, duration, 6*time.Minute)
	})

	t.Run("past HTTP date", func(t *testing.T) {
		pastTime := time.Now().Add(-5 * time.Minute)
		resp := &http.Response{
			Header: http.Header{
				"Retry-After": []string{pastTime.UTC().Format(http.TimeFormat)},
			},
		}
		duration := parseRetryAfterHeader(resp)
		require.Equal(t, time.Duration(0), duration)
	})

	t.Run("invalid format", func(t *testing.T) {
		resp := &http.Response{
			Header: http.Header{
				"Retry-After": []string{"invalid"},
			},
		}
		duration := parseRetryAfterHeader(resp)
		require.Equal(t, time.Duration(0), duration)
	})

	t.Run("missing header", func(t *testing.T) {
		resp := &http.Response{
			Header: http.Header{},
		}
		duration := parseRetryAfterHeader(resp)
		require.Equal(t, time.Duration(0), duration)
	})

	t.Run("zero seconds", func(t *testing.T) {
		resp := &http.Response{
			Header: http.Header{
				"Retry-After": []string{"0"},
			},
		}
		duration := parseRetryAfterHeader(resp)
		require.Equal(t, time.Duration(0), duration)
	})

	t.Run("negative seconds", func(t *testing.T) {
		resp := &http.Response{
			Header: http.Header{
				"Retry-After": []string{"-10"},
			},
		}
		duration := parseRetryAfterHeader(resp)
		require.Equal(t, time.Duration(0), duration)
	})
}

func TestReqOptsRetryMethods(t *testing.T) {
	t.Run("getRetryConfigForStatusCode", func(t *testing.T) {
		opts := &reqOpts{
			statusCodeRetryConfigs: []StatusCodeRetryConfig{
				{StatusCode: 502, MaxRetryDuration: 5 * time.Second},
				{StatusCode: 503, MaxRetryDuration: 10 * time.Second},
			},
		}

		config := opts.getRetryConfigForStatusCode(502)
		require.NotNil(t, config)
		require.Equal(t, 502, config.StatusCode)
		require.Equal(t, 5*time.Second, config.MaxRetryDuration)

		config = opts.getRetryConfigForStatusCode(503)
		require.NotNil(t, config)
		require.Equal(t, 503, config.StatusCode)
		require.Equal(t, 10*time.Second, config.MaxRetryDuration)

		config = opts.getRetryConfigForStatusCode(404)
		require.Nil(t, config)
	})

	t.Run("shouldRetryOnStatusCode", func(t *testing.T) {
		opts := &reqOpts{
			expectedHTTPCodes: []int{200, 404},
			statusCodeRetryConfigs: []StatusCodeRetryConfig{
				{StatusCode: 502, MaxRetryDuration: 5 * time.Second},
				{StatusCode: 503, MaxRetryDuration: 10 * time.Second},
			},
		}

		// Should retry on configured status codes that are not expected
		require.True(t, opts.shouldRetryOnStatusCode(502))
		require.True(t, opts.shouldRetryOnStatusCode(503))

		// Should not retry on expected status codes
		require.False(t, opts.shouldRetryOnStatusCode(200))
		require.False(t, opts.shouldRetryOnStatusCode(404))

		// Should not retry on unconfigured status codes
		require.False(t, opts.shouldRetryOnStatusCode(500))
	})
}
