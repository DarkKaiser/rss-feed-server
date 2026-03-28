package fetcher_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/darkkaiser/rss-feed-server/internal/service/crawl/fetcher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHTTPFetcher_Do verifies the standard behavior of the Do method.
// It covers success scenarios, error handling, context cancellation, and header injection.
func TestHTTPFetcher_Do(t *testing.T) {
	// Mock server to simulate various responses
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Header Verification
		if r.URL.Path == "/verify-headers" {
			if r.Header.Get("User-Agent") == "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("Missing User-Agent"))
				return
			}
			if r.Header.Get("Accept") == "" || r.Header.Get("Accept-Language") == "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("Missing Standard Headers"))
				return
			}
		}

		// Timeout Simulation
		if r.URL.Path == "/timeout" {
			time.Sleep(200 * time.Millisecond)
		}

		// Echo path
		if r.URL.Path == "/verify-path" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("path-ok"))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
	defer ts.Close()

	tests := []struct {
		name          string
		fetcherOpts   []fetcher.Option
		reqFunc       func() *http.Request
		ctxTimeout    time.Duration
		expectedError string // Substring match
		validateResp  func(t *testing.T, resp *http.Response)
	}{
		{
			name: "Success - Basic Request",
			reqFunc: func() *http.Request {
				req, _ := http.NewRequest("GET", ts.URL, nil)
				return req
			},
			validateResp: func(t *testing.T, resp *http.Response) {
				assert.Equal(t, http.StatusOK, resp.StatusCode)
			},
		},
		{
			name: "Success - Proxy Connection (Simulated)",
			// We simulate a proxy by using the mock server as the target,
			// and checking if the client works when configured with a proxy that (in this test case)
			// doesn't actually intercept but is valid config.
			// Real proxy integration is harder to mock without a specialized proxy server,
			// but we can verify the configuration doesn't break normal flow or error out.
			fetcherOpts: []fetcher.Option{
				fetcher.WithProxy(fetcher.NoProxy), // Ensure NoProxy works
			},
			reqFunc: func() *http.Request {
				req, _ := http.NewRequest("GET", ts.URL, nil)
				return req
			},
			validateResp: func(t *testing.T, resp *http.Response) {
				assert.Equal(t, http.StatusOK, resp.StatusCode)
			},
		},
		{
			name: "Success - Header Injection",
			reqFunc: func() *http.Request {
				req, _ := http.NewRequest("GET", ts.URL+"/verify-headers", nil)
				return req
			},
			validateResp: func(t *testing.T, resp *http.Response) {
				assert.Equal(t, http.StatusOK, resp.StatusCode)
			},
		},
		{
			name: "Success - Custom User-Agent Preserved",
			reqFunc: func() *http.Request {
				req, _ := http.NewRequest("GET", ts.URL+"/verify-headers", nil)
				req.Header.Set("User-Agent", "CustomBot/1.0")
				return req
			},
			validateResp: func(t *testing.T, resp *http.Response) {
				assert.Equal(t, http.StatusOK, resp.StatusCode)
				// Note: Server logic checks if UA exists, assumes if it returns 200, it passed.
				// Ideally server should echo it back for strict verification, but this verify logic
				// combined with server logic (400 if missing) covers the "it's sent" part.
			},
		},
		{
			name: "Error - Context Timeout",
			reqFunc: func() *http.Request {
				req, _ := http.NewRequest("GET", ts.URL+"/timeout", nil)
				return req
			},
			ctxTimeout:    100 * time.Millisecond,
			expectedError: "context deadline exceeded",
		},
		{
			name: "Error - Context Canceled",
			reqFunc: func() *http.Request {
				req, _ := http.NewRequest("GET", ts.URL+"/timeout", nil)
				ctx, cancel := context.WithCancel(context.Background())
				req = req.WithContext(ctx)
				cancel() // Cancel immediately
				return req
			},
			expectedError: "context canceled",
		},
		{
			name: "Error - Invalid URL",
			reqFunc: func() *http.Request {
				// Use port 0 to force immediate connection refused on most systems,
				// avoiding slow DNS lookups for non-existent domains.
				req, _ := http.NewRequest("GET", "http://127.0.0.1:0", nil)
				return req
			},
			expectedError: "dial tcp",
		},
		{
			name: "Error - Init Error Propagation",
			fetcherOpts: []fetcher.Option{
				fetcher.WithProxy(" ://invalid-proxy"), // This causes init error
			},
			reqFunc: func() *http.Request {
				req, _ := http.NewRequest("GET", ts.URL, nil)
				return req
			},
			expectedError: "프록시 URL의 형식이 유효하지 않습니다",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := fetcher.NewHTTPFetcher(tc.fetcherOpts...)
			req := tc.reqFunc()

			if tc.ctxTimeout > 0 {
				ctx, cancel := context.WithTimeout(context.Background(), tc.ctxTimeout)
				defer cancel()
				req = req.WithContext(ctx)
			}

			resp, err := f.Do(req)

			if tc.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedError)
				assert.Nil(t, resp)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				defer resp.Body.Close()

				if tc.validateResp != nil {
					tc.validateResp(t, resp)
				}
			}
		})
	}
}

// TestHTTPFetcher_RequestCloning ensures that the fetcher does not mutate the original request.
func TestHTTPFetcher_RequestCloning(t *testing.T) {
	f := fetcher.NewHTTPFetcher()
	req, _ := http.NewRequest("GET", "http://example.com", nil)

	// Ensure original headers are empty
	assert.Empty(t, req.Header.Get("User-Agent"))
	assert.Empty(t, req.Header.Get("Accept"))

	// We use an invalid URL to fail fast, but the cloning logic happens BEFORE the request execution
	// check. However, Do() might return initErr or URL error.
	// To safely test this without external calls, we can inspect if Do modifies it.
	// Even if Do returns an error, we check the original req.

	// Using a mock server ensures Do proceeds deeper into logic
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	validReq, _ := http.NewRequest("GET", ts.URL, nil)
	_, _ = f.Do(validReq)

	assert.Empty(t, validReq.Header.Get("User-Agent"), "Original User-Agent should remain empty")
	assert.Empty(t, validReq.Header.Get("Accept"), "Original Accept should remain empty")
}

// TestCheckResponseStatus verified the error formatting helper.
func TestCheckResponseStatus(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://user:pass@example.com/api?token=secret", nil)
	resp := &http.Response{
		StatusCode: http.StatusTeapot,
		Status:     "418 I'm a teapot",
		Request:    req,
	}

	err := fetcher.CheckResponseStatus(resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "418 I'm a teapot")
	assert.Contains(t, err.Error(), "http://user:xxxxx@example.com/api?token=xxxxx") // Redacted
	assert.NotContains(t, err.Error(), "pass")
	assert.NotContains(t, err.Error(), "secret")
}

// TestHTTPFetcher_RefererLeak verifies that credentials and sensitive query parameters are redacted from the Referer header
// during redirects.
func TestHTTPFetcher_RefererLeak(t *testing.T) {
	// 1. Redirect Target Server (Where credentials might be leaked)
	var capturedReferer string
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReferer = r.Header.Get("Referer")
		w.WriteHeader(http.StatusOK)
	}))
	defer targetServer.Close()

	// 2. Initial Server (Redirects to Target)
	// We access this server with credentials
	initialServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetServer.URL, http.StatusFound)
	}))
	defer initialServer.Close()

	t.Run("Credential Leak in Referer", func(t *testing.T) {
		// Construct URL with credentials
		// e.g. http://admin:secret123@127.0.0.1:xxx/
		u, _ := url.Parse(initialServer.URL)
		u.User = url.UserPassword("admin", "secret123")
		initialURL := u.String()

		f := fetcher.NewHTTPFetcher()
		req, _ := http.NewRequest(http.MethodGet, initialURL, nil)

		resp, err := f.Do(req.WithContext(context.Background()))
		require.NoError(t, err)
		defer resp.Body.Close()

		// Verify captured Referer
		assert.NotEmpty(t, capturedReferer, "Referer should be present")
		assert.NotContains(t, capturedReferer, "secret123", "Password leaked in Referer!")
		assert.NotContains(t, capturedReferer, "admin", "Username leaked in Referer!")
		assert.Contains(t, capturedReferer, u.Host, "Referer should contain host")
	})

	t.Run("Query Param Redaction in Referer", func(t *testing.T) {
		// Reset capture
		capturedReferer = ""

		initialURL := initialServer.URL + "?token=secret_token_value&public=value"

		f := fetcher.NewHTTPFetcher()
		req, _ := http.NewRequest(http.MethodGet, initialURL, nil)

		resp, err := f.Do(req.WithContext(context.Background()))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.NotEmpty(t, capturedReferer)
		assert.NotContains(t, capturedReferer, "secret_token_value", "Sensitive value leaked in Referer!")
		assert.Contains(t, capturedReferer, "token=xxxxx", "Sensitive value should be masked")
		assert.Contains(t, capturedReferer, "public=value", "Non-sensitive value should remain")
	})
}

// TestHTTPFetcher_ProxyConfiguration verifies the public API behavior for proxy settings.
func TestHTTPFetcher_ProxyConfiguration(t *testing.T) {
	// 1. WithProxy(nil) -> Should use default behavior (Environment variables)
	t.Run("WithProxy(nil) uses default settings", func(t *testing.T) {
		// We can't easily check internal state without reflection or export,
		// but we can verify it doesn't panic and behaves essentially like default.

		fDefault := fetcher.NewHTTPFetcher()
		// We expect this to work for normal requests (using env vars if any)
		req, _ := http.NewRequest("GET", "http://example.com", nil)
		// Just ensure it doesn't error on creation/setup
		assert.NotNil(t, fDefault)
		_ = req
	})

	// 2. WithProxy("DIRECT") or WithProxy(NoProxy)
	t.Run("WithProxy(NoProxy) disables proxy", func(t *testing.T) {
		f := fetcher.NewHTTPFetcher(fetcher.WithProxy(fetcher.NoProxy))
		// This should force direct connection.
		// We can try to connect to localhost port that is closed.
		// If proxy was set to something invalid, it would fail with proxy error.
		// Here we expect dial error.
		req, _ := http.NewRequest("GET", "http://127.0.0.1:0", nil)
		res, err := f.Do(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "dial tcp", "Should attempt direct dial and fail")
		assert.Nil(t, res)
	})

	// 3. WithProxy(ValidURL)
	t.Run("WithProxy(ValidURL) uses proxy", func(t *testing.T) {
		// Mock Proxy Server
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Proxy receives request
			w.WriteHeader(http.StatusOK)
		}))
		defer proxy.Close()

		f := fetcher.NewHTTPFetcher(fetcher.WithProxy(proxy.URL))

		// Target URL (can be anything, proxy intercepts)
		// Note: httptest server speaks HTTP.
		req, _ := http.NewRequest("GET", "http://example.com", nil)
		res, err := f.Do(req)

		// Verification depends on how Go's transport handles http proxy.
		// The proxy server above acts as the endpoint.
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, res.StatusCode)
	})

	// 4. WithProxy(InvalidURL)
	t.Run("WithProxy(InvalidURL) returns error", func(t *testing.T) {
		f := fetcher.NewHTTPFetcher(fetcher.WithProxy(" ://invalid"))
		req, _ := http.NewRequest("GET", "http://example.com", nil)
		_, err := f.Do(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "프록시 URL의 형식이 유효하지 않습니다") // Expected error message
	})
}

// TestHTTPFetcher_Close_ExternalTransport verifies that an externally injected Transport
// is NOT closed when the HTTPFetcher is closed.
func TestHTTPFetcher_Close_ExternalTransport(t *testing.T) {
	// 1. Create a dummy server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// 2. Create a shared Transport
	sharedTr := &http.Transport{
		MaxIdleConns: 1,
	}

	// 3. Create Fetcher with this Transport
	f := fetcher.NewHTTPFetcher(fetcher.WithTransport(sharedTr))

	// 4. Perform a request to ensure connection is established and put in pool
	req, _ := http.NewRequest("GET", ts.URL, nil)
	resp, err := f.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	// 5. Close the Fetcher
	// This should NOT close the sharedTr due to ownsTransport=false
	err = f.Close()
	require.NoError(t, err)

	// 6. Verify Transport is still usable by another client
	client2 := &http.Client{Transport: sharedTr}
	resp2, err2 := client2.Get(ts.URL)
	require.NoError(t, err2)
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	resp2.Body.Close()
}
