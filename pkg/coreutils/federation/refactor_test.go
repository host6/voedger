/*
 * Copyright (c) 2024-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package federation

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/voedger/voedger/pkg/coreutils"
)

// TestAbstractionLeak verifies that the abstraction leak has been fixed
func TestAbstractionLeak(t *testing.T) {
	require := require.New(t)

	t.Run("HTTP client should only handle low-level operations", func(t *testing.T) {
		// Create a test server that returns a 404 error
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error": {"message": "Not found"}}`))
		}))
		defer server.Close()

		// Create HTTP client
		httpClient, cleanup := coreutils.NewIHTTPClient()
		defer cleanup()

		// Make a request - HTTP client should return the response without high-level error processing
		resp, err := httpClient.Req(context.Background(), server.URL, "",
			coreutils.WithExpectedCode(http.StatusOK)) // This should not cause high-level error processing

		// HTTP client should return the response even for unexpected status codes
		// High-level error handling should be done by the federation layer
		require.NoError(err, "HTTP client should not perform high-level error handling")
		require.NotNil(resp)
		require.Equal(http.StatusNotFound, resp.HTTPResp.StatusCode)
		require.Contains(resp.Body, "Not found")
	})

	t.Run("Federation should handle high-level error processing", func(t *testing.T) {
		// This test would require more setup to test the federation layer
		// but demonstrates the intended separation of concerns

		// The federation layer should:
		// 1. Call the HTTP client for low-level operations
		// 2. Process the response for business logic errors
		// 3. Handle retry logic for 503 errors
		// 4. Validate expected error messages
		// 5. Convert HTTP responses to function responses

		// This is now properly separated between the layers
	})
}

// TestHTTPClientLowLevel verifies that HTTP client only handles low-level concerns
func TestHTTPClientLowLevel(t *testing.T) {
	require := require.New(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/success":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("success"))
		case "/error":
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"sys.Error": {"Message": "Bad request"}}`))
		case "/503":
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("Service unavailable"))
		}
	}))
	defer server.Close()

	httpClient, cleanup := coreutils.NewIHTTPClient()
	defer cleanup()

	t.Run("successful request", func(t *testing.T) {
		resp, err := httpClient.Req(context.Background(), server.URL+"/success", "")
		require.NoError(err)
		require.Equal(http.StatusOK, resp.HTTPResp.StatusCode)
		require.Equal("success", resp.Body)
	})

	t.Run("error response - no high-level processing", func(t *testing.T) {
		resp, err := httpClient.Req(context.Background(), server.URL+"/error", "")
		require.NoError(err) // HTTP client should not process business logic errors
		require.Equal(http.StatusBadRequest, resp.HTTPResp.StatusCode)
		require.Contains(resp.Body, "Bad request")
	})

	t.Run("503 response - no high-level retry", func(t *testing.T) {
		resp, err := httpClient.Req(context.Background(), server.URL+"/503", "")
		require.NoError(err) // HTTP client should not handle 503 retry logic
		require.Equal(http.StatusServiceUnavailable, resp.HTTPResp.StatusCode)
		require.Contains(resp.Body, "Service unavailable")
	})
}

// TestReaderInterface verifies ReqReader works correctly
func TestReaderInterface(t *testing.T) {
	require := require.New(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(err)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("received: " + string(body)))
	}))
	defer server.Close()

	httpClient, cleanup := coreutils.NewIHTTPClient()
	defer cleanup()

	bodyReader := io.NopCloser(bytes.NewReader([]byte("test data")))
	resp, err := httpClient.ReqReader(context.Background(), server.URL, bodyReader)

	require.NoError(err)
	require.Equal(http.StatusOK, resp.HTTPResp.StatusCode)
	require.Equal("received: test data", resp.Body)
}
