package fetcher

import (
	"container/list"
	"net/http"
	"time"
)

// Export constants for testing
const (
	MaxDrainBytes        = maxDrainBytes
	DefaultMaxBytes      = defaultMaxBytes
	MinRetries           = minAllowedRetries
	MaxAllowedRetries    = maxAllowedRetries
	DefaultMaxRetryDelay = defaultMaxRetryDelay
)

// Export variables for testing
// Note: These expose the variables themselves, but since they are unexported in the package,
// we can only expose their values or provide functions to modify them if they are vars.
// For slices/maps, exporting the value allows modification of the underlying data.
var (
	// DefaultUserAgents exposes the default UA list.
	// Since it's a slice, modifications to elements will affect the package.
	DefaultUserAgents = defaultUserAgents

	// DefaultTransport exposes the internal default transport.
	DefaultTransport = defaultTransport
)

// Export functions for testing
var DrainAndCloseBody = drainAndCloseBody
var NormalizeByteLimit = normalizeByteLimit

// Helper functions for white-box testing

// ResetTransportCache resets the internal transport cache.
// This is crucial for verifying cache behavior without interference between tests.
func ResetTransportCache() {
	transportCacheMu.Lock()
	defer transportCacheMu.Unlock()
	transportCache = make(map[transportCacheKey]*list.Element)
	transportCacheLRU.Init()
}

// SetDefaultUserAgents allows overwriting the default UA list for deterministic testing.
// It returns a restore function to revert the changes.
func SetDefaultUserAgents(uas []string) (restore func()) {
	original := defaultUserAgents
	defaultUserAgents = uas
	return func() {
		defaultUserAgents = original
	}
}

// =========================================================================
// Inspector Helpers for White-box Testing
// =========================================================================

// InspectLoggingFetcher returns the delegate of a LoggingFetcher.
func InspectLoggingFetcher(f Fetcher) Fetcher {
	if lf, ok := f.(*LoggingFetcher); ok {
		return lf.delegate
	}
	return nil
}

// InspectUserAgentFetcher returns the delegate and config of a UserAgentFetcher.
func InspectUserAgentFetcher(f Fetcher) (delegate Fetcher, userAgents []string) {
	if uaf, ok := f.(*UserAgentFetcher); ok {
		return uaf.delegate, uaf.userAgents
	}
	return nil, nil
}

// InspectRetryFetcher returns the delegate and config of a RetryFetcher.
func InspectRetryFetcher(f Fetcher) (delegate Fetcher, maxRetries int, minDelay, maxDelay time.Duration) {
	if rf, ok := f.(*RetryFetcher); ok {
		return rf.delegate, rf.maxRetries, rf.minRetryDelay, rf.maxRetryDelay
	}
	return nil, 0, 0, 0
}

// InspectMimeTypeFetcher returns the delegate and config of a MimeTypeFetcher.
func InspectMimeTypeFetcher(f Fetcher) (delegate Fetcher, allowedTypes []string, allowMissingContentType bool) {
	if mf, ok := f.(*MimeTypeFetcher); ok {
		return mf.delegate, mf.allowedMimeTypes, mf.allowMissingContentType
	}
	return nil, nil, false
}

// InspectStatusCodeFetcher returns the delegate and config of a StatusCodeFetcher.
func InspectStatusCodeFetcher(f Fetcher) (delegate Fetcher, allowedCodes []int) {
	if sf, ok := f.(*StatusCodeFetcher); ok {
		return sf.delegate, sf.allowedStatusCodes
	}
	return nil, nil
}

// InspectMaxBytesFetcher returns the delegate and config of a MaxBytesFetcher.
func InspectMaxBytesFetcher(f Fetcher) (delegate Fetcher, maxBytes int64) {
	if mbf, ok := f.(*MaxBytesFetcher); ok {
		return mbf.delegate, mbf.limit
	}
	return nil, 0
}

// HTTPFetcherOptions exposes internal configuration of an HTTPFetcher for testing.
type HTTPFetcherOptions struct {
	ProxyURL              *string
	MaxIdleConns          *int
	MaxIdleConnsPerHost   *int
	MaxConnsPerHost       *int
	IdleConnTimeout       *time.Duration
	TLSHandshakeTimeout   *time.Duration
	ResponseHeaderTimeout *time.Duration
	Timeout               time.Duration // Client.Timeout (Value type for safety)
	DisableCaching        bool
}

// InspectHTTPFetcher returns the internal configuration of an HTTPFetcher.
func InspectHTTPFetcher(f Fetcher) *HTTPFetcherOptions {
	hf, ok := f.(*HTTPFetcher)
	if !ok {
		return nil
	}

	return &HTTPFetcherOptions{
		ProxyURL:              hf.proxyURL,
		MaxIdleConns:          hf.maxIdleConns,
		MaxIdleConnsPerHost:   hf.maxIdleConnsPerHost,
		MaxConnsPerHost:       hf.maxConnsPerHost,
		IdleConnTimeout:       hf.idleConnTimeout,
		TLSHandshakeTimeout:   hf.tlsHandshakeTimeout,
		ResponseHeaderTimeout: hf.responseHeaderTimeout,
		Timeout:               hf.client.Timeout,
		DisableCaching:        hf.disableTransportCaching,
	}
}

// =========================================================================
// Transport & Cache Inspection Helpers
// =========================================================================

// TransportCacheLen returns the current number of items in the transport cache.
func TransportCacheLen() int {
	transportCacheMu.RLock()
	defer transportCacheMu.RUnlock()
	return len(transportCache)
}

// GetTransportFromFetcher extracts the underlying *http.Transport from an HTTPFetcher.
// It returns nil if the fetcher is not an HTTPFetcher or if the transport is not *http.Transport.
func GetTransportFromFetcher(f Fetcher) *http.Transport {
	hf, ok := f.(*HTTPFetcher)
	if !ok {
		return nil
	}

	// HTTPFetcher holds a *http.Client, which holds the RoundTripper (Transport)
	if tr, ok := hf.client.Transport.(*http.Transport); ok {
		return tr
	}
	return nil
}
