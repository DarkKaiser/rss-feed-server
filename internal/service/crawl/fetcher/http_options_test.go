package fetcher

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHTTPOptions_Table tests all functional options for HTTPFetcher using a table-driven approach.
// It verifies both the HTTPFetcher internal state and the resulting http.Transport configuration.
func TestHTTPOptions_Table(t *testing.T) {
	// Helper to get http.Transport from fetcher for verification
	transport := func(f *HTTPFetcher) *http.Transport {
		if f.client == nil || f.client.Transport == nil {
			return nil
		}
		if tr, ok := f.client.Transport.(*http.Transport); ok {
			return tr
		}
		return nil
	}

	type testCase struct {
		name        string
		options     []Option
		verify      func(t *testing.T, f *HTTPFetcher)
		expectError bool // Set to true if NewHTTPFetcher is expected to result in an init error (checked via Do)
	}

	tests := []testCase{
		// =================================================================================
		// Timeout Options
		// =================================================================================
		{
			name:    "WithTimeout - Positive",
			options: []Option{WithTimeout(10 * time.Second)},
			verify: func(t *testing.T, f *HTTPFetcher) {
				assert.Equal(t, 10*time.Second, f.client.Timeout)
			},
		},
		{
			name:    "WithTimeout - Zero (Infinite)",
			options: []Option{WithTimeout(0)},
			verify: func(t *testing.T, f *HTTPFetcher) {
				assert.Equal(t, time.Duration(0), f.client.Timeout)
			},
		},
		{
			name:    "WithTimeout - Negative (Use Default)",
			options: []Option{WithTimeout(-1)},
			verify: func(t *testing.T, f *HTTPFetcher) {
				assert.Equal(t, 30*time.Second, f.client.Timeout) // Default timeout
			},
		},
		{
			name:    "WithResponseHeaderTimeout",
			options: []Option{WithResponseHeaderTimeout(5 * time.Second)},
			verify: func(t *testing.T, f *HTTPFetcher) {
				require.NotNil(t, f.responseHeaderTimeout)
				assert.Equal(t, 5*time.Second, *f.responseHeaderTimeout)
				// Transport verification
				tr := transport(f)
				require.NotNil(t, tr)
				assert.Equal(t, 5*time.Second, tr.ResponseHeaderTimeout)
			},
		},
		{
			name:    "WithResponseHeaderTimeout - Negative (Infinite)",
			options: []Option{WithResponseHeaderTimeout(-1)},
			verify: func(t *testing.T, f *HTTPFetcher) {
				require.NotNil(t, f.responseHeaderTimeout)
				assert.Equal(t, time.Duration(0), *f.responseHeaderTimeout) // -1 is normalized to 0
				tr := transport(f)
				require.NotNil(t, tr)
				assert.Equal(t, time.Duration(0), tr.ResponseHeaderTimeout) // Result is Infinite
			},
		},
		{
			name:    "WithTLSHandshakeTimeout",
			options: []Option{WithTLSHandshakeTimeout(3 * time.Second)},
			verify: func(t *testing.T, f *HTTPFetcher) {
				require.NotNil(t, f.tlsHandshakeTimeout)
				assert.Equal(t, 3*time.Second, *f.tlsHandshakeTimeout)
				tr := transport(f)
				require.NotNil(t, tr)
				assert.Equal(t, 3*time.Second, tr.TLSHandshakeTimeout)
			},
		},
		{
			name:    "WithTLSHandshakeTimeout - Negative (Use Default)",
			options: []Option{WithTLSHandshakeTimeout(-1)},
			verify: func(t *testing.T, f *HTTPFetcher) {
				require.NotNil(t, f.tlsHandshakeTimeout)
				assert.Equal(t, 10*time.Second, *f.tlsHandshakeTimeout) // Default TLS timeout
				tr := transport(f)
				require.NotNil(t, tr)
				assert.Equal(t, 10*time.Second, tr.TLSHandshakeTimeout)
			},
		},
		{
			name:    "WithIdleConnTimeout",
			options: []Option{WithIdleConnTimeout(45 * time.Second)},
			verify: func(t *testing.T, f *HTTPFetcher) {
				require.NotNil(t, f.idleConnTimeout)
				assert.Equal(t, 45*time.Second, *f.idleConnTimeout)
				tr := transport(f)
				require.NotNil(t, tr)
				assert.Equal(t, 45*time.Second, tr.IdleConnTimeout)
			},
		},
		{
			name:    "WithIdleConnTimeout - Negative (Use Default)",
			options: []Option{WithIdleConnTimeout(-1)},
			verify: func(t *testing.T, f *HTTPFetcher) {
				require.NotNil(t, f.idleConnTimeout)
				assert.Equal(t, 90*time.Second, *f.idleConnTimeout) // Default Idle timeout
				tr := transport(f)
				require.NotNil(t, tr)
				assert.Equal(t, 90*time.Second, tr.IdleConnTimeout)
			},
		},

		// =================================================================================
		// Connection Pool Options
		// =================================================================================
		{
			name:    "WithMaxIdleConns - Positive",
			options: []Option{WithMaxIdleConns(50)},
			verify: func(t *testing.T, f *HTTPFetcher) {
				require.NotNil(t, f.maxIdleConns)
				assert.Equal(t, 50, *f.maxIdleConns)
				tr := transport(f)
				require.NotNil(t, tr)
				assert.Equal(t, 50, tr.MaxIdleConns)
			},
		},
		{
			name:    "WithMaxIdleConns - Zero (Unlimited)",
			options: []Option{WithMaxIdleConns(0)},
			verify: func(t *testing.T, f *HTTPFetcher) {
				require.NotNil(t, f.maxIdleConns)
				assert.Equal(t, 0, *f.maxIdleConns)
				tr := transport(f)
				require.NotNil(t, tr)
				assert.Equal(t, 0, tr.MaxIdleConns) // 0 means no limit in http.Transport
			},
		},
		{
			name:    "WithMaxIdleConns - Negative (Use Default)",
			options: []Option{WithMaxIdleConns(-1)},
			verify: func(t *testing.T, f *HTTPFetcher) {
				require.NotNil(t, f.maxIdleConns)
				assert.Equal(t, 100, *f.maxIdleConns) // Default max idle conns
				tr := transport(f)
				require.NotNil(t, tr)
				assert.Equal(t, 100, tr.MaxIdleConns)
			},
		},
		{
			name:    "WithMaxIdleConnsPerHost - Positive",
			options: []Option{WithMaxIdleConnsPerHost(5)},
			verify: func(t *testing.T, f *HTTPFetcher) {
				require.NotNil(t, f.maxIdleConnsPerHost)
				assert.Equal(t, 5, *f.maxIdleConnsPerHost)
				tr := transport(f)
				require.NotNil(t, tr)
				assert.Equal(t, 5, tr.MaxIdleConnsPerHost)
			},
		},
		{
			name:    "WithMaxIdleConnsPerHost - Zero (Unlimited)",
			options: []Option{WithMaxIdleConnsPerHost(0)},
			verify: func(t *testing.T, f *HTTPFetcher) {
				require.NotNil(t, f.maxIdleConnsPerHost)
				assert.Equal(t, 0, *f.maxIdleConnsPerHost)
				tr := transport(f)
				require.NotNil(t, tr)
				assert.Equal(t, 0, tr.MaxIdleConnsPerHost) // http.Transport default is 2, but 0 here means "use default" if nil, but explicit 0 means 0. Wait, http.Transport doc says "if 0, use DefaultMaxIdleConnsPerHost (2)".
				// Our WithMaxIdleConnsPerHost(0) sets pointer to 0. In configureTransport, if pointer is not nil, we use it. So transport.MaxIdleConnsPerHost becomes 0.
				// In http.Transport, MaxIdleConnsPerHost=0 means default 2. Correct.
			},
		},
		{
			name:    "WithMaxConnsPerHost - Positive",
			options: []Option{WithMaxConnsPerHost(10)},
			verify: func(t *testing.T, f *HTTPFetcher) {
				require.NotNil(t, f.maxConnsPerHost)
				assert.Equal(t, 10, *f.maxConnsPerHost)
				tr := transport(f)
				require.NotNil(t, tr)
				assert.Equal(t, 10, tr.MaxConnsPerHost)
			},
		},
		{
			name:    "WithMaxConnsPerHost - Zero (Unlimited)",
			options: []Option{WithMaxConnsPerHost(0)},
			verify: func(t *testing.T, f *HTTPFetcher) {
				require.NotNil(t, f.maxConnsPerHost)
				assert.Equal(t, 0, *f.maxConnsPerHost)
				tr := transport(f)
				require.NotNil(t, tr)
				assert.Equal(t, 0, tr.MaxConnsPerHost)
			},
		},

		// =================================================================================
		// Proxy Option
		// =================================================================================
		{
			name:    "WithProxy - Valid URL",
			options: []Option{WithProxy("http://proxy.example.com:8080")},
			verify: func(t *testing.T, f *HTTPFetcher) {
				require.NotNil(t, f.proxyURL)
				assert.Equal(t, "http://proxy.example.com:8080", *f.proxyURL)
				tr := transport(f)
				require.NotNil(t, tr)

				// Verify Proxy function works
				req, _ := http.NewRequest("GET", "http://example.com", nil)
				proxyURL, err := tr.Proxy(req)
				assert.NoError(t, err)
				assert.NotNil(t, proxyURL)
				assert.Equal(t, "http://proxy.example.com:8080", proxyURL.String())
			},
		},
		{
			name:    "WithProxy - Empty (No Proxy)",
			options: []Option{WithProxy("")},
			verify: func(t *testing.T, f *HTTPFetcher) {
				require.NotNil(t, f.proxyURL)
				assert.Equal(t, "", *f.proxyURL)
				tr := transport(f)
				require.NotNil(t, tr)
				// Default transport proxy is usually nil or FromEnvironment
			},
		},
		{
			name:    "WithProxy - Invalid URL (Runtime Error)",
			options: []Option{WithProxy(" ://invalid")},
			verify: func(t *testing.T, f *HTTPFetcher) {
				require.NotNil(t, f.proxyURL)
				assert.Equal(t, " ://invalid", *f.proxyURL)
				// Invalid proxy URL causes configureTransport to fail and set f.initErr
				assert.Error(t, f.initErr)
				req, _ := http.NewRequest("GET", "http://example.com", nil)
				_, err := f.Do(req)
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "프록시 URL의 형식이 유효하지 않습니다")
			},
			expectError: true,
		},

		// =================================================================================
		// Client Behavior Options
		// =================================================================================
		{
			name:    "WithUserAgent - Custom",
			options: []Option{WithUserAgent("MyBot/1.0")},
			verify: func(t *testing.T, f *HTTPFetcher) {
				assert.Equal(t, "MyBot/1.0", f.defaultUA)
			},
		},
		{
			name:    "WithUserAgent - Empty",
			options: []Option{WithUserAgent("")},
			verify: func(t *testing.T, f *HTTPFetcher) {
				assert.Equal(t, "", f.defaultUA)
			},
		},
		{
			name:    "WithMaxRedirects - Positive",
			options: []Option{WithMaxRedirects(5)},
			verify: func(t *testing.T, f *HTTPFetcher) {
				assert.NotNil(t, f.client.CheckRedirect)

				// Simulate redirect check
				req, _ := http.NewRequest("GET", "http://example.com", nil)
				via := make([]*http.Request, 5) // 5 prior redirects
				err := f.client.CheckRedirect(req, via)
				assert.ErrorIs(t, err, http.ErrUseLastResponse, "Should stop after 5 redirects")

				viaLessThanMax := make([]*http.Request, 4)
				err = f.client.CheckRedirect(req, viaLessThanMax)
				assert.NoError(t, err)
			},
		},
		{
			name:    "WithMaxRedirects - Zero (No Redirects)",
			options: []Option{WithMaxRedirects(0)},
			verify: func(t *testing.T, f *HTTPFetcher) {
				assert.NotNil(t, f.client.CheckRedirect)
				req, _ := http.NewRequest("GET", "http://example.com", nil)
				via := make([]*http.Request, 1) // Even 1 redirect should fail
				err := f.client.CheckRedirect(req, via)
				assert.ErrorIs(t, err, http.ErrUseLastResponse)
			},
		},
		{
			name:    "WithMaxRedirects - Negative (Use Default)",
			options: []Option{WithMaxRedirects(-1)},
			verify: func(t *testing.T, f *HTTPFetcher) {
				assert.NotNil(t, f.client.CheckRedirect)
				// Default is 10
				req, _ := http.NewRequest("GET", "http://example.com", nil)
				via := make([]*http.Request, 10)
				err := f.client.CheckRedirect(req, via)
				assert.ErrorIs(t, err, http.ErrUseLastResponse)

				viaLessThanMax := make([]*http.Request, 9)
				err = f.client.CheckRedirect(req, viaLessThanMax)
				assert.NoError(t, err)
			},
		},
		{
			name:    "WithCookieJar",
			options: []Option{WithCookieJar(&mockCookieJar{})},
			verify: func(t *testing.T, f *HTTPFetcher) {
				assert.NotNil(t, f.client.Jar)
				_, ok := f.client.Jar.(*mockCookieJar)
				assert.True(t, ok)
			},
		},
		{
			name:    "WithCookieJar - Nil",
			options: []Option{WithCookieJar(nil)},
			verify: func(t *testing.T, f *HTTPFetcher) {
				assert.Nil(t, f.client.Jar)
			},
		},

		// =================================================================================
		// Transport Control Options
		// =================================================================================
		{
			name:    "WithDisableTransportCaching",
			options: []Option{WithDisableTransportCaching(true)},
			verify: func(t *testing.T, f *HTTPFetcher) {
				assert.True(t, f.disableTransportCaching)
			},
		},
		{
			name: "WithTransport - Custom Transport",
			options: []Option{
				WithTransport(&http.Transport{DisableKeepAlives: true}),
			},
			verify: func(t *testing.T, f *HTTPFetcher) {
				tr := transport(f)
				require.NotNil(t, tr)
				assert.True(t, tr.DisableKeepAlives)
				// When WithTransport is used, we treat it as an isolated/custom transport setup.
				// Thus, transport cache should be disabled to prevent sharing/leaking config.
				assert.True(t, f.disableTransportCaching, "Resource Leak Protection: Cloned transport MUST be isolated (disableTransportCaching=true)")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := NewHTTPFetcher(tc.options...)
			tc.verify(t, f)
		})
	}
}

// mockCookieJar for testing WithCookieJar
type mockCookieJar struct{}

func (m *mockCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {}
func (m *mockCookieJar) Cookies(u *url.URL) []*http.Cookie             { return nil }

// TestHTTPOptions_Interaction verifies interactions between multiple options.
func TestHTTPOptions_Interaction(t *testing.T) {
	t.Run("WithProxy overrides Transport Cache", func(t *testing.T) {
		// When Proxy is set, it should use a cached transport for that proxy, OR create a new one.
		// It should NOT use the default global transport.
		f := NewHTTPFetcher(WithProxy("http://proxy.local:8080"))
		tr := f.transport() // Public method to get transport/client.Transport

		assert.NotNil(t, tr)
		// It should be a *http.Transport
		httpTr, ok := tr.(*http.Transport)
		require.True(t, ok)

		// Verify proxy is set
		req, _ := http.NewRequest("GET", "http://example.com", nil)
		proxyURL, err := httpTr.Proxy(req)
		assert.NoError(t, err)
		assert.Equal(t, "http://proxy.local:8080", proxyURL.String())
	})

	t.Run("WithTransport vs Other Options", func(t *testing.T) {
		// Even if WithTransport is used, other transport options (like WithMaxIdleConns) should be applied
		// if they are explicitly set. The fetcher logic (setupCustomTransport) clones the transport
		// and applies the settings.
		customTr := &http.Transport{}
		f := NewHTTPFetcher(
			WithTransport(customTr),
			WithMaxIdleConns(999),
		)

		currentTr := f.transport()
		// It should be a NEW object (cloned) because we applied WithMaxIdleConns
		assert.NotEqual(t, customTr, currentTr, "Spec: WithTransport + Options should trigger cloning")

		httpTr := currentTr.(*http.Transport)
		assert.Equal(t, 999, httpTr.MaxIdleConns, "Options should be APPLIED even when WithTransport is used")

		// Verify disableTransportCaching is set because cloning occurred
		assert.True(t, f.disableTransportCaching, "Resource Leak Protection: disableTransportCaching must be true after cloning")
	})

	t.Run("WithProxy with Custom Non-HTTP Transport", func(t *testing.T) {
		// Verify behavior when setting Proxy on a custom Transport that is NOT *http.Transport.
		// It should return an error because we can't configure it.
		customTr := &mockRoundTripper{}
		f := NewHTTPFetcher(
			WithTransport(customTr),
			WithProxy("http://proxy.local:8080"),
		)

		req, _ := http.NewRequest("GET", "http://example.com", nil)
		_, err := f.Do(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "지원하지 않는 Transport 형식입니다")
	})
}

// mockRoundTripper for testing custom transport
type mockRoundTripper struct{}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, nil
}
