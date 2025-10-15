# httpu

HTTP client utilities with automatic retry handling and configurable
request options.

## Problem

Making reliable HTTP requests requires extensive boilerplate for retry
logic, error handling, timeouts, and status code validation.

<details>
<summary>Without httpu</summary>

```go
// Verbose, error-prone HTTP client setup with manual retry logic
func makeRequest(url, body string) (*http.Response, error) {
    client := &http.Client{
        Timeout: time.Hour, // Manual timeout setup
        Transport: &http.Transport{
            // Manual connection configuration
            DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
                dialer := net.Dialer{}
                conn, err := dialer.DialContext(ctx, network, addr)
                if err != nil {
                    return nil, err
                }
                // Manual linger setup - easy to forget
                return conn, conn.(*net.TCPConn).SetLinger(0)
            },
        },
    }

    var resp *http.Response
    var err error

    // Manual retry logic - complex and error-prone
    for attempt := 0; attempt < 3; attempt++ {
        req, err := http.NewRequest("POST", url, strings.NewReader(body))
        if err != nil {
            return nil, err
        }

        // Manual header setup
        req.Header.Set("Content-Type", "application/json")
        req.Header.Set("Authorization", "Bearer "+token) // boilerplate here

        resp, err = client.Do(req)
        if err != nil {
            // Manual error classification - which errors to retry?
            if strings.Contains(err.Error(), "connection refused") {
                time.Sleep(time.Duration(attempt) * 100 * time.Millisecond)
                continue
            }
            return nil, err
        }

        // Manual status code handling - common mistakes here
        if resp.StatusCode == 429 || resp.StatusCode == 502 ||
           resp.StatusCode == 503 || resp.StatusCode == 504 {
            resp.Body.Close() // easy to forget cleanup
            time.Sleep(time.Duration(attempt) * 100 * time.Millisecond)
            continue
        }

        break
    }

    // Manual response body reading and validation
    if resp.StatusCode != 200 && resp.StatusCode != 201 {
        body, _ := io.ReadAll(resp.Body)
        resp.Body.Close()
        return nil, fmt.Errorf("unexpected status %d: %s",
            resp.StatusCode, string(body))
    }

    return resp, nil
}
```

</details>

<details>
<summary>With httpu</summary>

```go
import "github.com/voedger/voedger/pkg/goutils/httpu"

// Clean, simple HTTP requests with automatic retry and error handling
httpClient, cleanup := httpu.NewIHTTPClient()
defer cleanup()

resp, err := httpClient.Req(
    context.Background(),
    url,
    body,
    httpu.WithMethod("POST"),
    httpu.WithHeaders("Content-Type", "application/json"),
    httpu.WithAuthorizeBy(token),
    httpu.WithExpectedCode(201),
)
```

</details>

## Features

- **[Automatic retry](consts.go#L38)** - Built-in retry
  for connection errors and HTTP status codes
  - [Default retry policies: consts.go#L38](consts.go#L38)
  - [Retry configuration: impl_opts.go#L95](impl_opts.go#L95)
  - [Exponential backoff with jitter: impl.go#L112](impl.go#L112)
  - [Retry-After header support: impl.go#L139](impl.go#L139)
  - [Custom error matchers: impl_opts.go#L154](impl_opts.go#L154)

- **[Request configuration](impl_opts.go#L17)** -
  Functional options for headers, auth, and validation
  - [HTTP method options: impl_opts.go#L140](impl_opts.go#L140)
  - [Header and cookie management: impl_opts.go#L66](impl_opts.go#L66)
  - [Authentication helpers: impl_opts.go#L81](impl_opts.go#L81)
  - [Status code expectations: impl_opts.go#L74](impl_opts.go#L74)
  - [Response handling modes: impl_opts.go#L17](impl_opts.go#L17)

- **[Connection management](provide.go#L15)** -
  Optimized transport with proper cleanup
  - [TCP linger configuration: provide.go#L24](provide.go#L24)
  - [Connection cleanup: provide.go#L31](provide.go#L31)
  - [Request timeout handling: impl.go#L109](impl.go#L109)

- **[Response processing](impl.go#L162)** - Automatic
  body reading and status validation
  - [Body reading utilities: utils.go#L19](utils.go#L19)
  - [Status code validation: impl.go#L162](impl.go#L162)
  - [Error wrapping: impl.go#L181](impl.go#L181)

## Use

See [example](example_test.go)
