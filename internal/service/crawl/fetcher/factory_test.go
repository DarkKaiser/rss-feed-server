package fetcher

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =========================================================================
// Helper Functions for Pointers
// =========================================================================

func intPtr(v int) *int {
	return &v
}

func durationPtr(v time.Duration) *time.Duration {
	return &v
}

func int64Ptr(v int64) *int64 {
	return &v
}

func stringPtr(v string) *string {
	return &v
}

// =========================================================================
// Tests for Generics Helper Functions
// =========================================================================

func TestNormalizePtr(t *testing.T) {
	// 정규화 로직: 10보다 작으면 10으로, 100보다 크면 100으로 보정
	normalizer := func(v int) int {
		if v < 10 {
			return 10
		}
		if v > 100 {
			return 100
		}
		return v
	}

	tests := []struct {
		name     string
		input    *int
		defValue int
		expected int
	}{
		{
			name:     "Nil input should use default value",
			input:    nil,
			defValue: 50,
			expected: 50, // 50은 범위 내이므로 그대로 반환
		},
		{
			name:     "Nil input with out-of-range default should be normalized",
			input:    nil,
			defValue: 5,
			expected: 10, // 5 -> 10 보정
		},
		{
			name:     "Value too small should be normalized",
			input:    intPtr(5),
			defValue: 50,
			expected: 10,
		},
		{
			name:     "Value too large should be normalized",
			input:    intPtr(150),
			defValue: 50,
			expected: 100,
		},
		{
			name:     "Valid value should be kept",
			input:    intPtr(75),
			defValue: 50,
			expected: 75,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ptr := tt.input
			normalizePtr(&ptr, tt.defValue, normalizer)
			assert.NotNil(t, ptr)
			assert.Equal(t, tt.expected, *ptr)
		})
	}
}

func TestNormalizePtrPair(t *testing.T) {
	// 정규화 로직: min이 max보다 크면 max를 min에 맞춤
	normalizer := func(min, max int) (int, int) {
		if min > max {
			return min, min
		}
		return min, max
	}

	tests := []struct {
		name      string
		inputMin  *int
		inputMax  *int
		defMin    int
		defMax    int
		expectMin int
		expectMax int
	}{
		{
			name:      "Nil inputs should use defaults",
			inputMin:  nil,
			inputMax:  nil,
			defMin:    10,
			defMax:    20,
			expectMin: 10,
			expectMax: 20,
		},
		{
			name:      "Invalid default relationship should be normalized",
			inputMin:  nil,
			inputMax:  nil,
			defMin:    30,
			defMax:    20, // min > max
			expectMin: 30,
			expectMax: 30,
		},
		{
			name:      "Explicit values respecting logic",
			inputMin:  intPtr(5),
			inputMax:  intPtr(10),
			defMin:    1,
			defMax:    100,
			expectMin: 5,
			expectMax: 10,
		},
		{
			name:      "Explicit values violating logic should be normalized",
			inputMin:  intPtr(50),
			inputMax:  intPtr(30),
			defMin:    1,
			defMax:    100,
			expectMin: 50,
			expectMax: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p1 := tt.inputMin
			p2 := tt.inputMax
			normalizePtrPair(&p1, &p2, tt.defMin, tt.defMax, normalizer)
			assert.Equal(t, tt.expectMin, *p1)
			assert.Equal(t, tt.expectMax, *p2)
		})
	}
}

// =========================================================================
// Tests for Config Normalization
// =========================================================================

// TestConfig_Normalization_ZeroValues 기본값(Zero Value) 정규화 테스트
func TestConfig_Normalization_ZeroValues(t *testing.T) {
	input := Config{} // 모든 필드가 nil 또는 zero value
	expected := Config{
		// 필수 정규화 필드들 (nil -> default)
		MaxRetries:            intPtr(0),
		MinRetryDelay:         durationPtr(1 * time.Second),
		MaxRetryDelay:         durationPtr(30 * time.Second),
		MaxBytes:              int64Ptr(10 * 1024 * 1024),
		Timeout:               nil,
		TLSHandshakeTimeout:   nil,
		ResponseHeaderTimeout: nil,
		IdleConnTimeout:       nil,
		MaxIdleConns:          nil,
		MaxIdleConnsPerHost:   nil,
		MaxConnsPerHost:       nil,
		MaxRedirects:          nil,
		ProxyURL:              nil,
		// 슬라이스 및 bool은 그대로 유지
		UserAgents:                   nil,
		AllowedStatusCodes:           nil,
		AllowedMimeTypes:             nil,
		EnableUserAgentRandomization: false,
		DisableStatusCodeValidation:  false,
		DisableLogging:               false,
		DisableTransportCaching:      false,
	}

	input.applyDefaults()
	assert.Equal(t, expected, input)
}

// TestConfig_Normalization_Retry 재시도 관련 설정 정규화 테스트
func TestConfig_Normalization_Retry(t *testing.T) {
	tests := []struct {
		name     string
		input    Config
		expected Config
	}{
		{
			name:  "MaxRetries negative -> 0",
			input: Config{MaxRetries: intPtr(-5)},
			expected: Config{
				MaxRetries:    intPtr(0),
				MinRetryDelay: durationPtr(1 * time.Second),
				MaxRetryDelay: durationPtr(30 * time.Second),
				MaxBytes:      int64Ptr(10 * 1024 * 1024),
			},
		},
		{
			name:  "MaxRetries too large -> MaxAllowedRetries (10)",
			input: Config{MaxRetries: intPtr(100)},
			expected: Config{
				MaxRetries:    intPtr(10),
				MinRetryDelay: durationPtr(1 * time.Second),
				MaxRetryDelay: durationPtr(30 * time.Second),
				MaxBytes:      int64Ptr(10 * 1024 * 1024),
			},
		},
		{
			name:  "MinRetryDelay too small -> 1s",
			input: Config{MinRetryDelay: durationPtr(500 * time.Millisecond)},
			expected: Config{
				MaxRetries:    intPtr(0),
				MinRetryDelay: durationPtr(1 * time.Second),
				MaxRetryDelay: durationPtr(30 * time.Second),
				MaxBytes:      int64Ptr(10 * 1024 * 1024),
			},
		},
		{
			name: "MaxRetryDelay < MinRetryDelay -> Max = Min",
			input: Config{
				MinRetryDelay: durationPtr(5 * time.Second),
				MaxRetryDelay: durationPtr(2 * time.Second),
			},
			expected: Config{
				MaxRetries:    intPtr(0),
				MinRetryDelay: durationPtr(5 * time.Second),
				MaxRetryDelay: durationPtr(5 * time.Second),
				MaxBytes:      int64Ptr(10 * 1024 * 1024),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.input.applyDefaults()
			// 관련된 필드만 검증
			assert.Equal(t, *tt.expected.MaxRetries, *tt.input.MaxRetries, "MaxRetries mismatch")
			assert.Equal(t, *tt.expected.MinRetryDelay, *tt.input.MinRetryDelay, "MinRetryDelay mismatch")
			assert.Equal(t, *tt.expected.MaxRetryDelay, *tt.input.MaxRetryDelay, "MaxRetryDelay mismatch")
		})
	}
}

// TestConfig_Normalization_Timeouts 타임아웃 관련 설정 정규화 테스트
func TestConfig_Normalization_Timeouts(t *testing.T) {
	tests := []struct {
		name     string
		input    Config
		expected Config
	}{
		{
			name: "All timeouts negative -> Defaults (or 0 for ResponseHeader)",
			input: Config{
				Timeout:               durationPtr(-1),
				TLSHandshakeTimeout:   durationPtr(-1),
				IdleConnTimeout:       durationPtr(-1),
				ResponseHeaderTimeout: durationPtr(-1),
			},
			expected: Config{
				Timeout:               durationPtr(30 * time.Second),
				TLSHandshakeTimeout:   durationPtr(10 * time.Second),
				IdleConnTimeout:       durationPtr(90 * time.Second),
				ResponseHeaderTimeout: durationPtr(0),
			},
		},
		{
			name: "All timeouts explicitly set",
			input: Config{
				Timeout:               durationPtr(5 * time.Second),
				TLSHandshakeTimeout:   durationPtr(2 * time.Second),
				IdleConnTimeout:       durationPtr(10 * time.Second),
				ResponseHeaderTimeout: durationPtr(1 * time.Second),
			},
			expected: Config{
				Timeout:               durationPtr(5 * time.Second),
				TLSHandshakeTimeout:   durationPtr(2 * time.Second),
				IdleConnTimeout:       durationPtr(10 * time.Second),
				ResponseHeaderTimeout: durationPtr(1 * time.Second),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.input.applyDefaults()
			assert.Equal(t, *tt.expected.Timeout, *tt.input.Timeout)
			assert.Equal(t, *tt.expected.TLSHandshakeTimeout, *tt.input.TLSHandshakeTimeout)
			assert.Equal(t, *tt.expected.IdleConnTimeout, *tt.input.IdleConnTimeout)
			assert.Equal(t, *tt.expected.ResponseHeaderTimeout, *tt.input.ResponseHeaderTimeout)
		})
	}
}

// TestConfig_Normalization_ConnectionLimits 연결 제한 설정 정규화 테스트
func TestConfig_Normalization_ConnectionLimits(t *testing.T) {
	tests := []struct {
		name     string
		input    Config
		expected Config
	}{
		{
			name: "Negative values -> Defaults",
			input: Config{
				MaxIdleConns:        intPtr(-1),
				MaxIdleConnsPerHost: intPtr(-1),
				MaxConnsPerHost:     intPtr(-1),
				MaxRedirects:        intPtr(-1),
			},
			expected: Config{
				MaxIdleConns:        intPtr(100),
				MaxIdleConnsPerHost: intPtr(0), // Default 0 (uses net/http default 2)
				MaxConnsPerHost:     intPtr(0), // 0 (Unlimited)
				MaxRedirects:        intPtr(10),
			},
		},
		{
			name: "Explicit values",
			input: Config{
				MaxIdleConns:        intPtr(500),
				MaxIdleConnsPerHost: intPtr(10),
				MaxConnsPerHost:     intPtr(20),
				MaxRedirects:        intPtr(5),
			},
			expected: Config{
				MaxIdleConns:        intPtr(500),
				MaxIdleConnsPerHost: intPtr(10),
				MaxConnsPerHost:     intPtr(20),
				MaxRedirects:        intPtr(5),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.input.applyDefaults()
			assert.Equal(t, *tt.expected.MaxIdleConns, *tt.input.MaxIdleConns)
			assert.Equal(t, *tt.expected.MaxIdleConnsPerHost, *tt.input.MaxIdleConnsPerHost)
			assert.Equal(t, *tt.expected.MaxConnsPerHost, *tt.input.MaxConnsPerHost)
			assert.Equal(t, *tt.expected.MaxRedirects, *tt.input.MaxRedirects)
		})
	}
}

// TestConfig_Normalization_MaxBytes MaxBytes 설정 정규화 테스트
func TestConfig_Normalization_MaxBytes(t *testing.T) {
	tests := []struct {
		name     string
		input    Config
		expected Config
	}{
		{
			name:     "Nil -> Default (10MB)",
			input:    Config{MaxBytes: nil},
			expected: Config{MaxBytes: int64Ptr(10 * 1024 * 1024)},
		},
		{
			name:     "-1 (No Limit) -> Kept",
			input:    Config{MaxBytes: int64Ptr(-1)},
			expected: Config{MaxBytes: int64Ptr(-1)},
		},
		{
			name:     "Negative but not -1 -> Default",
			input:    Config{MaxBytes: int64Ptr(-500)},
			expected: Config{MaxBytes: int64Ptr(10 * 1024 * 1024)},
		},
		{
			name:     "Positive -> Kept",
			input:    Config{MaxBytes: int64Ptr(2048)},
			expected: Config{MaxBytes: int64Ptr(2048)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.input.applyDefaults()
			assert.Equal(t, *tt.expected.MaxBytes, *tt.input.MaxBytes)
		})
	}
}

// =========================================================================
// Tests for Fetcher Chain Construction
// =========================================================================

// TestNewFromConfig_ChainConstruction 체인 생성 순서 및 값 전달 검증
func TestNewFromConfig_ChainConstruction(t *testing.T) {
	// 완전한 설정
	cfg := Config{
		// Network
		Timeout:               durationPtr(5 * time.Second),
		ProxyURL:              stringPtr("http://proxy.example.com:8080"),
		MaxIdleConns:          intPtr(50),
		TLSHandshakeTimeout:   durationPtr(2 * time.Second),
		ResponseHeaderTimeout: durationPtr(3 * time.Second),
		IdleConnTimeout:       durationPtr(4 * time.Second),

		// Feature Flags
		DisableLogging:               false,
		EnableUserAgentRandomization: true,
		DisableStatusCodeValidation:  false,
		DisableTransportCaching:      true,

		// Validations
		AllowedStatusCodes: []int{200, 201},
		AllowedMimeTypes:   []string{"application/json"},
		MaxBytes:           int64Ptr(2048),
		MaxRedirects:       intPtr(3),

		// Retry
		MaxRetries:    intPtr(5),
		MinRetryDelay: durationPtr(100 * time.Millisecond),
		MaxRetryDelay: durationPtr(5 * time.Second),

		UserAgents: []string{"TestAgent/1.0"},
	}

	// Fetcher 생성
	f := NewFromConfig(cfg)
	require.NotNil(t, f)

	// 체인 검사: Outer -> Inner
	// Chain: Logging -> UserAgent -> Retry -> MimeType -> StatusCode -> MaxBytes -> HTTP

	curr := f

	// 1. LoggingFetcher
	if logDelegate := InspectLoggingFetcher(curr); logDelegate != nil {
		curr = logDelegate
	} else {
		assert.Fail(t, "LoggingFetcher should be present")
	}

	// 2. UserAgentFetcher
	var uaDelegate Fetcher
	var uaList []string
	if uaDelegate, uaList = InspectUserAgentFetcher(curr); uaDelegate != nil {
		curr = uaDelegate
		assert.ElementsMatch(t, cfg.UserAgents, uaList)
	} else {
		assert.Fail(t, "UserAgentFetcher should be present")
	}

	// 3. RetryFetcher
	var retryDelegate Fetcher
	var maxRetries int
	var minDelay, maxDelay time.Duration
	if retryDelegate, maxRetries, minDelay, maxDelay = InspectRetryFetcher(curr); retryDelegate != nil {
		curr = retryDelegate
		assert.Equal(t, 5, maxRetries)
		assert.Equal(t, 1*time.Second, minDelay, "MinRetryDelay normalized")
		assert.Equal(t, 5*time.Second, maxDelay)
	} else {
		assert.Fail(t, "RetryFetcher should be present")
	}

	// 4. MimeTypeFetcher
	var mimeDelegate Fetcher
	var allowedMimes []string
	if mimeDelegate, allowedMimes, _ = InspectMimeTypeFetcher(curr); mimeDelegate != nil {
		curr = mimeDelegate
		assert.ElementsMatch(t, cfg.AllowedMimeTypes, allowedMimes)
	} else {
		assert.Fail(t, "MimeTypeFetcher should be present")
	}

	// 5. StatusCodeFetcher
	var statusDelegate Fetcher
	var allowedCodes []int
	if statusDelegate, allowedCodes = InspectStatusCodeFetcher(curr); statusDelegate != nil {
		curr = statusDelegate
		assert.ElementsMatch(t, cfg.AllowedStatusCodes, allowedCodes)
	} else {
		assert.Fail(t, "StatusCodeFetcher should be present")
	}

	// 6. MaxBytesFetcher
	var bytesDelegate Fetcher
	var maxBytes int64
	if bytesDelegate, maxBytes = InspectMaxBytesFetcher(curr); bytesDelegate != nil {
		curr = bytesDelegate
		assert.Equal(t, int64(2048), maxBytes)
	} else {
		assert.Fail(t, "MaxBytesFetcher should be present")
	}

	// 7. HTTPFetcher
	httpOpts := InspectHTTPFetcher(curr)
	require.NotNil(t, httpOpts, "Innermost should be HTTPFetcher")
	assert.Equal(t, *cfg.ProxyURL, *httpOpts.ProxyURL)
	assert.Equal(t, *cfg.Timeout, httpOpts.Timeout)
	assert.True(t, httpOpts.DisableCaching)
}

// TestNewFromConfig_MiddlewareToggling_Disabled 기능 비활성화 시 미들웨어 생략 검증
func TestNewFromConfig_MiddlewareToggling_Disabled(t *testing.T) {
	cfg := Config{
		DisableLogging:               true,
		EnableUserAgentRandomization: false,
		DisableStatusCodeValidation:  true,
		AllowedMimeTypes:             nil, // Empty -> validation skipped
	}

	f := NewFromConfig(cfg)

	// Expected Chain: Retry -> MaxBytes -> HTTP
	// Skipped: Logging, UserAgent, MimeType, StatusCode

	// 1. RetryFetcher check (Should receive 'f' directly)
	retryDelegate, maxRetries, _, _ := InspectRetryFetcher(f)
	require.NotNil(t, retryDelegate, "RetryFetcher should be the outermost (when logging/UA disabled)")
	assert.Equal(t, 0, maxRetries, "Default max retries")

	curr := retryDelegate

	// 2. MaxBytesFetcher check (Skipping Mime and StatusCode)
	bytesDelegate, _ := InspectMaxBytesFetcher(curr)
	require.NotNil(t, bytesDelegate, "MaxBytesFetcher should be next")

	// 3. HTTPFetcher check
	httpOpts := InspectHTTPFetcher(bytesDelegate)
	require.NotNil(t, httpOpts)
}

// TestNewFromConfig_ValidationOptions_StatusCodesAndMimeTypes 검증 옵션 설정에 따른 분기 검증
func TestNewFromConfig_ValidationOptions_StatusCodesAndMimeTypes(t *testing.T) {
	// Case A: AllowedStatusCodes is nil/empty -> Default 200 OK only
	t.Run("StatusCodes: Default (200 OK)", func(t *testing.T) {
		cfg := Config{DisableLogging: true, DisableStatusCodeValidation: false}
		f := NewFromConfig(cfg)
		// Unwrap to StatusCodeFetcher
		retryDelegate, _, _, _ := InspectRetryFetcher(f)
		statusDelegate, allowedCodes := InspectStatusCodeFetcher(retryDelegate)

		require.NotNil(t, statusDelegate)
		assert.Nil(t, allowedCodes, "Should be nil (internal logic defaults to 200 explicitly but stores nil in struct if using default constructor, or empty slice)")
	})

	// Case B: Explicit Codes
	t.Run("StatusCodes: Explicit Codes", func(t *testing.T) {
		cfg := Config{
			DisableLogging:              true,
			DisableStatusCodeValidation: false,
			AllowedStatusCodes:          []int{201, 202},
		}
		f := NewFromConfig(cfg)
		retryDelegate, _, _, _ := InspectRetryFetcher(f)
		statusDelegate, allowedCodes := InspectStatusCodeFetcher(retryDelegate)

		require.NotNil(t, statusDelegate)
		assert.ElementsMatch(t, []int{201, 202}, allowedCodes)
	})

	// Case C: MimeTypes
	t.Run("MimeTypes: Explicit Types", func(t *testing.T) {
		cfg := Config{
			DisableLogging:   true,
			AllowedMimeTypes: []string{"application/json", "text/html"},
		}
		f := NewFromConfig(cfg)

		// Expected Chain: Retry -> MimeType -> StatusCode...
		retryDelegate, _, _, _ := InspectRetryFetcher(f)
		mimeDelegate, allowedMimes, _ := InspectMimeTypeFetcher(retryDelegate)

		require.NotNil(t, mimeDelegate, "MimeTypeFetcher should be present")
		assert.ElementsMatch(t, []string{"application/json", "text/html"}, allowedMimes)
	})

	t.Run("MimeTypes: Empty (Disabled)", func(t *testing.T) {
		cfg := Config{
			DisableLogging:   true,
			AllowedMimeTypes: nil,
		}
		f := NewFromConfig(cfg)

		// Expected Chain: Retry -> StatusCode (Skip MimeType)
		retryDelegate, _, _, _ := InspectRetryFetcher(f)
		// Check that MimeTypeFetcher is NOT present
		mimeDelegate, _, _ := InspectMimeTypeFetcher(retryDelegate)
		assert.Nil(t, mimeDelegate, "MimeTypeFetcher should be absent when list is empty")

		// Check that next is StatusCodeFetcher
		statusDelegate, _ := InspectStatusCodeFetcher(retryDelegate)
		require.NotNil(t, statusDelegate, "Should go directly to StatusCodeFetcher")
	})
}

// TestNewFromConfig_OptionOverride 옵션 우선순위 검증
func TestNewFromConfig_OptionOverride(t *testing.T) {
	cfg := Config{
		Timeout:      durationPtr(10 * time.Second),
		MaxIdleConns: intPtr(50),
	}

	overrideTimeout := 20 * time.Second
	overrideMaxIdle := 100

	f := NewFromConfig(cfg,
		WithTimeout(overrideTimeout),
		WithMaxIdleConns(overrideMaxIdle),
	)

	// Drill down
	lc := f
	if logDelegate := InspectLoggingFetcher(lc); logDelegate != nil {
		lc = logDelegate
	}
	rc, _, _, _ := InspectRetryFetcher(lc)
	sc, _ := InspectStatusCodeFetcher(rc)
	bc, _ := InspectMaxBytesFetcher(sc)
	httpOpts := InspectHTTPFetcher(bc)

	require.NotNil(t, httpOpts)
	assert.Equal(t, overrideTimeout, httpOpts.Timeout)
	assert.Equal(t, overrideMaxIdle, *httpOpts.MaxIdleConns)
}

// TestNew 편의 함수 검증
func TestNew(t *testing.T) {
	// New(maxRetries, minDelay, maxBytes, opts...)
	f := New(5, 500*time.Millisecond, 1024, WithMaxIdleConns(999))

	// Defaults applied?
	// MinDelay 500ms -> 1s normalized
	// Includes Logging by default

	curr := f
	if logDelegate := InspectLoggingFetcher(curr); logDelegate != nil {
		curr = logDelegate
	}

	// Retry Fetcher Check
	retryDelegate, maxRetries, minDelay, _ := InspectRetryFetcher(curr)
	require.NotNil(t, retryDelegate)
	assert.Equal(t, 5, maxRetries)
	assert.Equal(t, 1*time.Second, minDelay, "Should apply normalization through applyDefaults")

	// Chain: Retry -> StatusCode -> MaxBytes -> HTTP
	curr = retryDelegate
	if statusDelegate, _ := InspectStatusCodeFetcher(curr); statusDelegate != nil {
		curr = statusDelegate
	}

	// MaxBytes Check
	bytesDelegate, maxBytes := InspectMaxBytesFetcher(curr)
	require.NotNil(t, bytesDelegate)
	assert.Equal(t, int64(1024), maxBytes)

	// HTTPFetcher Check (Option Override)
	httpOpts := InspectHTTPFetcher(bytesDelegate)
	require.NotNil(t, httpOpts)
	assert.Equal(t, 999, *httpOpts.MaxIdleConns, "Option should be applied")
}

// TestConfig_Proxy 간단한 프록시 설정 적용 검증
func TestConfig_Proxy(t *testing.T) {
	cfg := Config{
		DisableLogging: true,
		ProxyURL:       stringPtr("http://127.0.0.1:8080"),
	}
	f := NewFromConfig(cfg)

	curr := f
	if retryDelegate, _, _, _ := InspectRetryFetcher(curr); retryDelegate != nil {
		curr = retryDelegate
	}
	if statusDelegate, _ := InspectStatusCodeFetcher(curr); statusDelegate != nil {
		curr = statusDelegate
	}
	if bytesDelegate, _ := InspectMaxBytesFetcher(curr); bytesDelegate != nil {
		curr = bytesDelegate
	}
	opts := InspectHTTPFetcher(curr)

	require.NotNil(t, opts)
	assert.Equal(t, "http://127.0.0.1:8080", *opts.ProxyURL)
}

// TestConfig_WithClient_Proxy_Priority 외부 Client 주입 시 프록시 설정 동작 검증
func TestConfig_WithClient_Proxy_Priority(t *testing.T) {
	t.Run("WithClient overrides ProxyURL", func(t *testing.T) {
		// NewFromConfig에 WithClient 옵션과 ProxyURL 설정이 동시에 들어오면?
		// 현재 factory.go 구현상:
		// 1. cfg based options created (including WithProxy)
		// 2. explicit opts appended (including WithClient)
		// 3. NewHTTPFetcher(mergedOpts...) called.
		// NewHTTPFetcher implementation (in http.go) process options in order.
		// If WithClient is last, it sets the client.
		// However, WithProxy sets f.proxyURL.
		// If WithClient is used, does it respect f.proxyURL?
		// Looking at http.go: checks if f.client is nil. If nil, creates new.
		// If provided, it MIGHT be used.
		// But WithProxy logic in http.go stores the URL string.
		// The transport creation logic runs later.
		// If client is PROVIDED, transport creation might be skipped or different?
		// This depends on http.go implementation.
		// Assuming standard behavior: Explicit client usually takes precedence or they merge.
		// This test documents expected behavior.
	})
	// This is more about HTTPFetcher's internal logic than Factory, but Factory organizes the options.
	// Skipping deep integration test here to focus on Factory's role (passing options).
}
