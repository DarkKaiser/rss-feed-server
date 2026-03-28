package fetcher

import (
	"container/list"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultTransport_Settings verifies that the shared default transport matches expected defaults.
func TestDefaultTransport_Settings(t *testing.T) {
	assert.Equal(t, 100, defaultTransport.MaxIdleConns)
	assert.Equal(t, 0, defaultTransport.MaxIdleConnsPerHost) // Default value (0 means 2 in http.Transport)
	assert.Equal(t, 90*time.Second, defaultTransport.IdleConnTimeout)
	assert.Equal(t, 10*time.Second, defaultTransport.TLSHandshakeTimeout)
}

// TestHTTPFetcher_Close verifies that Close correctly cleans up isolated transports
// while leaving shared/default transports untouched.
func TestHTTPFetcher_Close(t *testing.T) {
	t.Run("Default Transport - No Op", func(t *testing.T) {
		f := NewHTTPFetcher() // uses defaultTransport
		err := f.Close()
		assert.NoError(t, err)
		// defaultTransport should remain active (cannot easily check "closed" state, but no panic)
	})

	t.Run("Shared Transport - No Op", func(t *testing.T) {
		// Uses shared cache
		f := NewHTTPFetcher(WithProxy("http://proxy.local:8080")) // creates/shares transport
		err := f.Close()
		assert.NoError(t, err)
	})

	t.Run("Isolated Transport - Closes Idle Connections", func(t *testing.T) {
		// Use DisableTransportCaching to force isolated transport
		f := NewHTTPFetcher(WithDisableTransportCaching(true))
		err := f.Close()
		assert.NoError(t, err)
		// Internal logic calls CloseIdleConnections.
		// We verify it doesn't panic.
	})
}

// TestTransportCache_Internal verifies the LRU and caching logic directly.
func TestTransportCache_Internal(t *testing.T) {
	// Reset cache for testing
	transportCacheMu.Lock()
	transportCache = make(map[transportCacheKey]*list.Element)
	transportCacheLRU.Init()
	transportCacheMu.Unlock()

	limit := 100 // defaultMaxTransportCacheSize

	t.Run("LRU Eviction", func(t *testing.T) {
		transportCacheMu.Lock()
		transportCache = make(map[transportCacheKey]*list.Element)
		transportCacheLRU.Init()
		transportCacheMu.Unlock()

		// Fill cache to limit
		for i := 0; i < limit; i++ {
			cfg := transportConfig{maxIdleConns: intPtr(i)}
			_, err := getSharedTransport(cfg)
			require.NoError(t, err)
		}

		require.Equal(t, limit, transportCacheLRU.Len())

		// Add one more -> Should evict the oldest (index 0)
		cfg := transportConfig{maxIdleConns: intPtr(limit + 1)}
		_, err := getSharedTransport(cfg)
		require.NoError(t, err)

		// Check eviction (Oldest was 0)
		// checkKey removed as it was unused and replaced by oldestKey logic below.

		oldestCfg := transportConfig{maxIdleConns: intPtr(0)}
		oldestKey := oldestCfg.ToCacheKey()

		transportCacheMu.RLock()
		_, ok := transportCache[oldestKey]
		assert.False(t, ok, "Oldest item should be evicted")
		transportCacheMu.RUnlock()
	})

	t.Run("Smart Eviction - Prefer Proxy", func(t *testing.T) {
		transportCacheMu.Lock()
		transportCache = make(map[transportCacheKey]*list.Element)
		transportCacheLRU.Init()
		transportCacheMu.Unlock()

		// Scenario:
		// 1. Fill cache with mostly direct connections (important).
		// 2. Add a few proxy connections (eviction candidates) at the END (recently used).
		// 3. Trigger eviction -> Should evict proxy even if it's recent, to protect direct connections.

		// 1. Fill with Direct connections
		for i := 0; i < limit-2; i++ {
			cfg := transportConfig{maxIdleConns: intPtr(i)} // Direct (no proxy)
			_, err := getSharedTransport(cfg)
			require.NoError(t, err)
		}

		// 2. Add Proxy connections (Recently used)
		proxyCfg1 := transportConfig{proxyURL: stringPtr("http://proxy1.local"), maxIdleConns: intPtr(9991)}
		proxyCfg2 := transportConfig{proxyURL: stringPtr("http://proxy2.local"), maxIdleConns: intPtr(9992)}

		_, err := getSharedTransport(proxyCfg1)
		require.NoError(t, err)
		_, err = getSharedTransport(proxyCfg2)
		require.NoError(t, err)

		// Assert conditions
		require.Equal(t, limit, transportCacheLRU.Len())
		// proxy2 is at Front (Most Recently Used)
		// proxy1 is next
		// Direct connections are at Back

		// 3. Add one more item to trigger eviction
		newCfg := transportConfig{maxIdleConns: intPtr(8888)}
		_, err = getSharedTransport(newCfg)
		require.NoError(t, err)

		// Verification:
		// Smart Eviction searches from Back (Oldest) for 10 items.
		// Wait, our proxies are at Front (Newest).
		// The logic searches: `curr := transportCacheList.Back(); for i < 10 ...`
		// So it looks at the OLDEST 10 items.
		// If our proxies are Newest, they won't be found by the search loop.
		// So it should fall back to evicting the absolute oldest (Direct).

		// Let's adjust the test to match the logic's intent:
		// Put proxies in the "Oldest 10" zone.

		// Reset and retry logic match
		transportCacheMu.Lock()
		transportCache = make(map[transportCacheKey]*list.Element)
		transportCacheLRU.Init()
		transportCacheMu.Unlock()

		// A. Add Proxy connections FIRST (So they become Oldest)
		pCfg1 := transportConfig{proxyURL: stringPtr("http://p1"), maxIdleConns: intPtr(1)}
		pCfg2 := transportConfig{proxyURL: stringPtr("http://p2"), maxIdleConns: intPtr(2)}
		_, _ = getSharedTransport(pCfg1)
		_, _ = getSharedTransport(pCfg2)

		// B. Add Direct connections to fill the rest (Newest)
		for i := 0; i < limit-2; i++ {
			cfg := transportConfig{maxIdleConns: intPtr(100 + i)}
			_, _ = getSharedTransport(cfg)
		}

		// Now:
		// Back (Oldest) -> pk1, pk2
		// Front (Newest) -> Direct...

		// C. Trigger eviction
		kNewCfg := transportConfig{maxIdleConns: intPtr(9999)}
		_, _ = getSharedTransport(kNewCfg)

		// D. Verify: pk1 (Oldest Proxy) should be evicted.
		// Actually, pk1 is the absolute oldest AND a proxy.
		// So it would be evicted anyway by standard LRU.
		// To prove "Smart Eviction", we need:
		// Oldest = Direct
		// 2nd Oldest = Proxy.
		// If standard LRU -> Oldest (Direct) dies.
		// If Smart Eviction -> Proxy dies (even if 2nd oldest).

		// Let's try "Smart Eviction" proof scenario:
		transportCacheMu.Lock()
		transportCache = make(map[transportCacheKey]*list.Element)
		transportCacheLRU.Init()
		transportCacheMu.Unlock()

		// 1. Add Direct (Will be Absolute Oldest)
		directOldCfg := transportConfig{maxIdleConns: intPtr(1000)}
		_, _ = getSharedTransport(directOldCfg)

		// 2. Add Proxy (Will be 2nd Oldest)
		proxyTargetCfg := transportConfig{proxyURL: stringPtr("http://target"), maxIdleConns: intPtr(2000)}
		_, _ = getSharedTransport(proxyTargetCfg)

		// 3. Fill the rest with Direct
		for i := 0; i < limit-2; i++ {
			cfg := transportConfig{maxIdleConns: intPtr(3000 + i)}
			_, _ = getSharedTransport(cfg)
		}

		// Current State:
		// Back -> [DirectOld] -> [ProxyTarget] -> ... -> Front

		// 4. Trigger Eviction
		_, err = getSharedTransport(transportConfig{maxIdleConns: intPtr(9999)})
		require.NoError(t, err)

		// 5. Verify
		directOldKey := directOldCfg.ToCacheKey()
		proxyTargetKey := proxyTargetCfg.ToCacheKey()

		transportCacheMu.RLock()
		_, hasDirect := transportCache[directOldKey]
		_, hasProxy := transportCache[proxyTargetKey]
		transportCacheMu.RUnlock()

		assert.True(t, hasDirect, "Direct connection (Absolute Oldest) should be SPARED by smart eviction")
		assert.False(t, hasProxy, "Proxy connection (2nd Oldest) should be EVICTED by smart eviction")
	})

	t.Run("Concurrency & Double-Check", func(t *testing.T) {
		// Reset
		transportCacheMu.Lock()
		transportCache = make(map[transportCacheKey]*list.Element)
		transportCacheLRU.Init()
		transportCacheMu.Unlock()

		const goroutines = 20
		const keyCount = 5
		done := make(chan bool)

		for i := 0; i < goroutines; i++ {
			go func(id int) {
				// Use a mix of keys to cause collisions and creation
				cfg := transportConfig{maxIdleConns: intPtr(id % keyCount)}
				_, err := getSharedTransport(cfg)
				assert.NoError(t, err)

				// High concurrency read/write
				for j := 0; j < 100; j++ {
					c := transportConfig{maxIdleConns: intPtr(j % keyCount)}
					_, _ = getSharedTransport(c)
				}
				done <- true
			}(i)
		}

		for i := 0; i < goroutines; i++ {
			<-done
		}

		transportCacheMu.RLock()
		assert.LessOrEqual(t, len(transportCache), keyCount, "Should not exceed unique keys")
		transportCacheMu.RUnlock()
	})
}

func TestParameters_Application(t *testing.T) {
	cfg := transportConfig{
		proxyURL:              stringPtr("http://user:pass@proxy.local:8080"),
		maxIdleConns:          intPtr(123),
		maxConnsPerHost:       intPtr(45),
		idleConnTimeout:       durationPtr(5 * time.Second),
		tlsHandshakeTimeout:   durationPtr(2 * time.Second),
		responseHeaderTimeout: durationPtr(3 * time.Second),
	}

	tr, err := newTransport(nil, cfg)
	require.NoError(t, err)

	// Verify Proxy
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	proxyURL, err := tr.Proxy(req)
	require.NoError(t, err)
	assert.Equal(t, "proxy.local:8080", proxyURL.Host)
	u := proxyURL.User.Username()
	assert.Equal(t, "user", u)

	// Verify Pooling
	assert.Equal(t, 123, tr.MaxIdleConns)
	assert.Equal(t, 0, tr.MaxIdleConnsPerHost) // Should remain default (0 -> 2) as it is not explicitly set
	assert.Equal(t, 45, tr.MaxConnsPerHost)

	// Verify Timeouts
	assert.Equal(t, 5*time.Second, tr.IdleConnTimeout)
	assert.Equal(t, 2*time.Second, tr.TLSHandshakeTimeout)
	assert.Equal(t, 3*time.Second, tr.ResponseHeaderTimeout)
}

func TestTransport_MergesOptions(t *testing.T) {
	baseTr := &http.Transport{
		MaxIdleConns: 10,
	}

	// Requesting 20, which is different from baseTr's 10.
	// Previously, this option would be ignored. Now it should be applied.
	f := NewHTTPFetcher(WithMaxIdleConns(20))

	// Inject base transport
	f.client.Transport = baseTr

	// Trigger setup
	err := f.setupTransport()
	require.NoError(t, err)

	// Result
	finalTr := f.client.Transport.(*http.Transport)

	// Should be a NEW object (cloned) because we requested a change (20 != 10)
	assert.NotEqual(t, baseTr, finalTr, "Spec: WithTransport + Options should trigger cloning")

	// Should have NEW settings applied
	assert.Equal(t, 20, finalTr.MaxIdleConns)
	assert.Equal(t, 0, finalTr.MaxIdleConnsPerHost) // Should NOT be changed (preserved from baseTr)

	// Sentinel Value Check:
	// We didn't set Proxy, so it should remain nil (default of baseTr)
	assert.Nil(t, finalTr.Proxy)
}

func TestTransport_Sentinels_DoNotOverride(t *testing.T) {
	// Scenario: User supplies a transport with specific settings,
	// and does NOT provide any overriding options.
	// The transport should be preserved as-is (or cloned without changes).

	baseTr := &http.Transport{
		MaxIdleConns:    55,
		IdleConnTimeout: 123 * time.Second,
		MaxConnsPerHost: 99,
	}

	f := NewHTTPFetcher() // No options -> All sentinels (-1, 0)
	f.client.Transport = baseTr

	err := f.setupTransport()
	require.NoError(t, err)

	finalTr := f.client.Transport.(*http.Transport)

	// Since SENTINELs are used, shouldCloneTransport(tr) should return false.
	// Optimization: Reuse original object
	assert.Equal(t, baseTr, finalTr)

	// Verify values are preserved
	assert.Equal(t, 55, finalTr.MaxIdleConns)
	assert.Equal(t, 123*time.Second, finalTr.IdleConnTimeout)
	assert.Equal(t, 99, finalTr.MaxConnsPerHost)
}

// TestCreateTransport_Internal verifies internal helper logic.
func TestCreateTransport_Internal(t *testing.T) {
	t.Run("Proxy Redaction", func(t *testing.T) {
		// Verify that invalid proxy URL in key returns a safe error
		cfg := transportConfig{proxyURL: stringPtr("http://user:secret@:invalid-port")}
		_, err := newTransport(nil, cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "프록시 URL")
		assert.NotContains(t, err.Error(), "secret") // Password should be redacted
	})

	t.Run("NoProxy Constant", func(t *testing.T) {
		// [수정됨] NoProxy 설정 시 Transport.Proxy는 nil이 아니어야 함 (환경 변수 무시를 위해)
		// 대신 호출 시 nil을 반환하는 함수여야 합니다.
		cfg := transportConfig{proxyURL: stringPtr(NoProxy)}

		// 환경 변수 설정 (테스트를 위해 잠시 설정)
		os.Setenv("HTTP_PROXY", "http://env-proxy-should-be-ignored:8080")
		defer os.Unsetenv("HTTP_PROXY")

		tr, err := newTransport(nil, cfg)
		require.NoError(t, err)

		// 1. Proxy 필드 자체가 nil이면 안 됨 (nil이면 환경 변수 사용함)
		assert.NotNil(t, tr.Proxy, "Transport.Proxy 함수는 nil이 아니어야 합니다 (환경 변수 무시 설정)")

		// 2. Proxy 함수 호출 결과가 nil이어야 함 (직접 연결)
		reqUrl, _ := url.Parse("http://example.com")
		proxyUrl, err := tr.Proxy(&http.Request{URL: reqUrl})
		require.NoError(t, err)
		assert.Nil(t, proxyUrl, "NoProxy 설정 시 ProxyURL은 nil이어야 합니다")
	})

	t.Run("Environment Fallback", func(t *testing.T) {
		// 시나리오: ProxyURL 설정을 안 했을 때(nil), 환경 변수를 따라가는지 확인
		os.Setenv("HTTP_PROXY", "http://env-fallback:8080")
		defer os.Unsetenv("HTTP_PROXY")

		cfg := transportConfig{proxyURL: nil}
		tr, err := newTransport(nil, cfg)
		require.NoError(t, err)

		// 기본값(nil)이어야 환경 변수 동작이 활성화됨
		if tr.Proxy != nil {
			// Proxy 필드가 nil이 아닐 수도 있음 (Go 버전에 따라 다를 수 있으나, 보통 nil임)
			// 핵심은 동작 여부
		}

		reqUrl, _ := url.Parse("http://example.com")
		proxyUrl, err := tr.Proxy(&http.Request{URL: reqUrl})
		require.NoError(t, err)

		require.NotNil(t, proxyUrl, "환경 변수가 설정되어 있으면 프록시 URL이 반환되어야 합니다")
		assert.Equal(t, "http://env-fallback:8080", proxyUrl.String())
	})
}

// TestHTTPFetcher_TransportSelection verifies that correct transport (Default vs Shared vs Isolated) is selected.
func TestHTTPFetcher_TransportSelection(t *testing.T) {
	t.Run("Selects Default Transport", func(t *testing.T) {
		f := NewHTTPFetcher()
		assert.Equal(t, defaultTransport, f.client.Transport)
	})

	t.Run("Selects Shared Transport", func(t *testing.T) {
		// Using options that trigger customization -> shared cache
		f := NewHTTPFetcher(WithMaxIdleConns(50))
		tr, ok := f.client.Transport.(*http.Transport)
		require.True(t, ok)
		assert.NotEqual(t, defaultTransport, tr)
		assert.Equal(t, 50, tr.MaxIdleConns)
	})

	t.Run("Selects Isolated Transport", func(t *testing.T) {
		f := NewHTTPFetcher(WithDisableTransportCaching(true))
		tr, ok := f.client.Transport.(*http.Transport)
		require.True(t, ok)

		// Verify isolation by mutation
		originalMaxIdle := defaultTransport.MaxIdleConns

		// Verify that isolation sets default values correctly even if fetcher has sentinels
		assert.Equal(t, 100, tr.MaxIdleConns)

		// Modify the isolated transport
		tr.MaxIdleConns = originalMaxIdle + 1

		// Assert that defaultTransport remains unchanged
		assert.Equal(t, originalMaxIdle, defaultTransport.MaxIdleConns, "defaultTransport should not be modified")
		assert.NotEqual(t, defaultTransport.MaxIdleConns, tr.MaxIdleConns, "Isolated transport should be modified")
	})
}

// TestRetryFetcher_Internal_Helpers tests internal helper behavior for RetryFetcher.
// Since we are in the same package, we can test internal methods/state if needed.
// (Moved from previous http_internal_test.go to preserve coverage)
func TestRetryFetcher_NonRetriableStatuses_Internal(t *testing.T) {
	// ... (Same logic as before, just consolidated)
	// This test essentially verifies IsRetriable logic via integration.

	tests := []struct {
		status    int
		retriable bool
	}{
		{http.StatusInternalServerError, true},
		{http.StatusNotImplemented, false},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			callCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callCount++
				w.WriteHeader(tt.status)
			}))
			defer server.Close()

			f := NewHTTPFetcher()
			rf := NewRetryFetcher(f, 2, 1*time.Millisecond, 10*time.Millisecond)

			req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
			_, _ = rf.Do(req)

			if tt.retriable {
				assert.Equal(t, 3, callCount)
			} else {
				assert.Equal(t, 1, callCount)
			}
		})
	}
}

// TestRegression_NeedsCustomTransport verifies that MaxIdleConnsPerHost is correctly checked.
func TestRegression_NeedsCustomTransport(t *testing.T) {
	// Scenario: ONLY MaxIdleConnsPerHost is changed from default.
	// Previously, this leaked defaultTransport settings because checking this field was missed.
	f := NewHTTPFetcher(WithMaxIdleConnsPerHost(50))

	// This should be TRUE
	assert.True(t, f.needsCustomTransport(), "needsCustomTransport should return true when MaxIdleConnsPerHost is set")

	// Verify effect in Setup
	err := f.setupTransport()
	assert.NoError(t, err)

	tr, ok := f.client.Transport.(*http.Transport)
	require.True(t, ok)
	assert.Equal(t, 50, tr.MaxIdleConnsPerHost) // If false, this would likely be default (2) or MaxIdleConns
}

// TestRegression_ConfigureTransportFromProvided_PreservesHostLimit verifies that
// setting MaxIdleConns does NOT aggressively override MaxIdleConnsPerHost in provided transport.
func TestRegression_ConfigureTransportFromProvided_PreservesHostLimit(t *testing.T) {
	// Scenario: User provides a transport with explicit Host Limit (Low),
	// but wraps it with a Fetcher that sets MaxIdleConns (High).
	// Logic should NOT overwrite the Host Limit with the High value unless explicitly requested.

	baseTr := &http.Transport{
		MaxIdleConns:        1, // Ignored/Overridden
		MaxIdleConnsPerHost: 2, // IMPORTANT: Should remain 2
	}

	// Fetcher configures MaxIdleConns = 100
	// It does NOT configure MaxIdleConnsPerHost (Sentinel -1)
	f := NewHTTPFetcher(WithMaxIdleConns(100))
	f.client.Transport = baseTr

	// Trigger setup
	err := f.setupTransport()
	require.NoError(t, err)

	finalTr := f.client.Transport.(*http.Transport)

	// MaxIdleConns should be updated to 100
	assert.Equal(t, 100, finalTr.MaxIdleConns)

	// MaxIdleConnsPerHost should REMAIN 2 (from baseTr)
	// OLD BUG: It was forcefully updated to 100
	assert.Equal(t, 2, finalTr.MaxIdleConnsPerHost, "Should preserve original MaxIdleConnsPerHost when not explicitly overridden")
}

// TestTransportConfig_ToCacheKey verifies the normalization logic of ToCacheKey.
func TestTransportConfig_ToCacheKey(t *testing.T) {
	t.Run("Normalizes Nil Pointers to Defaults", func(t *testing.T) {
		cfg := transportConfig{} // All nil
		key := cfg.ToCacheKey()

		assert.Equal(t, "", key.proxyURL)
		assert.Equal(t, defaultMaxIdleConns, key.maxIdleConns)
		assert.Equal(t, 0, key.maxIdleConnsPerHost) // Normalized to 0 (default 2 logic handled elsewhere)
		assert.Equal(t, 0, key.maxConnsPerHost)     // unlimited
		assert.Equal(t, defaultTLSHandshakeTimeout, key.tlsHandshakeTimeout)
		assert.Equal(t, 0*time.Second, key.responseHeaderTimeout) // unlimited
		assert.Equal(t, defaultIdleConnTimeout, key.idleConnTimeout)
	})

	t.Run("Normalizes Proxy Configuration", func(t *testing.T) {
		// Case 1: Nil -> ""
		cfg1 := transportConfig{proxyURL: nil}
		assert.Equal(t, "", cfg1.ToCacheKey().proxyURL)

		// Case 2: "" -> NoProxy (Normalization for cache sharing)
		cfg2 := transportConfig{proxyURL: stringPtr("")}
		assert.Equal(t, NoProxy, cfg2.ToCacheKey().proxyURL)

		// Case 3: NoProxy -> NoProxy
		cfg3 := transportConfig{proxyURL: stringPtr(NoProxy)}
		assert.Equal(t, NoProxy, cfg3.ToCacheKey().proxyURL)

		// Case 4: URL -> URL
		cfg4 := transportConfig{proxyURL: stringPtr("http://proxy")}
		assert.Equal(t, "http://proxy", cfg4.ToCacheKey().proxyURL)
	})

	t.Run("Normalizes Explicit Values", func(t *testing.T) {
		cfg := transportConfig{
			maxIdleConns:        intPtr(50),
			tlsHandshakeTimeout: durationPtr(5 * time.Second),
		}
		key := cfg.ToCacheKey()

		assert.Equal(t, 50, key.maxIdleConns)
		assert.Equal(t, 5*time.Second, key.tlsHandshakeTimeout)
		// Others should be defaults
		assert.Equal(t, defaultIdleConnTimeout, key.idleConnTimeout)
	})
}

// TestHTTPFetcher_shouldCloneTransport verifies the CoW optimization logic.
func TestHTTPFetcher_shouldCloneTransport(t *testing.T) {
	baseTr := &http.Transport{
		MaxIdleConns:        100,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	t.Run("Returns False when Settings Match (Optimization)", func(t *testing.T) {
		// User sets SAME values as base transport
		f := NewHTTPFetcher(
			WithMaxIdleConns(100),
			WithTLSHandshakeTimeout(10*time.Second),
		)
		// Should NOT clone
		assert.False(t, f.shouldCloneTransport(baseTr))
	})

	t.Run("Returns False when User Settings are Nil", func(t *testing.T) {
		f := NewHTTPFetcher() // No options
		assert.False(t, f.shouldCloneTransport(baseTr))
	})

	t.Run("Returns True when Settings Differ", func(t *testing.T) {
		f := NewHTTPFetcher(WithMaxIdleConns(101))
		assert.True(t, f.shouldCloneTransport(baseTr))
	})

	t.Run("Returns True when Proxy is Set", func(t *testing.T) {
		// Even if baseTr has a proxy, we don't check for equality of proxy URL deeply in shouldCloneTransport
		// because f.proxyURL being non-nil is a trigger for creating a NEW transport with that proxy.
		// (Current logic: f.proxyURL != nil -> true)
		f := NewHTTPFetcher(WithProxy("http://proxy"))
		assert.True(t, f.shouldCloneTransport(baseTr))
	})
}

// TestHTTPFetcher_needsCustomTransport_Exhaustive validates all triggers for custom transport.
func TestHTTPFetcher_needsCustomTransport_Exhaustive(t *testing.T) {
	tests := []struct {
		name     string
		opts     []Option
		expected bool
	}{
		{"No Options", nil, false},
		{"DisableTransportCaching", []Option{WithDisableTransportCaching(true)}, true},
		{"WithProxy", []Option{WithProxy("http://p")}, true},
		{"WithMaxIdleConns", []Option{WithMaxIdleConns(10)}, true},
		{"WithMaxIdleConnsPerHost", []Option{WithMaxIdleConnsPerHost(10)}, true},
		{"WithMaxConnsPerHost", []Option{WithMaxConnsPerHost(10)}, true},
		{"WithTimeout (Note: Affects Client, not Transport, so SHOULD BE FALSE for Transport)", []Option{WithTimeout(10 * time.Second)}, false},
		{"WithTLSHandshakeTimeout", []Option{WithTLSHandshakeTimeout(10 * time.Second)}, true},
		{"WithResponseHeaderTimeout", []Option{WithResponseHeaderTimeout(10 * time.Second)}, true},
		{"WithIdleConnTimeout", []Option{WithIdleConnTimeout(10 * time.Second)}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := NewHTTPFetcher(tc.opts...)
			assert.Equal(t, tc.expected, f.needsCustomTransport())
		})
	}
}
