package fetcher

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	apperrors "github.com/darkkaiser/rss-feed-server/internal/errors"
	"github.com/stretchr/testify/assert"
)

// dummyFetcher is a local stub to avoid import cycles with the mocks package.
type dummyFetcher struct{}

func (d *dummyFetcher) Do(req *http.Request) (*http.Response, error) {
	return nil, nil
}

func (d *dummyFetcher) Close() error {
	return nil
}

func TestNormalizeMaxRetries(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"Normal value", 3, 3},
		{"Minimum boundary (0)", 0, 0},
		{"Below minimum (-1 -> 0)", -1, 0},
		{"Maximum boundary (10)", 10, 10},
		{"Above maximum (11 -> 10)", 11, 10},
		{"Far above maximum (100 -> 10)", 100, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeMaxRetries(tt.input))
		})
	}
}

func TestNormalizeRetryDelays(t *testing.T) {
	tests := []struct {
		name        string
		minDelay    time.Duration
		maxDelay    time.Duration
		expectedMin time.Duration
		expectedMax time.Duration
		description string
	}{
		{
			name:        "Normal values",
			minDelay:    2 * time.Second,
			maxDelay:    10 * time.Second,
			expectedMin: 2 * time.Second,
			expectedMax: 10 * time.Second,
			description: "Normal range should be preserved",
		},
		{
			name:        "Min delay too short (< 1s)",
			minDelay:    100 * time.Millisecond,
			maxDelay:    10 * time.Second,
			expectedMin: 1 * time.Second,
			expectedMax: 10 * time.Second,
			description: "Min delay should be clamped to 1s",
		},
		{
			name:        "Max delay zero (default)",
			minDelay:    time.Second,
			maxDelay:    0,
			expectedMin: time.Second,
			expectedMax: defaultMaxRetryDelay,
			description: "Zero max delay should use default value",
		},
		{
			name:        "Max delay less than Min delay",
			minDelay:    5 * time.Second,
			maxDelay:    2 * time.Second,
			expectedMin: 5 * time.Second,
			expectedMax: 5 * time.Second,
			description: "Max delay should be adjusted to Min delay",
		},
		{
			name:        "Min delay too short AND Max delay less than corrected Min",
			minDelay:    100 * time.Millisecond, // -> becomes 1s
			maxDelay:    500 * time.Millisecond,
			expectedMin: 1 * time.Second,
			expectedMax: 1 * time.Second, // Max (0.5s) < Min (1s), so Max becomes 1s
			description: "Complex adjustment: Min clamped to 1s, then Max adjusted to new Min",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMin, gotMax := normalizeRetryDelays(tt.minDelay, tt.maxDelay)
			assert.Equal(t, tt.expectedMin, gotMin, "minDelay mismatch")
			assert.Equal(t, tt.expectedMax, gotMax, "maxDelay mismatch")
		})
	}
}

func TestNewRetryFetcher_Initialization(t *testing.T) {
	mockDelegate := &dummyFetcher{}

	// Verify that NewRetryFetcher correctly uses the normalization functions
	// We test one complex case to ensure integration
	f := NewRetryFetcher(mockDelegate, 100, 100*time.Millisecond, 500*time.Millisecond)

	assert.Equal(t, 10, f.maxRetries)             // 100 -> 10
	assert.Equal(t, time.Second, f.minRetryDelay) // 0.1s -> 1s
	assert.Equal(t, time.Second, f.maxRetryDelay) // 0.5s < 1s -> 1s
	assert.Equal(t, mockDelegate, f.delegate)
}

func TestIsRetriable(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		// [Category 1: Basic Checks]
		{"Nil error", nil, false},
		{"Context canceled", context.Canceled, false},
		{"Wrapped Context canceled", fmt.Errorf("wrapped: %w", context.Canceled), false},
		{"Context deadline exceeded (raw)", context.DeadlineExceeded, true},

		// [Category 2: URL Errors]
		{"URL Error - 10 redirects", &url.Error{Err: errors.New("stopped after 10 redirects")}, false},
		{"URL Error - invalid control character", &url.Error{Err: errors.New("invalid control character in URL")}, false},
		{"URL Error - unsupported protocol", &url.Error{Err: errors.New("unsupported protocol scheme \"ftp\"")}, false},
		{"Wrapped URL Error", fmt.Errorf("wrapped: %w", &url.Error{Err: errors.New("unsupported protocol scheme \"ftp\"")}), false},
		{"URL Error - generic (retriable)", &url.Error{Err: errors.New("some network error")}, true},

		// [Category 3: Certificate Errors]
		{"Cert Error - HostnameError", x509.HostnameError{}, false},
		{"Cert Error - UnknownAuthorityError", x509.UnknownAuthorityError{}, false},
		{"Cert Error - CertificateInvalidError", x509.CertificateInvalidError{}, false},

		// [Category 4: Network Errors]
		{"Net Error - Timeout", &net.OpError{Err: context.DeadlineExceeded}, true},
		{"Generic Net Error", &net.OpError{Err: errors.New("connection reset")}, true}, // Assumed retriable via apperrors fallback

		// [Category 5: App Errors - Unavailable (Retriable)]
		{"AppError - Unavailable (generic)", apperrors.New(apperrors.Unavailable, "generic unavailable"), true},
		{"AppError - HTTP 500", apperrors.Wrap(&HTTPStatusError{StatusCode: 500}, apperrors.Unavailable, "500"), true},
		{"AppError - HTTP 502", apperrors.Wrap(&HTTPStatusError{StatusCode: http.StatusBadGateway}, apperrors.Unavailable, "502"), true},
		{"AppError - HTTP 503", apperrors.Wrap(&HTTPStatusError{StatusCode: http.StatusServiceUnavailable}, apperrors.Unavailable, "503"), true},
		{"AppError - HTTP 504", apperrors.Wrap(&HTTPStatusError{StatusCode: http.StatusGatewayTimeout}, apperrors.Unavailable, "504"), true},

		// [Category 6: App Errors - Unavailable (Non-Retriable 5xx)]
		{"AppError - HTTP 501", apperrors.Wrap(&HTTPStatusError{StatusCode: http.StatusNotImplemented}, apperrors.Unavailable, "501"), false},
		{"AppError - HTTP 505", apperrors.Wrap(&HTTPStatusError{StatusCode: http.StatusHTTPVersionNotSupported}, apperrors.Unavailable, "505"), false},
		{"AppError - HTTP 511", apperrors.Wrap(&HTTPStatusError{StatusCode: http.StatusNetworkAuthenticationRequired}, apperrors.Unavailable, "511"), false},

		// [Category 7: App Errors - Non-Retriable Types]
		{"AppError - ExecutionFailed", apperrors.New(apperrors.ExecutionFailed, "failed"), false},
		{"AppError - InvalidInput", apperrors.New(apperrors.InvalidInput, "invalid"), false},
		{"AppError - Forbidden", apperrors.New(apperrors.Forbidden, "forbidden"), false},
		{"AppError - NotFound", apperrors.New(apperrors.NotFound, "not found"), false},

		// [Category 8: Complex Wrappings]
		{"Deeply wrapped Net Error", fmt.Errorf("w1: %w", fmt.Errorf("w2: %w", &net.OpError{Err: context.DeadlineExceeded})), true},
		{"ErrGetBodyFailed", newErrGetBodyFailed(errors.New("inner")), false},
		{"Unknown generic error", errors.New("unknown error"), true}, // Default safe bet
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isRetriable(tt.err))
		})
	}
}

func TestIsIdempotentMethod(t *testing.T) {
	tests := []struct {
		method   string
		expected bool
	}{
		{http.MethodGet, true},
		{http.MethodHead, true},
		{http.MethodOptions, true},
		{http.MethodTrace, true},
		{http.MethodPut, true},
		{http.MethodDelete, true},
		{http.MethodPost, false},
		{http.MethodPatch, false},
		{"INVALID_METHOD", false},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			assert.Equal(t, tt.expected, isIdempotentMethod(tt.method))
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name          string
		value         string
		expectedDelay time.Duration
		expectedValid bool
		delta         time.Duration
	}{
		{"Empty value", "", 0, false, 0},
		{"Seconds format", "120", 120 * time.Second, true, 0},
		{"Seconds format with whitespace", "  120  ", 120 * time.Second, true, 0},
		{"Seconds format (zero)", "0", 0, true, 0},
		{"Seconds format (negative)", "-10", 0, false, 0},
		{"HTTP Date format (Future)", time.Now().UTC().Add(time.Hour).Format(http.TimeFormat), time.Hour, true, time.Second},
		{"HTTP Date format (Past)", time.Now().UTC().Add(-time.Hour).Format(http.TimeFormat), 0, true, 0},
		{"Invalid format", "soon", 0, false, 0},
		{"Very large number", "3000000000", 3000000000 * time.Second, true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delay, valid := parseRetryAfter(tt.value)
			assert.Equal(t, tt.expectedValid, valid)
			if tt.expectedValid {
				if tt.delta > 0 {
					assert.InDelta(t, tt.expectedDelay, delay, float64(tt.delta))
				} else {
					assert.Equal(t, tt.expectedDelay, delay)
				}
			}
		})
	}
}

func TestNewErrRetryAfterExceeded(t *testing.T) {
	err := newErrRetryAfterExceeded("10s", "1s")
	assert.Error(t, err)
	expectedMsg := "서버가 요구한 재시도 대기 시간(10s)이 설정된 최대 재시도 대기 시간(1s)을 초과하여 재시도가 중단되었습니다"
	assert.Contains(t, err.Error(), expectedMsg)
	assert.True(t, apperrors.Is(err, apperrors.Unavailable))
}
