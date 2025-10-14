/*
 * Copyright (c) 2025-present unTill Software Development Group B.V.
 * @author Denis Gribanov
 */

package httpu

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// ExampleWithRetryOnStatusCode demonstrates how to configure automatic retries
// for specific HTTP status codes using the new WithRetryOnStatusCode function.
func ExampleWithRetryOnStatusCode() {
	httpClient, cleanup := NewIHTTPClient()
	defer cleanup()

	// Example 1: Retry on 503 Service Unavailable with 10 second max duration
	_, err := httpClient.Req(
		context.Background(),
		"https://example.com/api/data",
		"",
		WithRetryOnStatusCode(http.StatusServiceUnavailable, 10*time.Second),
	)
	if err != nil {
		fmt.Printf("Request failed: %v\n", err)
	}

	// Example 2: Retry on multiple status codes
	_, err = httpClient.Req(
		context.Background(),
		"https://example.com/api/data",
		"",
		WithRetryOnStatusCode(http.StatusBadGateway, 5*time.Second),          // 502
		WithRetryOnStatusCode(http.StatusServiceUnavailable, 10*time.Second), // 503
		WithRetryOnStatusCode(http.StatusGatewayTimeout, 5*time.Second),      // 504
		WithRetryOnStatusCode(http.StatusTooManyRequests, 30*time.Second),    // 429 (will respect Retry-After header)
	)
	if err != nil {
		fmt.Printf("Request failed: %v\n", err)
	}

	// Example 3: Retry indefinitely (until context is cancelled)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = httpClient.Req(
		ctx,
		"https://example.com/api/data",
		"",
		WithRetryOnStatusCode(http.StatusServiceUnavailable, 0), // 0 means no time limit
	)
	if err != nil {
		fmt.Printf("Request failed: %v\n", err)
	}

	// Example 4: 429 with Retry-After header handling
	// When the server returns 429 with a Retry-After header, the client will:
	// - Parse the Retry-After header (supports both seconds and HTTP date formats)
	// - Wait for the specified duration before retrying (ignoring exponential backoff)
	// - Respect context cancellation during the wait
	// - This is handled automatically within the WithRetryOnStatusCode function
	_, err = httpClient.Req(
		context.Background(),
		"https://api.example.com/rate-limited-endpoint",
		"",
		WithRetryOnStatusCode(http.StatusTooManyRequests, 60*time.Second),
	)
	if err != nil {
		fmt.Printf("Request failed: %v\n", err)
	}
}
